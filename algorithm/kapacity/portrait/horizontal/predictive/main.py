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

import grpc
from kubernetes import client, config, utils
from google.protobuf import timestamp_pb2, duration_pb2

import kapacity.portrait.horizontal.predictive.replicas_estimator as estimator
import kapacity.timeseries.forecasting.forecaster as forecaster
import kapacity.portrait.horizontal.predictive.metrics.metric_pb2 as metric_pb
import kapacity.portrait.horizontal.predictive.metrics.provider_pb2 as provider_pb
import kapacity.portrait.horizontal.predictive.metrics.provider_pb2_grpc as provider_pb_grpc


class EnvInfo:
    namespace = None
    pod_name = None
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
    parser.add_argument('--metrics-server-addr', help='address of the gRPC metrics provider server',
                        required=True)
    parser.add_argument('--tsf-model-path',
                        help='dir path containing related files of the time series forecasting model',
                        required=True, default='/opt/kapacity/timeseries/forcasting/model')
    parser.add_argument('--tsf-freq', help='frequency (precision) of the time series forecasting model,'
                                           'should be the same as set for training', required=True)
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
    env.namespace = os.getenv('POD_NAMESPACE')
    env.pod_name = os.getenv('POD_NAME')
    env.hp_name = env.pod_name[0:env.pod_name.rindex('-', 0, env.pod_name.rindex('-'))]
    return env


def predict_traffics(args, metric_ctx):
    model = forecaster.load_model(args.tsf_model_path)
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

    pred = estimator.estimate(history,
                              pred_traffics,
                              'timestamp',
                              metric_ctx.resource_name,
                              'replicas',
                              traffic_col,
                              metric_ctx.resource_target,
                              int(args.re_time_delta_hours),
                              int(args.re_test_dataset_size_in_seconds))
    return pred


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
    start, end = compute_history_range(args)

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

            resource_history = fetch_metrics(args, env, metric, scale_target, start, end)
            metric_ctx.resource_name = resource['name']
            metric_ctx.resource_target = compute_resource_target(env.namespace, resource, scale_target)
            metric_ctx.resource_history = resource_history.rename(columns={'value': resource['name']})
        elif i == 1:
            if metric_type != 'External':
                raise RuntimeError('MetricTypeError')

            replica_history = fetch_replicas_metric_history(args, env.namespace, metric, scale_target, start, end)
            metric_ctx.replicas_history = replica_history.rename(columns={'value': 'replicas'})
        else:
            if metric_type == 'Object':
                metric_name = metric['object']['metric']['name']
            elif metric_type == 'External':
                metric_name = metric['external']['metric']['name']
            else:
                raise RuntimeError('MetricTypeError')
            traffic_history = fetch_metrics(args, env, metric, scale_target, start, end)
            metric_ctx.traffics_history_dict[metric_name] = traffic_history

    return metric_ctx


def fetch_metrics(args, env, metric, scale_target, start, end):
    metric_type = metric['type']
    if metric_type == 'Resource':
        # FIXME: we should fetch resource history for ready pods only to ensure the accuracy of replicas estimation
        return fetch_resource_metric_history(args=args,
                                             namespace=env.namespace,
                                             metric=metric,
                                             scale_target=scale_target,
                                             start=start,
                                             end=end)
    elif metric_type == 'ContainerResource':
        # FIXME: ditto
        return fetch_container_resource_metric_history(args=args,
                                                       namespace=env.namespace,
                                                       metric=metric,
                                                       scale_target=scale_target,
                                                       start=start,
                                                       end=end)
    elif metric_type == 'Pods':
        # TODO: support pods metric type
        raise RuntimeError('UnsupportedMetricType')
    elif metric_type == 'Object':
        return fetch_object_metric_history(args=args,
                                           namespace=env.namespace,
                                           metric=metric,
                                           start=start,
                                           end=end)
    elif metric_type == 'External':
        return fetch_external_metric_history(args=args,
                                             namespace=env.namespace,
                                             metric=metric,
                                             start=start,
                                             end=end)
    else:
        raise RuntimeError('UnsupportedMetricType')


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


def compute_history_range(args):
    now = datetime.datetime.now()
    ago = (now - datetime.timedelta(minutes=time_period_to_minutes(args.re_history_len)))

    start = timestamp_pb2.Timestamp()
    start.FromSeconds(int(ago.timestamp()))
    end = timestamp_pb2.Timestamp()
    end.FromSeconds(int(now.timestamp()))
    return start, end


def fetch_replicas_metric_history(args, namespace, metric, scale_target, start, end):
    external = metric['external']
    metric_identifier = build_metric_identifier(external['metric'])
    name, group_kind = get_obj_name_and_group_kind(scale_target)
    workload_external = metric_pb.WorkloadExternalQuery(group_kind=group_kind,
                                                        namespace=namespace,
                                                        name=name,
                                                        metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.WORKLOAD_EXTERNAL,
                            workload_external=workload_external)
    return query_metrics(args=args, query=query, start=start, end=end)


def fetch_resource_metric_history(args, namespace, metric, scale_target, start, end):
    resource_name = metric['resource']['name']
    name, group_kind = get_obj_name_and_group_kind(scale_target)
    workload_resource = metric_pb.WorkloadResourceQuery(group_kind=group_kind,
                                                        namespace=namespace,
                                                        name=name,
                                                        resource_name=resource_name,
                                                        ready_pods_only=True)
    query = metric_pb.Query(type=metric_pb.WORKLOAD_RESOURCE,
                            workload_resource=workload_resource)
    return query_metrics(args=args, query=query, start=start, end=end)


def fetch_container_resource_metric_history(args, namespace, metric, scale_target, start, end):
    container_resource = metric['containerResource']
    resource_name = container_resource['name']
    container_name = container_resource['container']
    name, group_kind = get_obj_name_and_group_kind(scale_target)
    workload_container_resource = metric_pb.WorkloadContainerResourceQuery(group_kind=group_kind,
                                                                           namespace=namespace,
                                                                           name=name,
                                                                           resource_name=resource_name,
                                                                           container_name=container_name,
                                                                           ready_pods_only=True)
    query = metric_pb.Query(type=metric_pb.WORKLOAD_CONTAINER_RESOURCE,
                            workload_container_resource=workload_container_resource)
    return query_metrics(args=args, query=query, start=start, end=end)


def fetch_object_metric_history(args, namespace, metric, start, end):
    obj = metric['object']
    metric_identifier = build_metric_identifier(obj['metric'])
    name, group_kind = get_obj_name_and_group_kind(obj['describedObject'])
    object_query = metric_pb.ObjectQuery(namespace=namespace,
                                         name=name,
                                         group_kind=group_kind,
                                         metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.OBJECT,
                            object=object_query)
    return query_metrics(args=args, query=query, start=start, end=end)


def fetch_external_metric_history(args, namespace, metric, start, end):
    external = metric['external']
    metric_identifier = build_metric_identifier(external['metric'])
    external_query = metric_pb.ExternalQuery(namespace=namespace,
                                             metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.EXTERNAL,
                            external=external_query)
    return query_metrics(args=args, query=query, start=start, end=end)


def build_metric_identifier(metric):
    metric_name, metric_selector = None, None
    if 'name' in metric:
        metric_name = metric['name']
    elif 'selector' in metric:
        metric_selector = metric['selector']
    return metric_pb.MetricIdentifier(name=metric_name,
                                      selector=metric_selector)


def get_obj_name_and_group_kind(obj):
    name = obj['name']
    group = obj['apiVersion'].split('/')[0]
    kind = obj['kind']
    return name, metric_pb.GroupKind(group=group, kind=kind)


def time_period_to_minutes(time_period):
    minutes = 0
    if time_period.find('D') != -1:
        if len(time_period) == 1:
            minutes = 24 * 60
        else:
            minutes = int(time_period.split('D')[0]) * 1440
    elif time_period.find('H') != -1:
        if len(time_period) == 1:
            minutes = 60
        else:
            minutes = int(time_period.split('H')[0]) * 60
    elif time_period.find('min') != -1:
        if len(time_period) == 1:
            minutes = 1
        else:
            minutes = int(time_period.split('min')[0])
    return minutes


def query_metrics(args, query, start, end):
    step = duration_pb2.Duration()
    step.FromSeconds(60)
    query_request = provider_pb.QueryRequest(query=query,
                                             start=start,
                                             end=end,
                                             step=step)
    with grpc.insecure_channel(args.metrics_server_addr) as channel:
        stub = provider_pb_grpc.ProviderServiceStub(channel)
        response = stub.Query(query_request)
    return convert_metric_series_to_dataframe(response.series)


def convert_metric_series_to_dataframe(series):
    dataframe = None
    for item in series:
        array = []
        for point in item.points:
            array.append([point.timestamp, point.value])
        df = pd.DataFrame(array, columns=['timestamp', 'value'], dtype=float)
        df['timestamp'] = df['timestamp'].map(lambda x: x / 1000).astype('int64')
        if dataframe is not None:
            # TODO: consider if it's possible to have multiple series
            pd.merge(dataframe, df, how='left', on='timestamp')
        else:
            dataframe = df
    return dataframe


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
        datetime.datetime.utcfromtimestamp(last_timestamp) + datetime.timedelta(minutes=time_period_to_minutes(args.scaling_freq)))
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
