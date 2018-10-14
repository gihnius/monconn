package main

import (
	"fmt"
	"net"
	"time"
)

func clientConn() net.Conn {
	conn, _ := net.Dial("tcp", "127.0.0.1:1234")
	return conn
}

func client() {
	c := clientConn()
    defer c.Close()
	buf := make([]byte, 256)
	for {
		_, err := c.Write([]byte("hello server!"))
		if err != nil {
			fmt.Println("write to server err: ", err)
		}

		_, err = c.Read(buf)
		if err != nil {
			fmt.Println("read err: ", err)
		} else {
			// fmt.Print("received from server:", string(buf[:n]))
		}
		time.Sleep(1 * time.Second)
	}
}

func main() {
	for i := 0; i <= 5000; i++ {
		go client()
	}

	select {}
}

// go run main.go
// telnet 127.0.0.1 1234
