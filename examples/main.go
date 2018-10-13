package main

import (
	"fmt"
	"net"

	"github.com/gihnius/monconn"
)

func server() {
	sv := monconn.NewService("localhost:1234")
	monconn.Debug = true // enable debug logging
	// service OPTIONAL settings
	// reset within the service life cycle, will affect new connections
	sv.ReadTimeout = 120              // default 600 (10 minutes)
	sv.WriteTimeout = 60              // default 600 (10 minutes)
	sv.MaxIdle = 60                   // default 900 (15 minutes)
	sv.KeepAlive = false              // default true
	sv.ConnLimit = 10                 // limit up to 10 concurrency connections
	sv.IPLimit = 5                    // limit up to 5 concurrency client ips
	sv.RejectIP("1.2.3.4", "5.6.7.8") // add ip to blacklist

	fmt.Println("Launching server...")
	ln, _ := net.Listen("tcp", "127.0.0.1:1234")
	// start monitor listener
	sv.Start(ln)
	for {
		conn, _ := ln.Accept()
		// main call, monitor incomming connection
		c := sv.WrapMonConn(conn)
		go handleConn(c)
	}
}

func handleConn(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 256)
	for {
		n, err := c.Read(buf)
		if err != nil {
			fmt.Println("handle err: ", err)
			return
		} else {
			fmt.Print("server received: ", string(buf[:n]))
			c.Write([]byte("from server: go on\n"))
		}
	}
}

func main() {
	go server()

	select {}
}

// go run main.go
// telnet 127.0.0.1 1234
