#  Copyright 2023 The Kapacity Authors.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

import logging
import math
import time

import lightgbm as lgb
import numpy as np
import pandas as pd
from scipy.stats import pearsonr
from sklearn.linear_model import ElasticNetCV
from sklearn.metrics import mean_squared_error
from sklearn.metrics import r2_score as sk_r2
from sklearn.model_selection import train_test_split


class Estimator(object):
    def __init__(self,
                 logger,
                 data,
                 data_pred,
                 time_col,
                 resource_col,
                 replicas_col,
                 traffic_cols,
                 resource_target,
                 time_delta_hours,
                 test_dataset_size_in_seconds):
        self.logger = logger

        self.time_col = time_col
        self.resource_col = resource_col
        self.replicas_col = replicas_col
        self.traffic_cols = traffic_cols

        self.resource_target = float(resource_target)
        self.resource_target_lst = list()
        self.resource_target_lst_rf = list()

        self.time_delta_hours = time_delta_hours
        self.test_dataset_size_in_seconds = test_dataset_size_in_seconds

        self.data = data
        self.nan_max = None
        self.nan_pct = None
        self.qps_100 = None
        self.data_train = None
        self.data_test = None
        self.default_cnt = data[self.replicas_col].max()
        self.features = list()

        self.no_result = 128

        self.replicas_max = max(100, math.ceil(data[self.replicas_col].max() * 2))  # a reasonable large max
        self.replicas_min = 1

        self.model_coef_ = None
        self.model_intercept_ = None
        self.train_timestamp = None
        self.train_loss = None

        self.r2_score = None
        self.r2_score_rf = None
        self.reg_rf = None
        self.reg_rf_str = None
        self.mse_rf = None
        self.big_e_rf = None
        self.pearsonr_rf = None
        self.big_e_10_rf = None

        self.future_ori_rf = None
        self.future_ctrl_rf = None
        self.future_cnt_rf = None

        self.mse = None
        self.big_e = None
        self.big_e_10 = None
        self.pearsonr = None
        self.future_timestamp = None
        self.future_ture = None
        self.future_ori = None
        self.future_ctrl = None
        self.future_cnt = None
        self.future_cnt_60 = None
        self.future_ctrl_60 = None

        self.assessment_timestamp = None
        self.assessment_ori = None

        self.rule_code = None
        self.rule_code_rf = None
        self.rule_code_str = None
        self.rule_code_rf_str = None

        self.data_pred = data_pred

    def preprocess_data(self):
        df = self.data[[self.time_col, self.resource_col, self.replicas_col] + self.traffic_cols].copy()
        df[self.time_col] = df[self.time_col].astype(int)
        df.drop_duplicates(inplace=True)
        df.sort_values(by=self.time_col, inplace=True)
        df = df.reset_index(drop=True)

        # scale resource to 0~100
        resource_max = df[self.resource_col].max()
        resource_scaling_factor = 1 if resource_max <= 100 else 10**np.ceil(np.log10(resource_max / 100))
        self.logger.info(f'resource scaling factor: {resource_scaling_factor}')
        df[self.resource_col] = df[self.resource_col] / resource_scaling_factor
        self.resource_target = self.resource_target / resource_scaling_factor

        features = self.traffic_cols

        self.logger.info(f'checkout before filtering NaN: '
                         f'NUM-{len(df.columns)}-SHAPE-{str(df.shape)}-NAME-{str(df.columns.tolist())}')

        cnt_col_ori = len(features)
        nan_col = list()
        nan_max = list()

        st1 = time.time()
        for i in features:
            if (df[i].isnull().sum() / df.shape[0] > 0.1) or (df[i].sum() == 0):
                nan_max.append([float(df[i].astype(float).idxmax()), float(df[i].astype(float).max())])
                df.drop(i, axis=1, inplace=True)
                nan_col.append(i)

        self.logger.info(f'filtering NaN cost time: {time.time() - st1}')

        self.nan_max = nan_max
        cnt_col_aft = cnt_col_ori - len(nan_col)
        if cnt_col_ori == 0:
            pass
        else:
            self.nan_pct = 1 - (cnt_col_aft / cnt_col_ori)

        df.dropna(axis=0, subset=df.columns, how='any', inplace=True)
        df = df.loc[df[self.replicas_col] != 0]

        self.logger.info(f'checkout after filtering NaN: '
                         f'NUM-{len(df.columns)}-SHAPE-{str(df.shape)}-NAME-{str(df.columns.tolist())}')

        features = list(df.columns)
        features = [i for i in features if i in self.traffic_cols]
        self.features = features

        df['datetime'] = pd.to_datetime(df[self.time_col], unit='s')
        df['datetime'] = df['datetime'] + pd.Timedelta(hours=self.time_delta_hours)
        df['weekday'] = df['datetime'].dt.dayofweek
        df['hour'] = df['datetime'].dt.hour
        df['minute'] = df['datetime'].dt.minute + df['datetime'].dt.hour * 60

        df['traintest_flag'] = 'ruleout'
        df.loc[(df[self.time_col] < (df[self.time_col].max() - self.test_dataset_size_in_seconds)), 'traintest_flag'] = 'train'
        df.loc[(df[self.time_col] >= (df[self.time_col].max() - self.test_dataset_size_in_seconds)), 'traintest_flag'] = 'test'

        self.data_train = df[df['traintest_flag'] == 'train']
        self.data_test = df[df['traintest_flag'] == 'test']

        self.default_cnt = self.data_test[self.replicas_col].max()

    def train(self):
        df_sample = self.data_train
        df_sample.sort_values(by=self.time_col, inplace=True)
        df_sample.reset_index(drop=True, inplace=True)

        self.train_timestamp = df_sample[self.time_col].values

        features = self.features

        st2 = time.time()

        x_train = df_sample[features + [self.replicas_col, self.time_col] + ['weekday', 'minute']].copy()
        x_train[features] = x_train[features].astype(float).div(x_train[self.replicas_col].values, axis=0)
        y_train = df_sample[self.resource_col].astype(float).div(x_train[self.replicas_col].values, axis=0)

        x_train_trans = x_train[features].values

        self.logger.info(f'train dataset traffic matrix calculation cost time: {time.time() - st2}')

        self.logger.info(f'checkout before train: SHAPE-{str(x_train_trans.shape)}')

        st3 = time.time()
        reg_multi = ElasticNetCV(l1_ratio=[.1, .5, .7, .9, .95, .99, 1], cv=5, positive=True, n_jobs=1).fit(
            x_train_trans, y_train.values)
        self.logger.info(f'training cost time: {time.time() - st3}')

        self.model_coef_ = [reg_multi.coef_, features]
        self.model_intercept_ = reg_multi.intercept_

        st4 = time.time()
        y_pred_linear_multi = reg_multi.predict(x_train_trans)
        self.train_loss = np.array(y_train) - y_pred_linear_multi.flatten()
        self.logger.info(f'train loss calculation cost time: {time.time() - st4}')

        x_train['lr_pred'] = y_pred_linear_multi.flatten()

        if (x_train[self.time_col].max() - x_train[self.time_col].min()) // 86400 < 10:
            x_train['weekday'] = x_train['weekday'].apply(lambda x: 0 if x < 5 else 1)

        x_train['train_loss'] = abs(self.train_loss)

        x_train['sample_weight'] = x_train['train_loss'] / x_train['train_loss'].sum()

        x_train_trans = x_train[features + ['lr_pred', 'weekday', 'minute', 'sample_weight']].values
        y_train = self.train_loss

        x_train_trans, x_valid, y_train, y_valid = train_test_split(x_train_trans, y_train, test_size=0.2,
                                                                    random_state=42, shuffle=True)

        if (self.data_train[self.time_col].max() - self.data_train[self.time_col].min()) // 86400 < 10:
            reg_rf = lgb.LGBMRegressor(num_leaves=31,
                                       learning_rate=0.1,
                                       n_estimators=50,
                                       n_jobs=1,
                                       min_child_samples=1,
                                       min_child_weight=0,
                                       objective='mse',
                                       boosting='dart',
                                       )
        else:
            reg_rf = lgb.LGBMRegressor(num_leaves=31,
                                       learning_rate=0.1,
                                       n_estimators=50,
                                       n_jobs=1,
                                       min_child_samples=3,
                                       min_child_weight=0,
                                       objective='mse',
                                       boosting='dart',
                                       )

        reg_rf.fit(x_train_trans[:, :-1], y_train,
                   eval_set=[(x_valid[:, :-1], y_valid)],
                   sample_weight=np.array(x_train_trans[:, -1].tolist()),
                   callbacks=[lgb.log_evaluation(0)])

        self.reg_rf = reg_rf
        self.reg_rf_str = reg_rf.booster_.model_to_string()

    def test(self):
        df_sample = self.data_test
        df_sample.sort_values(by=self.time_col, inplace=True)
        df_sample.reset_index(drop=True, inplace=True)

        self.future_timestamp = df_sample[self.time_col]

        features = self.features

        st5 = time.time()

        x_test = df_sample[features + [self.replicas_col, self.time_col] + ['weekday', 'minute']].copy()
        x_test[features] = x_test[features].astype(float).div(x_test[self.replicas_col].values, axis=0)
        y_test = df_sample[self.resource_col].astype(float).div(x_test[self.replicas_col].values, axis=0)
        self.future_ture = y_test

        x_test_trans = x_test[features].values

        self.logger.info(f'test dataset traffic matrix calculation cost time: {time.time() - st5}')

        self.logger.info(f'checkout before test: SHAPE-{str(x_test_trans.shape)}')

        st6 = time.time()
        model = ElasticNetCV()
        model.coef_ = (self.model_coef_[0])
        model.intercept_ = (self.model_intercept_)

        y_pred_linear_multi = model.predict(x_test_trans)
        self.future_ori = y_pred_linear_multi

        self.r2_score = model.score(x_test_trans, y_test)

        mse = mean_squared_error(y_test, y_pred_linear_multi)
        self.mse = mse

        loss = np.array(y_test) - y_pred_linear_multi.flatten()

        big_e = len(loss[(loss > 5) | (loss < -5)]) / len(loss)
        self.big_e = big_e

        pearsonr_time = pd.DataFrame(
            np.array([self.future_timestamp.values, np.array(y_test), y_pred_linear_multi.flatten()]).T,
            columns=['timestamp', 'y_test', 'y_pred'])

        zpearsonr = pearsonr(pearsonr_time['y_test'], pearsonr_time['y_pred'])

        self.pearsonr = zpearsonr
        self.logger.info(f'test dataset estimation cost time: {time.time() - st6}')

        st7 = time.time()
        loss_time = pd.DataFrame(np.array([self.future_timestamp.values, loss]).T, columns=['timestamp', 'loss'])

        get_loss_time = loss_time[loss_time['loss'] > 10]
        self.big_e_10 = get_loss_time.shape[0]
        self.logger.info(f'large loss point calculation cost time: {time.time() - st7}')

        x_test['lr_pred'] = y_pred_linear_multi.flatten()
        if (self.data_train[self.time_col].max() - self.data_train[self.time_col].min()) // 86400 < 10:
            x_test['weekday'] = x_test['weekday'].apply(lambda x: 0 if x < 5 else 1)

        x_test_trans = x_test[features + ['lr_pred', 'weekday', 'minute']].values

        reg_rf = self.reg_rf

        y_residual = reg_rf.predict(x_test_trans)
        y_final = y_pred_linear_multi + y_residual

        self.r2_score_rf = sk_r2(self.future_ture, y_final)

        self.future_ori_rf = y_final

        mse_rf = mean_squared_error(self.future_ture, y_final)
        self.mse_rf = mse_rf

        loss_rf = np.array(self.future_ture) - y_final.flatten()

        big_e_rf = len(loss_rf[(loss_rf > 5) | (loss_rf < -5)]) / len(loss_rf)
        self.big_e_rf = big_e_rf

        pearsonr_rf = pearsonr(self.future_ture, y_final)
        self.pearsonr_rf = pearsonr_rf

        loss_time_rf = pd.DataFrame(np.array([self.future_timestamp.values, loss_rf]).T,
                                    columns=['timestamp', 'loss'])
        get_loss_time_rf = loss_time_rf[(loss_time_rf['loss'] > 10)]
        self.big_e_10_rf = get_loss_time_rf.shape[0]

    def policy_residual(self):
        def global_search_residual(left, right, target, model, data, data_tot, reg_rf, cols,
                                   features, y_true, cnt_true, is_ratio):
            if y_true is not None:
                if y_true < target:
                    new_df = np.repeat(np.array([data.T.values]), cnt_true - left + 1, axis=0)
                    counts = np.array(range(left, cnt_true + 1, 1)).reshape(-1, 1)
                else:
                    new_df = np.repeat(np.array([data.T.values]), right - cnt_true + 1, axis=0)
                    counts = np.array(range(cnt_true, right + 1, 1)).reshape(-1, 1)

                x_test_trans = np.divide(new_df, counts)

                y_pred_linear_multi = model.predict(x_test_trans)

                if y_true < target:
                    new_df = np.repeat(np.array([data_tot.T.values]), cnt_true - left + 1, axis=0)
                else:
                    new_df = np.repeat(np.array([data_tot.T.values]), right - cnt_true + 1, axis=0)

                if is_ratio:
                    pass
                else:
                    new_df[:, :len(features)] = np.divide(new_df[:, :len(features)], counts)

                x_test_trans = np.concatenate((new_df[:, :len(features)], y_pred_linear_multi.flatten().reshape(-1, 1),
                                               new_df[:, len(features):]), axis=1)

                y_residual = reg_rf.predict(x_test_trans)

            else:
                new_df = np.repeat(np.array([data.T.values]), right - left + 1, axis=0)
                counts = np.array(range(left, right + 1, 1)).reshape(-1, 1)

                x_test_trans = np.divide(new_df, counts)
                y_pred_linear_multi = model.predict(x_test_trans)

                new_df = np.repeat(np.array([data_tot.T.values]), right - left + 1, axis=0)

                if is_ratio:
                    pass
                else:
                    new_df[:, :len(features)] = np.divide(new_df[:, :len(features)], counts)

                x_test_trans = np.concatenate((new_df[:, :len(features)], y_pred_linear_multi.flatten().reshape(-1, 1),
                                               new_df[:, len(features):]), axis=1)
                y_residual = reg_rf.predict(x_test_trans)

            y_final = y_pred_linear_multi + y_residual

            x_test_n = np.concatenate((counts, y_final.flatten().reshape(-1, 1)), axis=1)

            zpearsonr = pearsonr(1 / x_test_n[:, 0], x_test_n[:, 1])

            x_test_n = x_test_n[x_test_n[:, 1] <= target, :]

            if x_test_n.shape[0] == 0:
                if zpearsonr[0] > 0.9 and zpearsonr[1] < 0.01:
                    x_test_n = np.concatenate((counts, y_final.flatten().reshape(-1, 1)), axis=1)
                    return x_test_n[-1, 0], np.array([x_test_n[-1, 1]])
                else:
                    return -1, -1

            pred_lb = int(x_test_n[x_test_n[:, 0].argmin(), 1])

            x_test_n = x_test_n[x_test_n[:, 1] >= pred_lb, :]

            if x_test_n.shape[0] == 0:
                if zpearsonr[0] > 0.9 and zpearsonr[1] < 0.01:
                    x_test_n = np.concatenate((counts, y_final.flatten().reshape(-1, 1)), axis=1)
                    return x_test_n[0, 0], np.array([x_test_n[0, 1]])
                else:
                    return -1, -1

            if y_true is not None:
                if y_true < target:
                    out1 = x_test_n[:, 0].max()
                    out2 = x_test_n[x_test_n[:, 0].argmax(), 1]
                else:
                    out1 = x_test_n[:, 0].min()
                    out2 = x_test_n[x_test_n[:, 0].argmin(), 1]
            else:
                if len(x_test_n[x_test_n[:, 0] <= cnt_true, :]) != 0:
                    x_test_n = x_test_n[x_test_n[:, 0] <= cnt_true, :]
                out1 = x_test_n[abs(x_test_n[:, 0] - cnt_true).argmin(), 0]
                out2 = x_test_n[abs(x_test_n[:, 0] - cnt_true).argmin(), 1]

            return out1, np.array([out2])

        df_sample = self.data_pred

        df_sample.dropna(subset=self.time_col, how='any', inplace=True)
        df_sample[self.features] = df_sample[self.features].fillna(0)

        df_sample['datetime'] = pd.to_datetime(df_sample[self.time_col], unit='s')
        df_sample['datetime'] = df_sample['datetime'] + pd.Timedelta(hours=self.time_delta_hours)
        df_sample['weekday'] = df_sample['datetime'].dt.dayofweek
        df_sample['hour'] = df_sample['datetime'].dt.hour
        df_sample['minute'] = df_sample['datetime'].dt.minute + df_sample['datetime'].dt.hour * 60

        df_sample.sort_values(by=self.time_col, inplace=True)
        df_sample.reset_index(drop=True, inplace=True)

        features = self.features

        x_test = df_sample[features + ['weekday', 'minute']].copy()

        if (self.data_train[self.time_col].max() - self.data_train[self.time_col].min()) // 86400 < 10:
            x_test['weekday'] = x_test['weekday'].apply(lambda x: 0 if x < 5 else 1)

        model = ElasticNetCV()
        model.coef_ = (self.model_coef_[0])
        model.intercept_ = (self.model_intercept_)

        reg_rf = self.reg_rf

        st8 = time.time()
        pred_count = []
        pred_resource = []
        rule_code = list()
        for i in range(x_test.shape[0]):
            rule_code_tmp = 0

            resource_target_tmp = self.resource_target

            test_x0 = x_test.iloc[i].copy()

            self.resource_target_lst_rf.append(resource_target_tmp)

            count, y_final = global_search_residual(self.replicas_min, self.replicas_max, resource_target_tmp, model,
                                                    test_x0[features], test_x0[features + ['weekday', 'minute']],
                                                    reg_rf, features + ['weekday', 'minute'], features, None, 0, False)

            if count == -1 or y_final == -1:
                rule_code_tmp = rule_code_tmp | self.no_result
                count = np.nan
                y_final = np.array([np.nan])

            pred_resource.append(y_final)
            pred_count.append(count)
            rule_code.append(rule_code_tmp)

        self.logger.info(f'policy generation cost time: {time.time() - st8}')

        df_sample['pred_resource'] = pred_resource
        df_sample['pred_replicas'] = pred_count
        df_sample['pred_replicas'] = df_sample['pred_replicas'].apply(lambda x: math.ceil(x))
        df_sample['rule_code'] = rule_code

        self.future_ctrl_rf = pred_resource
        self.future_cnt_rf = df_sample['pred_replicas'].values.tolist()
        self.rule_code_rf = rule_code

        def bin2str(x):
            out = list()
            if x & self.no_result:
                out.append('NO_RESULT')
            out = ','.join(out)
            return out

        df_sample['rule_code'] = df_sample['rule_code'].apply(lambda x: bin2str(x))
        self.rule_code_rf_str = df_sample['rule_code'].values.tolist()

        df_sample['pred_resource'] = df_sample['pred_resource'].apply(lambda x: x[0])
        df_sample['decision_type'] = 'residual'
        self.output = df_sample

    def policy_linear(self):
        def global_search_linear(left, right, target, model, data, cols, features, y_true, cnt_true):
            if y_true is not None:
                if y_true < target:
                    new_df = np.repeat(np.array([data.T.values]), cnt_true - left + 1, axis=0)
                    counts = np.array(range(left, cnt_true + 1, 1)).reshape(-1, 1)
                else:
                    new_df = np.repeat(np.array([data.T.values]), right - cnt_true + 1, axis=0)
                    counts = np.array(range(cnt_true, right + 1, 1)).reshape(-1, 1)

                x_test_trans = np.divide(new_df, counts)

                y_pred_linear_multi = model.predict(x_test_trans)
            else:
                new_df = np.repeat(np.array([data.T.values]), right - left + 1, axis=0)
                counts = np.array(range(left, right + 1, 1)).reshape(-1, 1)

                x_test_trans = np.divide(new_df, counts)

                y_pred_linear_multi = model.predict(x_test_trans)

            x_test_n = np.concatenate((counts, y_pred_linear_multi.flatten().reshape(-1, 1)), axis=1)

            zpearsonr = pearsonr(1 / x_test_n[:, 0], x_test_n[:, 1])

            x_test_n = x_test_n[x_test_n[:, 1] <= target, :]

            if x_test_n.shape[0] == 0:
                if zpearsonr[0] > 0.9 and zpearsonr[1] < 0.01:
                    x_test_n = np.concatenate((counts, y_pred_linear_multi.flatten().reshape(-1, 1)), axis=1)
                    return x_test_n[-1, 0], np.array([x_test_n[-1, 1]])
                else:
                    return -1, -1

            pred_lb = int(x_test_n[x_test_n[:, 0].argmin(), 1])

            x_test_n = x_test_n[x_test_n[:, 1] >= pred_lb, :]

            if x_test_n.shape[0] == 0:
                if zpearsonr[0] > 0.9 and zpearsonr[1] < 0.01:
                    x_test_n = np.concatenate((counts, y_pred_linear_multi.flatten().reshape(-1, 1)), axis=1)
                    return x_test_n[0, 0], np.array([x_test_n[0, 1]])
                else:
                    return -1, -1

            if y_true is not None:
                if y_true < target:
                    out1 = x_test_n[:, 0].max()
                    out2 = x_test_n[x_test_n[:, 0].argmax(), 1]
                else:
                    out1 = x_test_n[:, 0].min()
                    out2 = x_test_n[x_test_n[:, 0].argmin(), 1]
            else:
                if len(x_test_n[x_test_n[:, 0] <= cnt_true, :]) != 0:
                    x_test_n = x_test_n[x_test_n[:, 0] <= cnt_true, :]
                out1 = x_test_n[abs(x_test_n[:, 0] - cnt_true).argmin(), 0]
                out2 = x_test_n[abs(x_test_n[:, 0] - cnt_true).argmin(), 1]

            return out1, np.array([out2])

        df_sample = self.data_pred

        df_sample.dropna(subset=self.time_col, how='any', inplace=True)
        df_sample[self.features] = df_sample[self.features].fillna(0)

        df_sample['datetime'] = pd.to_datetime(df_sample[self.time_col], unit='s')
        df_sample['datetime'] = df_sample['datetime'] + pd.Timedelta(hours=self.time_delta_hours)
        df_sample['weekday'] = df_sample['datetime'].dt.dayofweek
        df_sample['hour'] = df_sample['datetime'].dt.hour
        df_sample['minute'] = df_sample['datetime'].dt.minute + df_sample['datetime'].dt.hour * 60

        df_sample.sort_values(by=self.time_col, inplace=True)
        df_sample.reset_index(drop=True, inplace=True)

        features = self.features

        x_test = df_sample[features].copy()

        model = ElasticNetCV()
        model.coef_ = (self.model_coef_[0])
        model.intercept_ = (self.model_intercept_)

        st8 = time.time()
        pred_count = []
        pred_resource = []
        rule_code = list()
        for i in range(x_test.shape[0]):
            rule_code_tmp = 0

            resource_target_tmp = self.resource_target

            test_x0 = x_test.iloc[i]

            self.resource_target_lst.append(resource_target_tmp)

            count, pred_test_y1 = global_search_linear(self.replicas_min, self.replicas_max, resource_target_tmp, model,
                                                       test_x0[features], features, features, None, 0)
            if count == -1 or pred_test_y1 == -1:
                rule_code_tmp = rule_code_tmp | self.no_result
                count = np.nan
                pred_test_y1 = np.array([np.nan])

            pred_resource.append(pred_test_y1)
            pred_count.append(count)
            rule_code.append(rule_code_tmp)

        self.logger.info(f'policy generation cost time: {time.time() - st8}')

        df_sample['pred_resource'] = pred_resource
        df_sample['pred_replicas'] = pred_count
        df_sample['pred_replicas'] = df_sample['pred_replicas'].apply(lambda x: math.ceil(x))
        df_sample['rule_code'] = rule_code

        self.future_ctrl_rf = pred_resource
        self.future_cnt_rf = df_sample['pred_replicas'].values.tolist()
        self.rule_code_rf = rule_code

        def bin2str(x):
            out = list()
            if x & self.no_result:
                out.append('NO_RESULT')
            out = ','.join(out)
            return out

        df_sample['rule_code'] = df_sample['rule_code'].apply(lambda x: bin2str(x))
        self.rule_code_str = df_sample['rule_code'].values.tolist()

        df_sample['pred_resource'] = df_sample['pred_resource'].apply(lambda x: x[0])
        df_sample['decision_type'] = 'linear'
        self.output = df_sample


class EstimationException(Exception):
    pass


def estimate(data: pd.DataFrame,
             data_pred: pd.DataFrame,
             time_col: str,
             resource_col: str,
             replicas_col: str,
             traffic_cols: list[str],
             resource_target: float,
             time_delta_hours: int,
             test_dataset_size_in_seconds: int = 86400) -> pd.DataFrame:
    logging.basicConfig(level=logging.INFO,
                        format='%(asctime)s - %(levelname)s: %(message)s')
    logger = logging.getLogger()

    logger.info('********* start replicas estimation *********')
    st10 = time.time()
    estimator = Estimator(logger, data, data_pred, time_col, resource_col, replicas_col, traffic_cols, resource_target,
                          time_delta_hours, test_dataset_size_in_seconds)
    logger.info(f'********* initialization cost time: {time.time() - st10} *********')

    st10 = time.time()
    estimator.preprocess_data()
    logger.info(f'********* data preprocessing cost time: {time.time() - st10} *********')
    st10 = time.time()
    estimator.train()
    logger.info(f'********* training cost time: {time.time() - st10} *********')
    st10 = time.time()
    estimator.test()
    logger.info(f'********* testing cost time: {time.time() - st10} *********')

    if (estimator.pearsonr[0] >= 0.9 and estimator.pearsonr[1] < 0.01
            and estimator.big_e_10 == 0 and estimator.mse < 10):
        st10 = time.time()
        estimator.policy_linear()
        logger.info(f'********* linear policy cost time: {time.time() - st10} *********')
        return estimator.output

    elif (estimator.pearsonr_rf[0] >= 0.9 and estimator.pearsonr_rf[1] < 0.01 and estimator.big_e_10_rf == 0
          and estimator.mse_rf < 10 and estimator.pearsonr[0] >= 0.6 and estimator.pearsonr[1] < 0.01):
        st10 = time.time()
        estimator.policy_residual()
        logger.info(f'********* residual policy cost time: {time.time() - st10} *********')
        return estimator.output

    else:
        raise EstimationException("no policy fits")
