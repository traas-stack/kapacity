from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = []
    POD_RESOURCE: _ClassVar[QueryType]
    CONTAINER_RESOURCE: _ClassVar[QueryType]
    WORKLOAD_RESOURCE: _ClassVar[QueryType]
    WORKLOAD_CONTAINER_RESOURCE: _ClassVar[QueryType]
    OBJECT: _ClassVar[QueryType]
    EXTERNAL: _ClassVar[QueryType]
    WORKLOAD_EXTERNAL: _ClassVar[QueryType]
POD_RESOURCE: QueryType
CONTAINER_RESOURCE: QueryType
WORKLOAD_RESOURCE: QueryType
WORKLOAD_CONTAINER_RESOURCE: QueryType
OBJECT: QueryType
EXTERNAL: QueryType
WORKLOAD_EXTERNAL: QueryType

class Series(_message.Message):
    __slots__ = ["points", "labels", "window"]
    class LabelsEntry(_message.Message):
        __slots__ = ["key", "value"]
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    POINTS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    WINDOW_FIELD_NUMBER: _ClassVar[int]
    points: _containers.RepeatedCompositeFieldContainer[Point]
    labels: _containers.ScalarMap[str, str]
    window: _duration_pb2.Duration
    def __init__(self, points: _Optional[_Iterable[_Union[Point, _Mapping]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., window: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class Sample(_message.Message):
    __slots__ = ["point", "labels", "window"]
    class LabelsEntry(_message.Message):
        __slots__ = ["key", "value"]
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    POINT_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    WINDOW_FIELD_NUMBER: _ClassVar[int]
    point: Point
    labels: _containers.ScalarMap[str, str]
    window: _duration_pb2.Duration
    def __init__(self, point: _Optional[_Union[Point, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., window: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class Point(_message.Message):
    __slots__ = ["timestamp", "value"]
    TIMESTAMP_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    timestamp: int
    value: float
    def __init__(self, timestamp: _Optional[int] = ..., value: _Optional[float] = ...) -> None: ...

class Query(_message.Message):
    __slots__ = ["type", "pod_resource", "container_resource", "workload_resource", "workload_container_resource", "object", "external", "workload_external"]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    POD_RESOURCE_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_RESOURCE_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_RESOURCE_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_CONTAINER_RESOURCE_FIELD_NUMBER: _ClassVar[int]
    OBJECT_FIELD_NUMBER: _ClassVar[int]
    EXTERNAL_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_EXTERNAL_FIELD_NUMBER: _ClassVar[int]
    type: QueryType
    pod_resource: PodResourceQuery
    container_resource: ContainerResourceQuery
    workload_resource: WorkloadResourceQuery
    workload_container_resource: WorkloadContainerResourceQuery
    object: ObjectQuery
    external: ExternalQuery
    workload_external: WorkloadExternalQuery
    def __init__(self, type: _Optional[_Union[QueryType, str]] = ..., pod_resource: _Optional[_Union[PodResourceQuery, _Mapping]] = ..., container_resource: _Optional[_Union[ContainerResourceQuery, _Mapping]] = ..., workload_resource: _Optional[_Union[WorkloadResourceQuery, _Mapping]] = ..., workload_container_resource: _Optional[_Union[WorkloadContainerResourceQuery, _Mapping]] = ..., object: _Optional[_Union[ObjectQuery, _Mapping]] = ..., external: _Optional[_Union[ExternalQuery, _Mapping]] = ..., workload_external: _Optional[_Union[WorkloadExternalQuery, _Mapping]] = ...) -> None: ...

class PodResourceQuery(_message.Message):
    __slots__ = ["namespace", "name", "selector", "resource_name"]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    selector: str
    resource_name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., selector: _Optional[str] = ..., resource_name: _Optional[str] = ...) -> None: ...

class ContainerResourceQuery(_message.Message):
    __slots__ = ["namespace", "name", "selector", "resource_name", "container_name"]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_NAME_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    selector: str
    resource_name: str
    container_name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., selector: _Optional[str] = ..., resource_name: _Optional[str] = ..., container_name: _Optional[str] = ...) -> None: ...

class WorkloadResourceQuery(_message.Message):
    __slots__ = ["group_kind", "namespace", "name", "resource_name"]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_NAME_FIELD_NUMBER: _ClassVar[int]
    group_kind: GroupKind
    namespace: str
    name: str
    resource_name: str
    def __init__(self, group_kind: _Optional[_Union[GroupKind, _Mapping]] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., resource_name: _Optional[str] = ...) -> None: ...

class WorkloadContainerResourceQuery(_message.Message):
    __slots__ = ["group_kind", "namespace", "name", "resource_name", "container_name"]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_NAME_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_NAME_FIELD_NUMBER: _ClassVar[int]
    group_kind: GroupKind
    namespace: str
    name: str
    resource_name: str
    container_name: str
    def __init__(self, group_kind: _Optional[_Union[GroupKind, _Mapping]] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., resource_name: _Optional[str] = ..., container_name: _Optional[str] = ...) -> None: ...

class ObjectQuery(_message.Message):
    __slots__ = ["group_kind", "namespace", "name", "selector", "metric"]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    METRIC_FIELD_NUMBER: _ClassVar[int]
    group_kind: GroupKind
    namespace: str
    name: str
    selector: str
    metric: MetricIdentifier
    def __init__(self, group_kind: _Optional[_Union[GroupKind, _Mapping]] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., selector: _Optional[str] = ..., metric: _Optional[_Union[MetricIdentifier, _Mapping]] = ...) -> None: ...

class ExternalQuery(_message.Message):
    __slots__ = ["namespace", "metric"]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    METRIC_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    metric: MetricIdentifier
    def __init__(self, namespace: _Optional[str] = ..., metric: _Optional[_Union[MetricIdentifier, _Mapping]] = ...) -> None: ...

class WorkloadExternalQuery(_message.Message):
    __slots__ = ["group_kind", "namespace", "name", "metric"]
    GROUP_KIND_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    METRIC_FIELD_NUMBER: _ClassVar[int]
    group_kind: GroupKind
    namespace: str
    name: str
    metric: MetricIdentifier
    def __init__(self, group_kind: _Optional[_Union[GroupKind, _Mapping]] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., metric: _Optional[_Union[MetricIdentifier, _Mapping]] = ...) -> None: ...

class GroupKind(_message.Message):
    __slots__ = ["group", "kind"]
    GROUP_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    group: str
    kind: str
    def __init__(self, group: _Optional[str] = ..., kind: _Optional[str] = ...) -> None: ...

class MetricIdentifier(_message.Message):
    __slots__ = ["name", "selector"]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    name: str
    selector: str
    def __init__(self, name: _Optional[str] = ..., selector: _Optional[str] = ...) -> None: ...
