package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strings"
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
	workers := runtime.NumCPU() * 2
	runtime.GOMAXPROCS(workers)
	rand.Seed(time.Now().Unix())
}

func main() {
	flag.Var(&setting.addressMaps, "p", "address mappings: listenIP:listenPort=backendIP:backendPort ...")
	flag.StringVar(&setting.configFile, "c", "config.yaml", "yaml format config file")
	flag.BoolVar(&setting.debug, "D", false, "verbose debug messages")
	flag.Parse()

	monconn.Debug = setting.debug

	if err := parseConfig(setting.configFile); err != nil {
		for i := range setting.addressMaps {
			addr := setting.addressMaps[i]
			cfg := config{
				ListenIP:    addr.listenIP,
				ListenPort:  addr.listenPort,
				BackendIP:   addr.backendIP,
				BackendPort: addr.backendPort,
			}
			go cfg.launchService()
		}
	} else {
		fmt.Println("using config file, -p params will be ignored.")
		for s, _ := range servicesConfig {
			c := servicesConfig[s]
			go c.launchService()
		}
	}
	waitSignal()
}

// flag var
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

type addrPair struct {
	listenIP    string
	listenPort  string
	backendIP   string
	backendPort string
}

func (a *addrPair) listenAddr() string {
	return fmt.Sprintf("%s:%s", a.listenIP, a.listenPort)
}

func (a *addrPair) backendAddr() string {
	return fmt.Sprintf("%s:%s", a.backendIP, a.backendPort)
}

func (a addrPair) String() string {
	return fmt.Sprintf("%s=%s", a.listenAddr(), a.backendAddr())
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
