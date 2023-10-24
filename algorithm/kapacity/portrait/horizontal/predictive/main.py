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
import json
import os
import datetime
import pandas as pd

from kubernetes import client, config, utils

import kapacity.portrait.horizontal.predictive.replicas_estimator as estimator
import kapacity.timeseries.forecasting.forecaster as forecaster
import kapacity.metric.query as query


class EnvInfo:
    metrics_server_addr = None
    namespace = None
    hp_name = None


class MetricsContext:
    workload_identifier = None
    resource_name = None
    resource_target = 0
    resource_history = None
    replicas_history = None
    traffics_history_dict = {}


def main():
    # 1. parse arguments
    args = parse_args()
    env = read_env()
    hp_cr = read_hp_cr(args, env.namespace, env.hp_name)

    # 2. fetch all history metrics needed
    metrics_ctx = fetch_metrics_history(args, env, hp_cr)

    # 3. predict every traffic metric
    pred_traffics = predict_traffics(args, metrics_ctx)

    # 4. predict replicas
    pred_replicas = predict_replicas(args, metrics_ctx, pred_traffics)

    # 5. resample predict replicas by scaling frequency
    pred_replicas_by_freq = resample_by_freq(pred_replicas[['timestamp', 'pred_replicas']],
                                             args.scaling_freq,
                                             {'pred_replicas': 'max'})

    # 6. write result to configmap
    write_pred_replicas_to_config_map(args, env, hp_cr, pred_replicas_by_freq)

    return


def parse_args():
    parser = argparse.ArgumentParser(description='args of Kapacity predictive horizontal portrait algorithm job')
    parser.add_argument('--kubeconfig', help='location of kubeconfig file, will use in-cluster client if not set',
                        required=False)
    parser.add_argument('--tsf-model-path',
                        help='dir path containing related files of the time series forecasting model',
                        required=True, default='/opt/kapacity/timeseries/forcasting/model')
    parser.add_argument('--tsf-freq', help='frequency (precision) of the time series forecasting model,'
                                           'should be the same as set for training', required=True)
    parser.add_argument('--tsf-dataloader-num-workers', help='number of worker subprocesses to use for data loading'
                                                             'of the time series forecasting model',
                        required=False, default=0)
    parser.add_argument('--re-history-len', help='history length of training data for replicas estimation',
                        required=True)
    parser.add_argument('--re-time-delta-hours', help='time zone offset for replicas estimation model',
                        required=True)
    parser.add_argument('--re-test-dataset-size-in-seconds',
                        help='size of test dataset in seconds for replicas estimation model',
                        required=False, default=86400)
    parser.add_argument('--scaling-freq', help='frequency of scaling, the duration should be larger than tsf-freq',
                        required=True)
    args = parser.parse_args()
    return args


def read_env():
    env = EnvInfo()
    env.metrics_server_addr = os.getenv('METRICS_SERVER_ADDR')
    env.namespace = os.getenv('HP_NAMESPACE')
    env.hp_name = os.getenv('HP_NAME')
    return env


def predict_traffics(args, metric_ctx):
    model = forecaster.load_model(args.tsf_model_path, int(args.tsf_dataloader_num_workers))
    freq = model.config['freq']
    context_length = model.config['context_length']
    df = None
    for key in metric_ctx.traffics_history_dict:
        traffic_history = resample_by_freq(metric_ctx.traffics_history_dict[key], freq, {'value': 'mean'})
        traffic_history = traffic_history[len(traffic_history) - context_length:len(traffic_history)]
        df_traffic = model.predict(df=traffic_history,
                                   freq=args.tsf_freq,
                                   target_col='value',
                                   time_col='timestamp',
                                   series_cols_dict={'workload': metric_ctx.workload_identifier, 'metric': key})
        df_traffic.rename(columns={'value': key}, inplace=True)
        if df is None:
            df = df_traffic
        else:
            df = pd.merge(left=df, right=df_traffic, on='timestamp')
    return df


def predict_replicas(args, metric_ctx, pred_traffics):
    history = merge_history_dict(metric_ctx.traffics_history_dict)
    history = pd.merge(left=history, right=metric_ctx.resource_history, on='timestamp')
    history = pd.merge(left=history, right=metric_ctx.replicas_history, on='timestamp')
    traffic_col = list(metric_ctx.traffics_history_dict.keys())

    try:
        pred = estimator.estimate(history,
                                  pred_traffics,
                                  'timestamp',
                                  metric_ctx.resource_name,
                                  'replicas',
                                  traffic_col,
                                  metric_ctx.resource_target,
                                  int(args.re_time_delta_hours),
                                  int(args.re_test_dataset_size_in_seconds))
        if 'NO_RESULT' in pred['rule_code'].unique():
            raise RuntimeError('there exist points that no replica number would meet the resource target, please consider setting a more reasonable resource target')
        return pred
    except estimator.EstimationException as e:
        raise RuntimeError("replicas estimation failed, this may be caused by insufficient or irregular history data") from e


def merge_history_dict(history_dict):
    df = None
    for key in history_dict:
        if df is None:
            df = history_dict[key].rename(columns={'value': key})
        else:
            df1 = history_dict[key].rename(columns={'value': key})
            df = pd.merge(left=df, right=df1, on='timestamp')
    return df


def resample_by_freq(old_df, freq, agg_funcs):
    df = old_df.copy()
    df = df.sort_values(by='timestamp', ascending=True)
    df['timestamp'] = pd.to_datetime(df['timestamp'], unit='s')
    df = df.resample(rule=freq, on='timestamp').agg(agg_funcs)
    df = df.rename_axis('timestamp').reset_index()
    df['timestamp'] = df['timestamp'].astype('int64') // 10 ** 9
    return df


def fetch_metrics_history(args, env, hp_cr):
    # fetch the start time and end time for metrics
    start, end = query.compute_history_range(args.re_history_len)

    # read horizontal portrait cr
    metrics_spec = hp_cr['spec']['metrics']
    scale_target = hp_cr['spec']['scaleTargetRef']

    metric_ctx = MetricsContext()
    metric_ctx.workload_identifier = '%s/%s' % (env.namespace, scale_target['name'])
    metric_ctx.traffics_history_dict = {}
    for i in range(len(metrics_spec)):
        metric = metrics_spec[i]
        metric_type = metric['type']
        if i == 0:
            if metric_type == 'Resource':
                resource = metric['resource']
            elif metric_type == 'ContainerResource':
                resource = metric['containerResource']
            else:
                raise RuntimeError('MetricTypeError')
            resource_history = query.fetch_metrics(env.metrics_server_addr, env.namespace, metric, scale_target, start, end)
            metric_ctx.resource_name = resource['name']
            metric_ctx.resource_target = compute_resource_target(env.namespace, resource, scale_target)
            metric_ctx.resource_history = resource_history.rename(columns={'value': resource['name']})
        elif i == 1:
            if metric_type != 'External':
                raise RuntimeError('MetricTypeError')
            replica_history = query.fetch_replicas_metric_history(env.metrics_server_addr, env.namespace, metric,
                                                                  scale_target, start, end)
            metric_ctx.replicas_history = replica_history.rename(columns={'value': 'replicas'})
        else:
            if metric_type == 'Object':
                metric_name = metric['object']['metric']['name']
            elif metric_type == 'External':
                metric_name = metric['external']['metric']['name']
            else:
                raise RuntimeError('MetricTypeError')
            traffic_history = query.fetch_metrics(env.metrics_server_addr, env.namespace, metric, scale_target, start, end)
            metric_ctx.traffics_history_dict[metric_name] = traffic_history

    return metric_ctx


def compute_resource_target(namespace, resource, scale_target):
    target = resource['target']
    resource_name = resource['name']

    if target['type'] == 'Value':
        raise RuntimeError('UnsupportedResourceTargetType')
    elif target['type'] == 'AverageValue':
        return target['averageValue']
    elif target['type'] == 'Utilization':
        return (target['averageUtilization'] / 100) * fetch_workload_pod_request(namespace, resource_name, scale_target)


def fetch_workload_pod_request(namespace, resource_name, scale_target):
    app_api = client.AppsV1Api()
    core_api = client.CoreV1Api()

    # TODO: use general scale api to support arbitrary workload
    if scale_target['kind'] == 'ReplicaSet':
        scale = app_api.read_namespaced_replica_set_scale(namespace=namespace, name=scale_target['name'])
    elif scale_target['kind'] == 'Deployment':
        scale = app_api.read_namespaced_deployment_scale(namespace=namespace, name=scale_target['name'])
    elif scale_target['kind'] == 'StatefulSet':
        scale = app_api.read_namespaced_stateful_set_scale(namespace=namespace, name=scale_target['name'])
    else:
        raise RuntimeError('UnsupportedWorkloadType')

    ret = core_api.list_namespaced_pod(namespace=namespace, label_selector=scale.status.selector)
    if len(ret.items) == 0:
        raise RuntimeError('PodNotFound')

    total_request = 0
    for container in ret.items[0].to_dict()['spec']['containers']:
        total_request += utils.parse_quantity(container['resources']['requests'][resource_name])
    if total_request == 0:
        raise RuntimeError('PodRequestInvalid')
    return float(total_request)


def read_hp_cr(args, namespace, name):
    if args.kubeconfig:
        config.load_kube_config(config_file=args.kubeconfig)
    else:
        config.incluster_config.load_incluster_config()
    api = client.CustomObjectsApi()
    hp_cr = api.get_namespaced_custom_object(group='autoscaling.kapacitystack.io',
                                             version='v1alpha1',
                                             namespace=namespace,
                                             plural='horizontalportraits',
                                             name=name)
    return hp_cr


def write_pred_replicas_to_config_map(args, env, hp_cr, pred_replicas):
    last_timestamp = pred_replicas.iloc[len(pred_replicas) - 1, 0]
    content = pred_replicas.set_index('timestamp', drop=True).to_dict()['pred_replicas']
    expire_time = parse_timestamp_to_rfc3339(
        datetime.datetime.utcfromtimestamp(last_timestamp) + datetime.timedelta(
            minutes=query.time_period_to_minutes(args.scaling_freq)))
    cmap_namespace = env.namespace
    cmap_name = env.hp_name + '-result'

    api = client.CoreV1Api()
    try:
        cmap = api.read_namespaced_config_map(namespace=cmap_namespace, name=cmap_name)
    except client.ApiException as e:
        if e.status == 404:
            cmap = client.V1ConfigMap()
            owner_refs = [client.V1OwnerReference(kind='HorizontalPortrait',
                                                  api_version='autoscaling.kapacitystack.io/v1alpha1',
                                                  name=env.hp_name,
                                                  uid=hp_cr['metadata']['uid'],
                                                  controller=True,
                                                  block_owner_deletion=True)]
            cmap.metadata = client.V1ObjectMeta(namespace=cmap_namespace, name=cmap_name, owner_references=owner_refs)
            cmap.data = {'type': 'TimeSeries', 'expireTime': expire_time, 'timeSeries': json.dumps(content)}
            api.create_namespaced_config_map(namespace=cmap_namespace, body=cmap)
    else:
        cmap.data = {'type': 'TimeSeries', 'expireTime': expire_time, 'timeSeries': json.dumps(content)}
        api.replace_namespaced_config_map(namespace=cmap_namespace, name=cmap_name, body=cmap)
    return


def parse_timestamp_to_rfc3339(t):
    return t.strftime('%Y-%m-%dT%H:%M:%S.%f')[:-4] + 'Z'


if __name__ == '__main__':
    main()
