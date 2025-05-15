package help

type PortMappingConfig struct {
	Protocol     string
	ServerPort   int
	ResourceAddr string
	ResourcePort int
}

// ControlMessage 控制消息
type ControlMessage struct {
	Type    string      // 消息类型，如 "register_mappings"
	Payload interface{} // 消息载荷
}

// 控制消息类型
const (
	MessageTypeRegisterMappings = "register_mappings"
	MessageTypeForwardData      = "forward_data"
	MessageTypeHeartbeat        = "heartbeat"
)

// RegisterMappingsPayload 注册端口映射的消息载荷
type RegisterMappingsPayload struct {
	Mappings []PortMappingConfig // 端口映射列表
}

// ForwardDataPayload 转发数据的消息载荷
type ForwardDataPayload struct {
	Mapping PortMappingConfig // 转发的信息
}

// HeartbeatPayload 心跳消息载荷
type HeartbeatPayload struct {
	Timestamp int64 // 心跳时间戳
}
