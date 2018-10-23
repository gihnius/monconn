package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gihnius/monconn"
)

var setting struct {
	debug       bool
	addressMaps addressMap
	configFile  string
}

func init() {
	monconn.Debug = true
}

func main() {
	flag.Var(&setting.addressMaps, "p", "address mappings: listenIP:listenPort=backendIP:backendPort ...")
	flag.StringVar(&setting.configFile, "c", "", "yaml format config file")
	flag.BoolVar(&setting.debug, "D", false, "verbose debug messages")
	flag.Parse()

	for _, m := range setting.addressMaps {
		fmt.Println(m)
		l := m.listenIP + ":" + m.listenPort
		r := m.backendIP + ":" + m.backendPort
		launchService(l, r)
	}

	waitSignal()
}

func launchService(localAddr, remoteAddr string) {
	ln, err := net.Listen("tcp", localAddr)
	if err != nil {
		fmt.Printf("Failed to listen on %s", localAddr)
		return
	}
	sv := monconn.NewService(localAddr)
	sv.Start(ln)
	// sv.PrintBytes = true
	for {
		conn, err := ln.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			time.Sleep(1 * time.Second)
			continue
		}
		if conn != nil && sv.Acquirable(conn) {
			go handleConn(sv.WrapMonConn(conn), remoteAddr)
		}
	}
}

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

// flag var
type addrPair struct {
	listenIP    string
	listenPort  string
	backendIP   string
	backendPort string
}

func (a addrPair) String() string {
	return fmt.Sprintf("%s:%s=%s:%s", a.listenIP, a.listenPort, a.backendIP, a.backendPort)
}

type addressMap []addrPair

func (m addressMap) String() string {
	s := make([]string, len(m))
	for i, addr := range m {
		s[i] = addr.String()
	}
	return strings.Join(s, " ")
}

func (m *addressMap) Set(value string) error {
	vals := strings.Split(value, "=")
	l := strings.Split(vals[0], ":")
	r := strings.Split(vals[1], ":")
	*m = append(*m, addrPair{l[0], l[1], r[0], r[1]})
	return nil
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

	if msgLeft != "" || msgRight != "" {
		err = fmt.Errorf("%s - %s", msgLeft, msgRight)
	}
	return toLeft, toRight, err
}

func waitSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM)
	for sig := range sigCh {
		if sig == syscall.SIGHUP {
			// TODO:
			// reload from config file
			continue
		}
		// else: INT, TERM, QUIT
		monconn.Shutdown()
		os.Exit(0)
	}
}
