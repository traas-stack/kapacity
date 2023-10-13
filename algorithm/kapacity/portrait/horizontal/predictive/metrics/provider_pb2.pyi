import metric_pb2 as _metric_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryLatestRequest(_message.Message):
    __slots__ = ["query"]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    query: _metric_pb2.Query
    def __init__(self, query: _Optional[_Union[_metric_pb2.Query, _Mapping]] = ...) -> None: ...

class QueryLatestResponse(_message.Message):
    __slots__ = ["samples"]
    SAMPLES_FIELD_NUMBER: _ClassVar[int]
    samples: _containers.RepeatedCompositeFieldContainer[_metric_pb2.Sample]
    def __init__(self, samples: _Optional[_Iterable[_Union[_metric_pb2.Sample, _Mapping]]] = ...) -> None: ...

class QueryRequest(_message.Message):
    __slots__ = ["query", "start", "end", "step"]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    START_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    STEP_FIELD_NUMBER: _ClassVar[int]
    query: _metric_pb2.Query
    start: _timestamp_pb2.Timestamp
    end: _timestamp_pb2.Timestamp
    step: _duration_pb2.Duration
    def __init__(self, query: _Optional[_Union[_metric_pb2.Query, _Mapping]] = ..., start: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., end: _Optional[_Union[_timestamp_pb2.Timestamp, _Mapping]] = ..., step: _Optional[_Union[_duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class QueryResponse(_message.Message):
    __slots__ = ["series"]
    SERIES_FIELD_NUMBER: _ClassVar[int]
    series: _containers.RepeatedCompositeFieldContainer[_metric_pb2.Series]
    def __init__(self, series: _Optional[_Iterable[_Union[_metric_pb2.Series, _Mapping]]] = ...) -> None: ...
