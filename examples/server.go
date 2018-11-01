package main

import (
	"fmt"
	"net"
	"time"

	"github.com/gihnius/monconn"
)

var sv = monconn.NewService("localhost:1234")

func server() {
	monconn.Debug = true // enable debug logging
	// service OPTIONAL settings
	// reset within the service life cycle, will affect new connections
	sv.ReadTimeout = 120 // default 600 (10 minutes)
	sv.WriteTimeout = 60 // default 600 (10 minutes)
	sv.MaxIdle = 60      // default 900 (15 minutes)
	sv.KeepAlive = false // default true
	// sv.ConnLimit = 10                 // limit up to 10 concurrency connections
	// sv.IPLimit = 5                    // limit up to 5 concurrency client ips
	sv.RejectIP("1.2.3.4", "5.6.7.8") // add ip to blacklist
	sv.PrintBytes = true

	fmt.Println("Launching server...")
	// start monitor listener
	sv.Listen("tcp", "127.0.0.1:1234")
	for {
		conn, err := sv.Accept()
		// main call, monitor incomming connection
		if err != nil {
			break
		}
		go handleConn(conn)
	}
}

func handleConn(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 256)
	for {
		_, err := c.Read(buf)
		if err != nil {
			fmt.Println("handle err: ", err)
			break
		} else {
			// fmt.Print("server received: ", string(buf[:n]))
			c.Write([]byte("from server: go on\n"))
		}
	}
}

func main() {
	go server()
	time.Sleep(20 * time.Second)
	sv.Close()
	select {}
}

// go run server.go
// go run client.go
// or
// telnet 127.0.0.1 1234
