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

import datetime
import json
import logging
import os
import time
from typing import List, Tuple

import numpy as np
import pandas as pd
import torch
import torch.nn as nn
import yaml
from torch import optim
from torch.utils.data import Dataset, DataLoader


class TimeSeriesDataset(Dataset):
    def __init__(self,
                 flag: str = 'train',
                 df: pd.DataFrame = None,
                 df_path: str = None,
                 config: dict = None,
                 model_path: str = None,
                 ) -> None:
        """
        Constructor of the dataset
        :param flag: train,val,test
        :param df: data
        :param df_path: path of data
        :param config:
        :param model_path: path of saving model
        """
        # init
        assert flag in ['train', 'test', 'val']
        type_map = {'train': 0, 'val': 1, 'test': 2}
        self.config = config
        self.set_type = type_map[flag]
        self.pred_len = self.config['prediction_length']
        self.context_len = self.config['context_length']
        self.seq_len = self.config['context_length'] + self.config['prediction_length']
        self.config = config
        self.features_map = {
            'H': ['hour', 'dayofweek', 'day'],
            '1H': ['hour', 'dayofweek', 'day'],
            'min': ['minute', 'hour', 'dayofweek', 'day'],
            '1min': ['minute', 'hour', 'dayofweek', 'day'],
            '10min': ['minute', 'hour', 'dayofweek', 'day'],
            'D': ['dayofweek', 'day'],
            '1D': ['dayofweek', 'day']
        }

        self.flag = flag
        self.model_path = model_path

        self.df = df
        self.df_path = df_path
        self.__read_data__()

    def __read_data__(self):
        # data filling & aligning
        # get the data
        if self.df is not None and isinstance(self.df, pd.DataFrame):
            df_raw = self.df
        elif self.df_path is not None and isinstance(self.df_path, str):
            df_raw = pd.read_csv(self.df_path)
        else:
            raise Exception('one of the df or df_path must be provided')
        # process the item_id map
        for i in range(len(self.config['series_cols'])):
            if i == 0:
                df_raw['item'] = df_raw[self.config['series_cols'][i]].astype(str)
            else:
                df_raw['item'] = df_raw['item'] + df_raw[self.config['series_cols'][i]].astype(str)

        if self.flag == 'train':
            df_raw['item_id'] = df_raw['item'].astype('category').cat.codes.astype('int16')
            item2id = {}
            item_df = df_raw[['item', 'item_id']].drop_duplicates()
            for i, j in zip(item_df['item'], item_df['item_id']):
                item2id[i] = j
            with open(os.path.join(self.model_path, 'item2id.json'), 'w') as fp:
                json.dump(item2id, fp)
        else:
            assert os.path.exists(os.path.join(self.model_path,
                                               'item2id.json')), 'there is no the item_id map for predict or val'
            with open(os.path.join(self.model_path, 'item2id.json'), 'r') as fp:
                item2id = json.load(fp)
            df_raw['item_id'] = df_raw['item'].apply(lambda series: item2id[series])
        # row -> column
        df_raw = df_raw[['item_id', self.config['time_col'], self.config['target_col']]]
        df_raw = df_raw.set_index([self.config['time_col'], 'item_id'])[self.config['target_col']].unstack()
        df_raw.columns.name = None
        df_raw.reset_index(inplace=True)
        df_raw.fillna(0, inplace=True)
        df_raw = df_raw.sort_values([self.config['time_col']]).reset_index(drop=True)
        df_raw[self.config['time_col']] = df_raw[self.config['time_col']].apply(
            lambda date: pd.to_datetime(int(date), unit='s'))
        # fill the full time range
        df_raw = df_raw.set_index(self.config['time_col']).resample(self.config['freq']).mean().reset_index()
        df_raw.fillna(0, inplace=True)
        # processing the target data
        cols_data = df_raw.columns[1:]
        data = df_raw[cols_data].values

        # processing the features
        df_stamp = df_raw[[self.config['time_col']]]
        df_stamp['day'] = df_stamp[self.config['time_col']].apply(lambda row: row.day, 1)
        df_stamp['dayofweek'] = df_stamp[self.config['time_col']].apply(lambda row: row.dayofweek, 1)
        df_stamp['hour'] = df_stamp[self.config['time_col']].apply(lambda row: row.hour, 1)
        df_stamp['minute'] = df_stamp[self.config['time_col']].apply(lambda row: row.minute, 1)
        stamp = df_stamp[self.features_map[self.config['freq']]].values

        if self.flag == 'train':
            self.data_x = data[0:-self.seq_len - self.config['val_num']]
            self.data_y = data[0:-self.seq_len - self.config['val_num']]
            self.data_stamp = stamp[0:-self.seq_len - self.config['val_num']]
        elif self.flag == 'val':
            self.data_x = data[-self.seq_len - self.config['val_num']:]
            self.data_y = data[-self.seq_len - self.config['val_num']:]
            self.data_stamp = stamp[-self.seq_len - self.config['val_num']:]
        else:
            self.data_x = data[-self.seq_len:]
            self.data_y = data[-self.seq_len:]
            self.data_stamp = stamp[-self.seq_len:]

        self.num_series = self.data_x.shape[1]

    def __getitem__(self, index):
        time_index = index // self.num_series
        series_index = index % self.num_series
        s_begin = time_index
        s_end = s_begin + self.context_len
        r_begin = s_end
        r_end = r_begin + self.pred_len
        seq_x_static_feat = np.zeros_like((1,))
        seq_x_static_feat[0] = series_index
        seq_y_static_feat = seq_x_static_feat

        seq_x = self.data_x[s_begin:s_end, series_index:series_index + 1]
        seq_y = self.data_y[r_begin:r_end, series_index:series_index + 1]
        seq_x_dynamic_feat = self.data_stamp[s_begin:s_end]
        seq_y_dynamic_feat = self.data_stamp[r_begin:r_end]

        return seq_x, seq_y, seq_x_static_feat, seq_y_static_feat, seq_x_dynamic_feat, seq_y_dynamic_feat

    def __len__(self):
        return (len(self.data_x) - self.seq_len + 1) * self.num_series


class Fcnet(nn.Module):
    """
    the implementation of fcnet model for time series forecasting
    """

    def __init__(self,
                 configs,
                 static_cardinality,
                 static_embed_dims,
                 dynamic_cardinality,
                 dynamic_embed_dims):
        super(Fcnet, self).__init__()
        assert len(static_cardinality) == len(
            static_embed_dims), 'len of static_cardinality equal to len of static_embed_dims'
        assert len(dynamic_embed_dims) == len(
            dynamic_cardinality), 'len of dynamic_cardinality equal to len of dynamic_embed_dims'
        self.configs = configs
        self.seq_len = configs['context_length'] + configs['prediction_length']
        self.enc_len = configs['context_length']
        self.dec_len = configs['prediction_length']

        self.static_embeddings = nn.ModuleList(
            [nn.Embedding(i, j) for i, j in zip(static_cardinality, static_embed_dims)])
        self.dynamic_embeddings = nn.ModuleList(
            [nn.Embedding(i, j) for i, j in zip(dynamic_cardinality, dynamic_embed_dims)])
        self.static_feat_num = len(static_cardinality)
        self.dynamic_feat_num = len(dynamic_cardinality)
        self.rho = 0.80
        enc_hidden_size = sum(static_embed_dims) * 2 + sum(dynamic_embed_dims) * 2 + self.enc_len * 2 + 2 * len(
            dynamic_embed_dims) * self.enc_len
        self.static_embed_dims = static_embed_dims
        self.dynamic_embed_dims = dynamic_embed_dims
        self.lc = nn.Linear(enc_hidden_size, self.dec_len)

        self.SELayers = nn.ModuleList([SELayer(enc_hidden_size) for _ in range(4)])

    def forward(self,
                batch_x,
                batch_seq_x_static_feat,
                batch_seq_y_static_feat,
                batch_seq_x_dynamic_feat,
                batch_seq_y_dynamic_feat):
        static_feats = torch.split(batch_seq_x_static_feat, split_size_or_sections=1, dim=-1)
        static_feats = [embed(feat.squeeze(dim=-1)) for feat, embed in zip(static_feats, self.static_embeddings)]
        static = torch.cat(static_feats, dim=1)
        static_feats = [batch_x[..., 0] * i for i in static_feats]

        start_feats = torch.split(batch_seq_y_dynamic_feat[:, 0, :], split_size_or_sections=1, dim=-1)
        start_feats = [embed(feat.squeeze(dim=-1)) for feat, embed in zip(start_feats, self.dynamic_embeddings)]
        start = torch.cat(start_feats, dim=1)
        start_feats = [batch_x[..., 0] * i for i in start_feats]

        end_feats = torch.split(batch_seq_y_dynamic_feat[:, -1, :], split_size_or_sections=1, dim=-1)
        end_feats = [embed(feat.squeeze(dim=-1)) for feat, embed in zip(end_feats, self.dynamic_embeddings)]
        end = torch.cat(end_feats, dim=1)
        end_feats = [batch_x[..., 0] * i for i in end_feats]

        inp = torch.cat([batch_x.mean(dim=1), batch_x[..., 0]], dim=1)
        inp = inp[:, 1:] - self.rho * inp[:, :-1]

        pred = torch.cat(
            [static, start, end, batch_x[..., 0], inp] + static_feats + end_feats + start_feats,
            dim=1)
        for layer in self.SELayers:
            pred = layer(pred)
        pred = self.lc(pred)

        return pred[..., None]


class SELayer(nn.Module):
    def __init__(self, channel, reduction=2):
        super(SELayer, self).__init__()
        self.fc = nn.Sequential(
            nn.Linear(channel, channel // reduction, bias=False),
            nn.ReLU(inplace=True),
            nn.Linear(channel // reduction, channel, bias=False),
            nn.Sigmoid()
        )

    def forward(self, x):
        y = self.fc(x)
        return x * y


class MeanScaler(nn.Module):
    """
    The ``MeanScaler`` computes a per-item scale according to the average
    absolute value over time of each item. The average is computed only among
    the observed values in the data tensor, as indicated by the second
    argument. Items with no observed data are assigned a scale based on the
    global average.

    Args:
        minimum_scale
            default scale that is used if the time series has only zeros.
    """

    def __init__(self, minimum_scale: float = 1e-10):
        super().__init__()
        self.register_buffer('minimum_scale', torch.as_tensor(minimum_scale))

    def forward(
            self, data: torch.Tensor
    ) -> Tuple[torch.Tensor, torch.Tensor]:
        scale = data.abs().mean(dim=1, keepdim=True)
        scale = torch.max(scale, self.minimum_scale).detach()
        return data / scale, scale


class EarlyStopping:
    """
    early stopping for the model training
    """

    def __init__(self, patience=7, verbose=False, delta=0):
        self.patience = patience
        self.verbose = verbose
        self.counter = 0
        self.best_score = None
        self.early_stop = False
        self.val_loss_min = np.Inf
        self.delta = delta

    def __call__(self, val_loss, model, path):
        score = -val_loss
        if self.best_score is None:
            self.best_score = score
            self.save_checkpoint(val_loss, model, path)
        elif score < self.best_score + self.delta:
            self.counter += 1
            print(f'EarlyStopping counter: {self.counter} out of {self.patience}')
            if self.counter >= self.patience:
                self.early_stop = True
        else:
            self.best_score = score
            self.save_checkpoint(val_loss, model, path)
            self.counter = 0

    def save_checkpoint(self, val_loss, model, path):
        if self.verbose:
            print(f'Validation loss decreased ({self.val_loss_min:.6f} --> {val_loss:.6f}).  Saving model ...')
        torch.save(model.state_dict(), path + '/' + 'checkpoint.pth')
        self.val_loss_min = val_loss


class Estimator(object):
    def __init__(self,
                 config,
                 logger,
                 model_path):
        """
        :param config: config of the data
        :param logger: logger
        :param model_path: path of model dir
        """
        super(Estimator, self).__init__()
        self.config = config
        assert self.config['freq'] in ['H', '1H', 'min', '1min', '10min', 'D',
                                       '1D'], "freq must be in ['H','1H','min','1min','10min','D','1D']"

        self.feat_cardinality_map = {
            'H': [24, 8, 32],
            '1H': [24, 8, 32],
            'min': [60, 24, 8, 32],
            '1min': [60, 24, 8, 32],
            '10min': [60, 24, 8, 32],
            'D': [8, 32],
            '1D': [8, 32]
        }

        self.dynamic_cardinality = self.feat_cardinality_map[self.config['freq']]
        self.dynamic_embed_dims = len(self.dynamic_cardinality) * [self.config['context_length']]

        self.model_dict = {
            'Fcnet': Fcnet,
        }
        self.logger = logger
        self.model_path = model_path
        self.scaler = MeanScaler()

    def build_model(self, num_series):
        self.static_cardinality = [num_series]  # * len(self.config['series_cols'])
        self.static_embed_dims = len(self.static_cardinality) * [self.config['context_length']]
        self.device = torch.device(
            'cuda'
            if torch.cuda.is_available()
            # else 'mps'
            # if torch.backends.mps.is_available()
            else 'cpu'
        )
        self.logger.info(f'Using device: {self.device}')
        self.model = self.model_dict[self.config['model']](
            configs=self.config,
            static_cardinality=self.static_cardinality,
            static_embed_dims=self.static_embed_dims,
            dynamic_cardinality=self.dynamic_cardinality,
            dynamic_embed_dims=self.dynamic_embed_dims
        ).float().to(self.device)

    def load_model(self, model_path):
        best_model_path = model_path + '/' + 'checkpoint.pth'
        self.model.load_state_dict(torch.load(best_model_path))

    def get_data(self,
                 flag,
                 df=None,
                 df_path=None):
        data_set, data_loader = self.data_provider(flag=flag,
                                                   df=df,
                                                   df_path=df_path)
        return data_set, data_loader

    def data_provider(self,
                      flag,
                      df=None,
                      df_path=None):
        if flag == 'test':
            shuffle_flag = False
            drop_last = False
            batch_size = self.config['batch_size']
        else:
            shuffle_flag = True
            drop_last = False
            batch_size = self.config['batch_size']

        data_set = TimeSeriesDataset(
            flag=flag,
            df=df,
            df_path=df_path,
            config=self.config,
            model_path=self.model_path,
        )
        self.logger.info(f'Dataset: {flag}, Size: {len(data_set)}')
        data_loader = DataLoader(
            data_set,
            batch_size=batch_size,
            shuffle=shuffle_flag,
            num_workers=self.config['num_workers'],
            drop_last=drop_last)

        return data_set, data_loader

    def train(self,
              train_loader,
              vali_loader):

        if not os.path.exists(self.model_path):
            os.makedirs(self.model_path)

        train_steps = len(train_loader)
        early_stopping = EarlyStopping(patience=self.config['patience'], verbose=True)

        model_optim = optim.Adam(self.model.parameters(), lr=self.config['learning_rate'])
        criterion = nn.L1Loss(reduction='mean')

        for epoch in range(self.config['epochs']):
            iter_count = 0
            train_loss = []

            self.model.train()
            epoch_time = time.time()
            for i, (batch_x,
                    batch_y,
                    batch_seq_x_static_feat,
                    batch_seq_y_static_feat,
                    batch_seq_x_dynamic_feat,
                    batch_seq_y_dynamic_feat) in enumerate(train_loader):
                iter_count += 1
                model_optim.zero_grad()
                _, scale = self.scaler(batch_x)
                batch_x = batch_x / scale
                batch_y = batch_y / scale
                batch_x = batch_x.float().to(self.device)
                batch_y = batch_y.float().to(self.device)

                batch_seq_x_static_feat = batch_seq_x_static_feat.long().to(self.device)
                batch_seq_y_static_feat = batch_seq_y_static_feat.long().to(self.device)
                batch_seq_x_dynamic_feat = batch_seq_x_dynamic_feat.long().to(self.device)
                batch_seq_y_dynamic_feat = batch_seq_y_dynamic_feat.long().to(self.device)

                outputs = self.model(batch_x=batch_x,
                                     batch_seq_x_static_feat=batch_seq_x_static_feat,
                                     batch_seq_y_static_feat=batch_seq_y_static_feat,
                                     batch_seq_x_dynamic_feat=batch_seq_x_dynamic_feat,
                                     batch_seq_y_dynamic_feat=batch_seq_y_dynamic_feat)

                # loss = criterion(outputs*scale.to(self.device), batch_y*scale.to(self.device))
                loss = criterion(outputs, batch_y)
                train_loss.append(loss.item())

                loss.backward()
                model_optim.step()

            self.logger.info(f'Epoch: {epoch + 1} cost time: {time.time() - epoch_time}')
            train_loss = np.average(train_loss)
            if self.config['use_validation']:
                vali_loss = self.vali(vali_loader, criterion)
                early_stopping(vali_loss, self.model, self.model_path)
            else:
                early_stopping(train_loss, self.model, self.model_path)

            self.logger.info(f'Epoch: {epoch + 1}, Steps: {train_steps} | Train Loss: {train_loss:.7f}')

            if early_stopping.early_stop:
                self.logger.info('Early stopping')
                break

        return self.model

    def vali(self,
             vali_loader,
             criterion):
        total_loss = []
        self.model.eval()
        with torch.no_grad():
            for i, (batch_x,
                    batch_y,
                    batch_seq_x_static_feat,
                    batch_seq_y_static_feat,
                    batch_seq_x_dynamic_feat,
                    batch_seq_y_dynamic_feat) in enumerate(vali_loader):
                _, scale = self.scaler(batch_x)
                batch_x = batch_x / scale
                batch_y = batch_y / scale
                batch_x = batch_x.float().to(self.device)
                batch_y = batch_y.float()

                batch_seq_x_static_feat = batch_seq_x_static_feat.long().to(self.device)
                batch_seq_y_static_feat = batch_seq_y_static_feat.long().to(self.device)
                batch_seq_x_dynamic_feat = batch_seq_x_dynamic_feat.long().to(self.device)
                batch_seq_y_dynamic_feat = batch_seq_y_dynamic_feat.long().to(self.device)

                outputs = self.model(batch_x=batch_x,
                                     batch_seq_x_static_feat=batch_seq_x_static_feat,
                                     batch_seq_y_static_feat=batch_seq_y_static_feat,
                                     batch_seq_x_dynamic_feat=batch_seq_x_dynamic_feat,
                                     batch_seq_y_dynamic_feat=batch_seq_y_dynamic_feat)

                pred = outputs.detach().cpu()
                true = batch_y.detach().cpu()
                scale = scale.detach().cpu()

                loss = criterion(pred * scale, true * scale)

                total_loss.append(loss)
        total_loss = np.average(total_loss)
        self.model.train()
        return total_loss

    def test(self, test_loader):
        preds = []

        self.model.eval()
        with torch.no_grad():
            for i, (batch_x,
                    batch_y,
                    batch_seq_x_static_feat,
                    batch_seq_y_static_feat,
                    batch_seq_x_dynamic_feat,
                    batch_seq_y_dynamic_feat) in enumerate(test_loader):
                _, scale = self.scaler(batch_x)
                batch_x = batch_x / scale
                batch_x = batch_x.float().to(self.device)

                batch_seq_x_static_feat = batch_seq_x_static_feat.long().to(self.device)
                batch_seq_y_static_feat = batch_seq_y_static_feat.long().to(self.device)
                batch_seq_x_dynamic_feat = batch_seq_x_dynamic_feat.long().to(self.device)
                batch_seq_y_dynamic_feat = batch_seq_y_dynamic_feat.long().to(self.device)

                outputs = self.model(batch_x=batch_x,
                                     batch_seq_x_static_feat=batch_seq_x_static_feat,
                                     batch_seq_y_static_feat=batch_seq_y_static_feat,
                                     batch_seq_x_dynamic_feat=batch_seq_x_dynamic_feat,
                                     batch_seq_y_dynamic_feat=batch_seq_y_dynamic_feat)
                outputs = outputs * (scale.float().to(self.device))
                pred = outputs.detach().cpu()

                preds.append(pred)
        preds = np.concatenate(preds, axis=0)

        return preds

    def predict(self,
                df,
                freq,
                target_col,
                time_col,
                series_cols_dict):
        query_df, ctx_end_date = self.pre_processing_query(query=df,
                                                           time_col=time_col,
                                                           target_col=target_col,
                                                           series_cols_dict=series_cols_dict,
                                                           freq=freq)
        test_data, test_loader = self.get_data(
            flag='test',
            df=query_df,
            df_path=None)
        pred = self.test(test_loader=test_loader)
        result = self.post_processing_result(pred=pred,
                                             ctx_end_date=ctx_end_date,
                                             time_col=time_col,
                                             target_col=target_col,
                                             freq=freq)
        return result

    def pre_processing_query(self,
                             query,
                             time_col,
                             target_col,
                             series_cols_dict,
                             freq):
        test_df = query.sort_values([time_col]).reset_index(drop=True)
        ctx_end_date = test_df[time_col].iat[-1]

        pred_len = self.config['prediction_length']
        ctx_end_date = pd.to_datetime(int(ctx_end_date), unit='s')
        for i in range(1, pred_len + 1):
            if freq in ['H', '1H']:
                date = pd.to_datetime(ctx_end_date + datetime.timedelta(hours=i)).value / 1e9
            elif freq in ['10min']:
                date = pd.to_datetime(ctx_end_date + 10 * datetime.timedelta(minutes=i)).value / 1e9
            elif freq in ['1min', 'min']:
                date = pd.to_datetime(ctx_end_date + datetime.timedelta(minutes=i)).value / 1e9
            elif freq in ['D', '1D']:
                date = pd.to_datetime(ctx_end_date + datetime.timedelta(days=i)).value / 1e9
            else:
                date = pd.to_datetime(ctx_end_date).value / 1e9
            test_df = pd.concat([test_df, pd.DataFrame([{time_col: int(date), target_col: 0}])], ignore_index=True)

        for k, v in series_cols_dict.items():
            test_df[k] = v

        return test_df, ctx_end_date

    def post_processing_result(self,
                               pred,
                               ctx_end_date,
                               time_col,
                               target_col,
                               freq):
        result_df = pd.DataFrame()
        pred_len = self.config['prediction_length']
        for i in range(1, pred_len + 1):
            if freq in ['H', '1H']:
                date = pd.to_datetime(ctx_end_date + datetime.timedelta(hours=i)).value / 1e9
            elif freq in ['10min']:
                date = pd.to_datetime(ctx_end_date + 10 * datetime.timedelta(minutes=i)).value / 1e9
            elif freq in ['1min', 'min']:
                date = pd.to_datetime(ctx_end_date + datetime.timedelta(minutes=i)).value / 1e9
            elif freq in ['D', '1D']:
                date = pd.to_datetime(ctx_end_date + 10 * datetime.timedelta(hours=i)).value / 1e9
            else:
                date = ctx_end_date
            result_df = pd.concat([result_df,
                                   pd.DataFrame([{time_col: int(date), target_col: float(pred[0, i - 1, 0])}])],
                                  ignore_index=True)
        return result_df


# train model
def fit(freq: str,
        target_col: str,
        time_col: str,
        series_cols: List[str],
        prediction_length: int,
        context_length: int,
        learning_rate: float = 1e-3,
        epochs: int = 100,
        batch_size: int = 1024,
        model_path: str = './',
        df: pd.DataFrame = None,
        df_path: str = None):
    """
    :param freq: the freq of the time series
    :param target_col: the predict target of the time series
    :param time_col: the timestamp column of the time series
    :param series_cols: the item corresponds to one unique series
    :param prediction_length: the prediction length of the time series
    :param context_length: the context length of the time series
    :param learning_rate: the learning rate of the time series, default 1e-3
    :param epochs: the training epochs of the time series default
    :param batch_size: the batch size for the time series
    :param model_path: the dir path for saving model at
    :param df: the dataframe of data
    :param df_path: the path of data, should be csv file
    :return:
    """
    # init logger
    logging.basicConfig(level=logging.INFO,
                        format='%(asctime)s - %(levelname)s: %(message)s')
    logger = logging.getLogger()

    # init estimator
    config = {
        'freq': freq,
        'target_col': target_col,
        'time_col': time_col,
        'series_cols': series_cols,
        'prediction_length': prediction_length,
        'context_length': context_length,
        'learning_rate': learning_rate,
        'epochs': epochs,
        'batch_size': batch_size,
        'model_path': model_path,
        'model': 'Fcnet',
        'loss': 'MSE',
        'num_workers': 10,
        'patience': 15,
        'use_validation': False,
        'val_num': 0,
    }
    estimator = Estimator(config=config,
                          logger=logger,
                          model_path=model_path)

    # init dataset
    train_data, train_loader = estimator.get_data(
        flag='train',
        df=df,
        df_path=df_path)
    vali_data, vali_loader = estimator.get_data(
        flag='val',
        df=df,
        df_path=df_path)

    # save estimator config
    config['num_series'] = train_data.num_series
    with open(os.path.join(model_path, 'estimator_config.yaml'), 'w') as f:
        yaml.dump(config, f)

    # train and save model
    estimator.build_model(num_series=config['num_series'])
    estimator.train(
        train_loader=train_loader,
        vali_loader=vali_loader)
    return


# load model with estimator
def load_model(model_path: str):
    """
    :param model_path: path of model dir
    :return: the estimator
    """
    logging.basicConfig(level=logging.INFO,
                        format='%(asctime)s - %(levelname)s: %(message)s')
    logger = logging.getLogger()

    with open(os.path.join(model_path, 'estimator_config.yaml'), 'r') as f:
        config = yaml.safe_load(f)

    estimator = Estimator(config=config,
                          logger=logger,
                          model_path=model_path)
    estimator.build_model(num_series=config['num_series'])
    estimator.load_model(model_path=model_path)

    return estimator


def train_main():
    model_path = './model'
    # TODO: support auto building training df by fetching metrics server
    df = pd.DataFrame(columns=['timestamp', 'value'])
    fit(
        df=df,
        freq='10min',
        target_col='value',
        time_col='timestamp',
        series_cols=['workload', 'metric'],
        prediction_length=12,
        context_length=12,
        learning_rate=0.001,
        epochs=100,
        batch_size=32,
        model_path=model_path)
    return


if __name__ == '__main__':
    train_main()
