package help

import (
	"time"

	"github.com/quic-go/quic-go"
)

func GetQuicConfig() *quic.Config {
	return &quic.Config{
		MaxIdleTimeout: 50 * time.Second,
	}
}
