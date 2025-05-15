package types

// PortMapping 端口映射配置
type PortMapping struct {
	Protocol     string // tcp或udp
	ServerPort   int    // 在proxy上监听的端口
	ResourceAddr string // 目标资源地址
	ResourcePort int    // 目标资源端口
	BridgeID     string // 所属的bridge ID
}

// ControlMessage 控制消息
type ControlMessage struct {
	Type    string      // 消息类型，如 "register_mappings"
	Payload interface{} // 消息载荷
}

// RegisterMappingsPayload 注册端口映射的消息载荷
type RegisterMappingsPayload struct {
	BridgeID string        // bridge的唯一标识
	Mappings []PortMapping // 端口映射列表
}

// 控制消息类型常量
const (
	MessageTypeRegisterMappings = "register_mappings"
	MessageTypeForwardData      = "forward_data"
)
