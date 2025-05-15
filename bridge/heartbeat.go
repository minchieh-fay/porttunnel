package bridge

import (
	"encoding/json"
	"log"
	"porttunnel/help"
	"time"
)

func (b *Bridge) sendHeartbeats() {
	for {
		time.Sleep(10 * time.Second)

		// 发送心跳消息
		msg := help.ControlMessage{
			Type:    help.MessageTypeHeartbeat,
			Payload: help.HeartbeatPayload{Timestamp: time.Now().Unix()},
		}

		encoder := json.NewEncoder(b.controlStream)
		if err := encoder.Encode(msg); err != nil {
			log.Printf("发送心跳消息失败: %v", err)
		}
	}
}
