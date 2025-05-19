package main

import (
	"fmt"
	"log"
	"os"
	"porttunnel/bridge"
	"porttunnel/help"
	"porttunnel/proxy"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	TunPort      int
	ProxyAddr    string
	PortMappings []help.PortMappingConfig
)

func init() {
	// 从环境变量读取TUNPORT
	if envPort := os.Getenv("TUNPORT"); envPort != "" {
		if port, err := strconv.Atoi(envPort); err == nil {
			TunPort = port
		} else {
			log.Printf("环境变量TUNPORT格式错误: %v", err)
		}
	} else {
		TunPort = 5566 // 默认值
	}

	// 从环境变量读取SERVER
	if envServer := os.Getenv("SERVER"); envServer != "" {
		ProxyAddr = envServer
	}

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
		PortMappings = append(PortMappings, help.PortMappingConfig{
			Protocol:     parts[0],
			ServerPort:   serverPort,
			ResourceAddr: parts[2],
			ResourcePort: resourcePort,
		})
	}
}

func test() {
	ProxyAddr = "localhost:5566"
	PortMappings = []help.PortMappingConfig{
		{
			Protocol:     "tcp",
			ServerPort:   5500,
			ResourceAddr: "10.35.146.7",
			ResourcePort: 11002,
		},
	}
}

func main() {
	log.Println("tunproxy开始启动")
	// 设置单线程
	runtime.GOMAXPROCS(1)

	// 打印当前配置
	log.Printf("当前配置：TUNPORT=%d, SERVER=%s", TunPort, ProxyAddr)
	if len(PortMappings) > 0 {
		log.Printf("端口映射数量：%d", len(PortMappings))
		for i, mapping := range PortMappings {
			log.Printf("映射 %d: %s:%d -> %s:%d",
				i+1,
				mapping.Protocol,
				mapping.ServerPort,
				mapping.ResourceAddr,
				mapping.ResourcePort)
		}
	}

	proxy := proxy.NewProxy()
	proxy.Start(TunPort)
	if ProxyAddr != "" {
		log.Println("tunproxy开始启动bridge")
		bridge := bridge.NewBridge()
		bridge.Start(ProxyAddr, PortMappings)
	}
	select {}
}
