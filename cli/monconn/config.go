package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/gihnius/monconn"

	"gopkg.in/yaml.v2"
)

type config struct {
	ListenIP     string   `yaml:"listen_ip"`
	ListenPort   string   `yaml:"listen_port"`
	BackendIP    string   `yaml:"backend_ip"`
	BackendPort  string   `yaml:"backend_port"`
	ReadTimeout  int      `yaml:"read_timeout"`
	WriteTimeout int      `yaml:"write_timeout"`
	WaitTimeout  int      `yaml:"wait_timeout"`
	MaxIdle      int      `yaml:"max_idle"`
	ConnLimit    int      `yaml:"conn_limit"`
	IPLimit      int      `yaml:"ip_limit"`
	KeepAlive    bool     `yaml:"keepalive"`
	PrintBytes   bool     `yaml:"print_bytes"`
	IPBlackList  []string `yaml:"ip_blacklist"`
}

var servicesConfig = map[string]config{}

func parseConfig(path string) error {
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("open config file error: %v\n", err)
		return err
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		fmt.Printf("read config file error: %v\n", err)
		return err
	}
	err = yaml.Unmarshal([]byte(data), &servicesConfig)
	if err != nil {
		fmt.Printf("parse yaml config error: %v\n", err)
	}
	return err
}

func (a *config) listenAddr() string {
	return fmt.Sprintf("%s:%s", a.ListenIP, a.ListenPort)
}

func (a *config) backendAddr() string {
	return fmt.Sprintf("%s:%s", a.BackendIP, a.BackendPort)
}

func (c *config) launchService() {
	sv := monconn.NewService(c.listenAddr())
	c.setDefault(sv)
	err := sv.Listen("tcp", c.listenAddr())
	if err != nil {
		fmt.Printf("Failed to listen on %s\n", c.listenAddr())
		return
	}
	for {
		conn, err := sv.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				return
			}
			time.Sleep(1 * time.Second)
			continue
		}
		go handleConn(conn, c.backendAddr())
	}
}

func (c *config) setDefault(sv *monconn.Service) {
	if c.ReadTimeout > 0 {
		sv.ReadTimeout = c.ReadTimeout
	}
	if c.WriteTimeout > 0 {
		sv.WriteTimeout = c.WriteTimeout
	}
	if c.WaitTimeout > 0 {
		sv.WaitTimeout = c.WaitTimeout
	}
	if c.MaxIdle > 0 {
		sv.MaxIdle = c.MaxIdle
	}
	if c.ConnLimit > 0 {
		sv.ConnLimit = c.ConnLimit
	}
	if c.IPLimit > 0 {
		sv.IPLimit = c.IPLimit
	}
	if !c.KeepAlive {
		// default true
		sv.KeepAlive = c.KeepAlive
	}
	if c.PrintBytes {
		sv.PrintBytes = c.PrintBytes
	}
	if len(c.IPBlackList) > 0 {
		sv.RejectIP(c.IPBlackList...)
	}
}
