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
import kapacity.metric.query as query
import kapacity.timeseries.forecasting.forecaster as forecaster
import pandas as pd
import yaml


def main():
    # 1. parse arguments
    args = parse_args()

    # 2. load training config
    config = load_config(args.config_file)

    # 3. fetch training dataset
    df = fetch_train_data(args, config)

    # 4. train the model
    train(args, config, df)

    return


def parse_args():
    parser = argparse.ArgumentParser(description='args of timeseries forcasting training')
    parser.add_argument('--config-file', help='path of training config file', required=True)
    parser.add_argument('--model-save-path', help='dir path where the model and its related files would be saved in',
                        required=True)
    parser.add_argument('--dataset-file', help='path of training dataset file, if set, will load dataset from this '
                                               'file instead of fetching from metrics server', required=False)
    parser.add_argument('--metrics-server-addr', help='address of the gRPC metrics provider server', required=False)
    parser.add_argument('--dataloader-num-workers', help='number of worker subprocesses to use for data loading',
                        required=False, default=0)

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
        learning_rate=config['hyperParams']['learningRate'],
        epochs=config['hyperParams']['epochs'],
        batch_size=config['hyperParams']['batchSize'],
        num_workers=int(args.dataloader_num_workers),
        model_path=args.model_save_path)


def load_config(config_file):
    with open(config_file, 'r') as f:
        return yaml.safe_load(f)


def fetch_train_data(args, config):
    if args.dataset_file is not None:
        df = pd.read_csv(args.dataset_file)
    else:
        df = pd.DataFrame(columns=['timestamp', 'value', 'workload', 'metric'])
        for i in range(len(config['targets'])):
            target = config['targets'][i]
            df_target = fetch_history_metrics(args, target)
            df = pd.concat([df, df_target])
    return df


def fetch_history_metrics(args, target):
    df = pd.DataFrame(columns=['timestamp', 'value', 'workload', 'metric'])
    start, end = query.compute_history_range(target['historyLength'])

    for i in range(len(target['metrics'])):
        metric = target['metrics'][i]
        if metric['type'] != 'Object' and metric['type'] != 'External':
            raise RuntimeError('UnsupportedMetricType')
        workload_namespace = target['workloadNamespace']
        workload_name = target['workloadName']
        df_metric = query.fetch_metrics(addr=args.metrics_server_addr,
                                        namespace=workload_namespace,
                                        metric=metric,
                                        start=start,
                                        end=end)
        df_metric['metric'] = metric['name']
        df_metric['workload'] = '%s/%s' % (workload_namespace, workload_name)
        df = pd.concat([df, df_metric])
    return df


if __name__ == '__main__':
    main()
