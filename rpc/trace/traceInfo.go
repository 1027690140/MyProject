package trace

import (
	"rpc_service/naming"
)

type Endpoint struct {
	ServiceName string `json:"service_name"`
	Hostname    string `json:"hostname"`
	IPv4        string `json:"ipv4"`
	IPv6        string `json:"ipv6"`
	Port        int    `json:"port"`
}

type Annotation struct {
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
}

type ErrorInfo struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StackTrace string `json:"stack_trace"`
}

type Event struct {
	Timestamp int64         `json:"timestamp"`
	Name      string        `json:"name"`
	Data      []interface{} `json:"data"`
}

type ServiceMetadata struct {
	Instance *naming.Instance
	Metadata map[string]string `json:"metadata"`
}

// TraceInfo 链路信息
type TraceInfo struct {
	TraceID         string            `json:"trace_id"`  // 链路的唯一标识符
	ParentID        string            `json:"parent_id"` // 父SpanID
	ID              string            `json:"id"`        // SpanID
	Kind            string            `json:"kind"`      // 表示Span的类型，包括未指定类型、客户端、服务器、生产者和消费者  日志服务、持久化服务等等。
	Name            string            `json:"name"`
	Timestamp       int64             `json:"timestamp"`      //表示Span的开始时间戳
	EditTimestamp   int64             `json:"edit_timestamp"` //表示存redis时修改时间戳
	Duration        int64             `json:"duration"`
	LocalEndpoint   *Endpoint         `json:"local_endpoint"`  //表示本地端点的信息，包括服务名称、IPv4、IPv6和端口等
	RemoteEndpoint  *Endpoint         `json:"remote_endpoint"` //表示远程端点的信息，包括服务名称、IPv4、IPv6和端口等
	Annotations     []*Annotation     `json:"annotations"`     //表示Span的注解信息，包括时间戳和注解值的列表
	Tags            map[string]string `json:"tags"`
	Debug           bool              `json:"debug"`
	Shared          bool              `json:"shared"`
	Error           []*ErrorInfo      `json:"error,omitempty"`
	Events          []*Event          `json:"events"`
	ServiceMetadata *ServiceMetadata  `json:"service_metadata,omitempty"` //可以包含服务的元数据，如服务版本、部署环境、实例ID等
}
