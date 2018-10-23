package main

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

func handleConn(c net.Conn, remoteAddr string) {
	defer c.Close()
	rc, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		fmt.Printf("failed to connect to target: %s", err)
		return
	}
	defer rc.Close()
	_, _, err = relay(c, rc)
	if err != nil {
		if strings.Contains(err.Error(), "i/o timeout") {
			// ignore
			return
		}
		fmt.Printf("relay error: %v", err)
	}
}

// copies betweens two connections
func relay(left, right net.Conn) (int64, int64, error) {
	var toLeft, toRight int64
	var err, errLeft, errRight error
	var msgLeft, msgRight string
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		toRight, errRight = io.Copy(right, left)
		if errRight != nil {
			msgRight = fmt.Sprintf("copy to right from left err: %v", errRight)
		}
		right.SetDeadline(time.Now())
	}()

	toLeft, errLeft = io.Copy(left, right)
	if errLeft != nil {
		msgLeft = fmt.Sprintf("copy to left from right err: %v", errLeft)
	}
	left.SetDeadline(time.Now())
	wg.Wait()
	// merge the errors, not good
	if msgLeft != "" || msgRight != "" {
		err = fmt.Errorf("%s - %s", msgLeft, msgRight)
	}
	return toLeft, toRight, err
}
