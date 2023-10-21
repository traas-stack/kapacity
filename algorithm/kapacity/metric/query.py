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


import time

import pandas as pd
import grpc

from google.protobuf import timestamp_pb2, duration_pb2

import kapacity.metric.pb.metric_pb2 as metric_pb
import kapacity.metric.pb.provider_pb2 as provider_pb
import kapacity.metric.pb.provider_pb2_grpc as provider_pb_grpc


def fetch_metrics(addr, namespace, metric, scale_target, start, end):
    metric_type = metric['type']
    if metric_type == 'Resource':
        return fetch_resource_metric_history(addr=addr,
                                             namespace=namespace,
                                             metric=metric,
                                             scale_target=scale_target,
                                             start=start,
                                             end=end)
    elif metric_type == 'ContainerResource':
        return fetch_container_resource_metric_history(addr=addr,
                                                       namespace=namespace,
                                                       metric=metric,
                                                       scale_target=scale_target,
                                                       start=start,
                                                       end=end)
    elif metric_type == 'Pods':
        # TODO: support pods metric type
        raise RuntimeError('UnsupportedMetricType')
    elif metric_type == 'Object':
        return fetch_object_metric_history(addr=addr,
                                           namespace=namespace,
                                           metric=metric,
                                           start=start,
                                           end=end)
    elif metric_type == 'External':
        return fetch_external_metric_history(addr=addr,
                                             namespace=namespace,
                                             metric=metric,
                                             start=start,
                                             end=end)
    else:
        raise RuntimeError('UnsupportedMetricType')


def compute_history_range(history_len):
    now = time.time()
    ago = now - (time_period_to_minutes(history_len) * 60)

    start = timestamp_pb2.Timestamp()
    start.FromSeconds(int(ago))
    end = timestamp_pb2.Timestamp()
    end.FromSeconds(int(now))
    return start, end


def fetch_replicas_metric_history(addr, namespace, metric, scale_target, start, end):
    external = metric['external']
    metric_identifier = build_metric_identifier(external['metric'])
    name, group_kind = get_obj_name_and_group_kind(scale_target)
    workload_external = metric_pb.WorkloadExternalQuery(group_kind=group_kind,
                                                        namespace=namespace,
                                                        name=name,
                                                        metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.WORKLOAD_EXTERNAL,
                            workload_external=workload_external)
    return query_metrics(addr=addr, query=query, start=start, end=end)


def fetch_resource_metric_history(addr, namespace, metric, scale_target, start, end):
    resource_name = metric['resource']['name']
    name, group_kind = get_obj_name_and_group_kind(scale_target)
    workload_resource = metric_pb.WorkloadResourceQuery(group_kind=group_kind,
                                                        namespace=namespace,
                                                        name=name,
                                                        resource_name=resource_name,
                                                        ready_pods_only=True)
    query = metric_pb.Query(type=metric_pb.WORKLOAD_RESOURCE,
                            workload_resource=workload_resource)
    return query_metrics(addr=addr, query=query, start=start, end=end)


def fetch_container_resource_metric_history(addr, namespace, metric, scale_target, start, end):
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
    return query_metrics(addr=addr, query=query, start=start, end=end)


def fetch_object_metric_history(addr, namespace, metric, start, end):
    obj = metric['object']
    metric_identifier = build_metric_identifier(obj['metric'])
    name, group_kind = get_obj_name_and_group_kind(obj['describedObject'])
    object_query = metric_pb.ObjectQuery(namespace=namespace,
                                         name=name,
                                         group_kind=group_kind,
                                         metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.OBJECT,
                            object=object_query)
    return query_metrics(addr=addr, query=query, start=start, end=end)


def fetch_external_metric_history(addr, namespace, metric, start, end):
    external = metric['external']
    metric_identifier = build_metric_identifier(external['metric'])
    external_query = metric_pb.ExternalQuery(namespace=namespace,
                                             metric=metric_identifier)
    query = metric_pb.Query(type=metric_pb.EXTERNAL,
                            external=external_query)
    return query_metrics(addr=addr, query=query, start=start, end=end)


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


def query_metrics(addr, query, start, end):
    step = duration_pb2.Duration()
    step.FromSeconds(60)
    query_request = provider_pb.QueryRequest(query=query,
                                             start=start,
                                             end=end,
                                             step=step)
    with grpc.insecure_channel(addr) as channel:
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
