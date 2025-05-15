package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"time"

	"github.com/quic-go/quic-go"
)

func main() {
	// 启动服务端
	go runServer()

	// 等待服务端启动
	time.Sleep(1 * time.Second)

	// 启动客户端
	runClient()
}

// 生成TLS配置
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour * 24 * 180),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}
}

// 服务端
func runServer() {
	log.Println("服务端开始启动...")

	// 监听地址
	addr := "localhost:4242"

	// QUIC配置
	quicConfig := &quic.Config{}

	// 启动QUIC服务器
	listener, err := quic.ListenAddr(addr, generateTLSConfig(), quicConfig)
	if err != nil {
		log.Fatalf("服务端启动失败: %v", err)
	}

	log.Printf("服务端已启动，监听 %s", addr)

	// 等待并接受连接
	conn, err := listener.Accept(context.Background())
	if err != nil {
		log.Fatalf("接受连接失败: %v", err)
	}

	log.Printf("服务端接受了客户端连接: %s", conn.RemoteAddr())

	// 处理客户端发起的流(客户端创建流)
	go handleIncomingStreams(conn)

	// 3秒后服务端创建一个流并在3秒后关闭
	go func() {
		// 等待5秒，确保客户端先创建的流已处理完毕
		time.Sleep(5 * time.Second)

		log.Println("服务端即将创建一个流...")
		stream, err := conn.OpenStreamSync(context.Background())
		if err != nil {
			log.Printf("服务端创建流失败: %v", err)
			return
		}

		log.Println("服务端流创建成功，发送数据...")
		if _, err := stream.Write([]byte("从服务端发送的消息")); err != nil {
			log.Printf("服务端写入数据失败: %v", err)
			return
		}

		log.Println("服务端等待3秒后将关闭流...")
		time.Sleep(3 * time.Second)

		log.Println("服务端正在关闭流...")
		if err := stream.Close(); err != nil {
			log.Printf("服务端关闭流失败: %v", err)
			return
		}
		log.Println("服务端已关闭流")
	}()

	// 保持服务器运行
	select {}
}

// 处理客户端发起的流
func handleIncomingStreams(conn quic.Connection) {
	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("接受流失败: %v", err)
			return
		}

		log.Printf("服务端接受了一个新的流: %d", stream.StreamID())

		// 处理流数据
		go handleStream(stream)
	}
}

// 处理单个流
func handleStream(stream quic.Stream) {
	// 读取流数据
	buffer := make([]byte, 1024)
	n, err := stream.Read(buffer)
	if err == nil {
		log.Printf("服务端从流读取到数据: %s", buffer[:n])
	} else if err == io.EOF {
		log.Printf("服务端读取到EOF，流已结束")
	} else {
		log.Printf("服务端读取流数据出错: %v", err)
	}

	// 监听流关闭
	for {
		_, err := stream.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Println("服务端感知到流被关闭: 收到EOF")
			} else {
				log.Printf("服务端感知到流异常: %v", err)
			}
			break
		}
	}

	log.Println("服务端流处理完成")
}

// 客户端
func runClient() {
	log.Println("客户端开始启动...")

	// 服务器地址
	addr := "localhost:4242"

	// TLS配置（客户端不验证证书）
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}

	// QUIC配置
	quicConfig := &quic.Config{}

	// 连接到服务器
	conn, err := quic.DialAddr(context.Background(), addr, tlsConfig, quicConfig)
	if err != nil {
		log.Fatalf("客户端连接服务器失败: %v", err)
	}

	log.Printf("客户端连接到服务器成功: %s", addr)

	// 创建一个流
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("客户端创建流失败: %v", err)
	}

	log.Println("客户端流创建成功，发送数据...")

	// 发送数据
	_, err = stream.Write([]byte("从客户端发送的消息"))
	if err != nil {
		log.Fatalf("客户端写入数据失败: %v", err)
	}

	log.Println("客户端等待3秒后将关闭流...")
	time.Sleep(3 * time.Second)

	// 关闭流
	log.Println("客户端正在关闭流...")
	err = stream.Close()
	if err != nil {
		log.Fatalf("客户端关闭流失败: %v", err)
	}
	log.Println("客户端已关闭流")

	// 处理服务器发起的流
	go func() {
		for {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				log.Printf("客户端接受流失败: %v", err)
				return
			}

			log.Printf("客户端接受了一个新的流: %d", stream.StreamID())

			// 处理流
			go handleClientStream(stream)
		}
	}()

	// 等待一段时间后退出
	time.Sleep(15 * time.Second)
	log.Println("客户端退出")
}

// 客户端处理服务器发起的流
func handleClientStream(stream quic.Stream) {
	// 读取数据
	buffer := make([]byte, 1024)
	n, err := stream.Read(buffer)
	if err == nil {
		log.Printf("客户端从流读取到数据: %s", buffer[:n])
	} else if err == io.EOF {
		log.Printf("客户端读取到EOF，流已结束")
	} else {
		log.Printf("客户端读取流数据出错: %v", err)
	}

	// 监听流关闭
	for {
		_, err := stream.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Println("客户端感知到流被关闭: 收到EOF")
			} else {
				log.Printf("客户端感知到流异常: %v", err)
			}
			break
		}
	}

	log.Println("客户端流处理完成")
}
