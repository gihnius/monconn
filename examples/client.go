package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

func clientConn() net.Conn {
	conn, _ := net.Dial("tcp", "127.0.0.1:1234")
	return conn
}

func client(wg *sync.WaitGroup) {
	c := clientConn()
	defer func() {
		c.Close()
		fmt.Println("connection closed:", c.LocalAddr())
		wg.Done()
	}()
	if c == nil {
		fmt.Println("unable to connect to server")
		return
	}
	fmt.Println("connected to server:", c.LocalAddr())
	buf := make([]byte, 256)
	for {
		_, err := c.Write([]byte("hello server!"))
		if err != nil {
			fmt.Println("write to server err: ", err)
			break
		}

		_, err = c.Read(buf)
		if err != nil {
			fmt.Println("read err: ", err)
			break
		} else {
			// fmt.Print("received from server:", string(buf[:n]))
		}
		time.Sleep(5 * time.Second)
		return
	}
}

func main() {
	wg := &sync.WaitGroup{}
	for i := 0; i <= 1000; i++ {
		wg.Add(1)
		// time.Sleep(time.Second)
		go client(wg)
	}
	wg.Wait()
}

// go run main.go
// telnet 127.0.0.1 1234
