package main

import (
	"flag"
	"fmt"
	"os"
	"porttunnel/bridge"
	"porttunnel/proxy"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type PortMappingConfig struct {
	Protocol     string
	ServerPort   int
	ResourceAddr string
	ResourcePort int
}

var (
	TunPort      int
	ProxyAddr    string
	PortMappings []PortMappingConfig
)

func init() {
	flag.IntVar(&TunPort, "TUNPORT", 5566, "隧道数据端口")
	flag.StringVar(&ProxyAddr, "SERVER", "", "上层服务proxy的地址")
	// 从环境变量中读取PORT1，PORT2，PORT3，...， 直到读取为空/失败 停止
	for i := 1; ; i++ {
		envPort := os.Getenv(fmt.Sprintf("PORT%d", i))
		if envPort == "" {
			break
		}
		parts := strings.Split(envPort, ":")
		if len(parts) != 4 {
			fmt.Printf("PORT%d 格式错误，请使用格式：tcp/udp:{server代理的端口}:{资源ip}:{资源port}\n", i)
			time.Sleep(time.Second * 1)
			os.Exit(1)
		}
		// 字符串转整数
		serverPort, _ := strconv.Atoi(parts[1])
		resourcePort, _ := strconv.Atoi(parts[3])
		PortMappings = append(PortMappings, PortMappingConfig{
			Protocol:     parts[0],
			ServerPort:   serverPort,
			ResourceAddr: parts[2],
			ResourcePort: resourcePort,
		})
	}
}

func main() {
	// 设置单线程
	runtime.GOMAXPROCS(1)
	flag.Parse()
	proxy := proxy.NewProxy()
	proxy.Start(TunPort)
	if ProxyAddr != "" {
		bridge := bridge.NewBridge()
		bridge.Start(ProxyAddr, PortMappings)
	}
}
