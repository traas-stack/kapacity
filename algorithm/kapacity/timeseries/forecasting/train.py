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


import argparse
import yaml
import datetime
import pandas as pd
from google.protobuf import timestamp_pb2

import kapacity.portrait.horizontal.predictive.main as pred_main
import kapacity.timeseries.forecasting.forecaster as forecaster


def main():
    # 1. parse arguments
    args = parse_args()

    # 2. fetch train config
    config = load_config(args.train_config_file)

    # 3. fetch training data set
    df = fetch_train_data(args, config)

    # 4. train the model using data sets
    train(args, config, df)

    return


def parse_args():
    parser = argparse.ArgumentParser(description='args of timeseries forcasting training')
    parser.add_argument('--train-data-file', help='training data set file', required=False)
    parser.add_argument('--train-config-file', help='training profile', required=False)
    parser.add_argument('--metrics-server-addr', help='address of the gRPC metrics provider server', required=False)
    parser.add_argument('--model-path', help='The directory where the model is stored', required=True)

    args = parser.parse_args()
    return args


def train(args, config, df):
    return forecaster.fit(
        df=df,
        freq=config['freq'],
        target_col='value',
        time_col='timestamp',
        series_cols=['workload', 'metric'],
        prediction_length=config['predictionLength'],
        context_length=config['contextLength'],
        learning_rate=config['contextLength'],
        epochs=config['hyperParams']['epochs'],
        batch_size=config['hyperParams']['batchSize'],
        model_path=args.model_path)


def load_config(train_config_file):
    with open(train_config_file, 'r') as f:
        return yaml.safe_load(f)


def fetch_train_data(args, config):
    if args.train_data_file is not None:
        df = pd.read_csv(args.train_data_file)
    else:
        df = pd.DataFrame(columns=['timestamp', 'value', 'workload', 'metric'])
        for i in range(len(config['targets'])):
            target = config['targets'][i]
            df_target = fetch_traffic_metrics(args, target)
            df = pd.concat([df, df_target])
    return df


def compute_history_range(config):
    now = datetime.datetime.now()
    history_in_minutes = pred_main.time_period_to_minutes(config['historyLength'])
    ago = (now - datetime.timedelta(minutes=history_in_minutes))
    start = timestamp_pb2.Timestamp()
    start.FromSeconds(int(ago.timestamp()))
    end = timestamp_pb2.Timestamp()
    end.FromSeconds(int(now.timestamp()))
    return start, end


def fetch_traffic_metrics(args, target):
    df_traffic = pd.DataFrame(columns=['timestamp', 'value', 'workload', 'metric'])
    start, end = compute_history_range(target)

    for j in range(len(target['metrics'])):
        metric = target['metrics'][j]
        workload_namespace = target['workloadNamespace']
        workload_name = target['workloadName']

        if metric['type'] == 'Object':
            df = pred_main.fetch_object_metric_history(args=args,
                                                       namespace=workload_namespace,
                                                       metric=metric,
                                                       start=start,
                                                       end=end)

            df['metric'] = metric['object']['metric']['name']

        elif metric['type'] == 'External':
            df = pred_main.fetch_external_metric_history(args=args,
                                                         namespace=workload_namespace,
                                                         metric=metric,
                                                         start=start,
                                                         end=end)
            df['metric'] = metric['external']['metric']['name']
        else:
            raise RuntimeError('UnsupportedMetricType')

        df['workload'] = '%s/%s' % (workload_namespace, workload_name)
        df_traffic = pd.concat([df, df_traffic])
    return df_traffic


if __name__ == '__main__':
    main()
