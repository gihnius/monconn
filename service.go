// Package monconn provides monitors for cnnections from a listener
package monconn

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Debug debuging messages toggle
var Debug = false

// LogFunc logger function type
type LogFunc func(format string, args ...interface{})

// DebugFunc custom a debug logger function
var DebugFunc LogFunc

func logf(format string, args ...interface{}) {
	if DebugFunc == nil {
		fmt.Printf(format, args...)
		fmt.Println()
	} else {
		DebugFunc(format, args...)
	}
}

// Service monitor listener and all of connections
type Service struct {
	*IPBucket
	sid          string
	ln           net.Listener
	stopChan     chan struct{}
	wg           *sync.WaitGroup
	ipBlackList  []string // reject connection from these client ip addresses
	bootAt       int64    // service start time
	accessAt     int64    // client latest accessed(read) time
	connCount    int64    // current connections count
	ipCount      int64    // current connecting ips count
	readBytes    int64    // connections read total bytes
	writeBytes   int64    // connections write total bytes
	ReadTimeout  int      // in seconds, default 10 minutes
	WriteTimeout int      // in seconds, default 10 minutes
	MaxIdle      int      // in seconds, default 15 minutes
	IdleInterval int      // check idle every N seconds, default 15 seconds
	ConnLimit    int64    // 0 means no limit
	IPLimit      int64    // 0 means no limit
	KeepAlive    bool     // keep conn alive default true
}

// call NewService to construct, and mondify exported attrs from returnd instance
func newService() (s *Service) {
	s = &Service{
		stopChan:     make(chan struct{}),
		wg:           &sync.WaitGroup{},
		ipBlackList:  make([]string, 128),
		IPBucket:     &IPBucket{&sync.Map{}},
		ReadTimeout:  600,
		WriteTimeout: 600,
		KeepAlive:    true,
		MaxIdle:      900,
		IdleInterval: 15,
	}
	return
}

// RejectIP add (client) ip(s) to blacklist
func (s *Service) RejectIP(ip ...string) {
	s.ipBlackList = append(s.ipBlackList, ip...)
	logf("added ip:%v to blacklist", ip)
}

// Acquirable check ConnLimit or IPLimit if exceed
func (s *Service) Acquirable() bool {
	yes := true
	if s.ConnLimit > 0 {
		yes = s.connCount <= s.ConnLimit
	}
	if yes && s.IPLimit > 0 {
		yes = s.ipCount <= s.IPLimit
	}
	if Debug && !yes {
		logf("acquire connection failed: %d(ip) and %d(conn). %s",
			s.ipCount,
			s.connCount,
			s.IPBucket.Log())
	}
	return yes
}

// WrapMonConn wrap net.Conn
func (s *Service) WrapMonConn(c net.Conn) net.Conn {
	mc := &MonConn{
		Conn:      c,
		service:   s,
		createdAt: time.Now().Unix(),
		ch:        make(chan struct{}),
	}
	mc.init()
	s.wg.Add(1)
	go s.monitorConn(mc)
	return mc
}

// Start monitor listener
func (s *Service) Start(ln net.Listener) {
	s.ln = ln
	s.wg.Add(1)
	go s.monitorListener()
	s.bootAt = time.Now().Unix()
	s.accessAt = s.bootAt
}

// Stop stop the listener and close all of connections
func (s *Service) Stop() {
	logf("stopping [%s] service...", s.sid)
	logf("service stats: %s", s.Log())
	logf("service connections: %s", s.IPBucket.Log())
	close(s.stopChan)
	s.wg.Wait()
	logf("stopped [%s] service.", s.sid)
}

// monitorListener wait listener
func (s *Service) monitorListener() {
	defer s.wg.Done()
	// wait for service Stop
	<-s.stopChan
	logf("stopping listening on %s", s.ln.Addr())
	s.ln.Close()
}

// EliminateFlow reduce the num record of rw bytes
// service ReadBytes & WriteBytes will not always growing,
// eliminate the amount after extract and store to other place,
// eg. store them in redis or database.
func (s *Service) EliminateBytes(r, w int64) {
	if r > 0 {
		atomic.AddInt64(&s.readBytes, -r)
	}
	if w > 0 {
		atomic.AddInt64(&s.writeBytes, -w)
	}
}

// ReadWriteBytes return number of read and write bytes
func (s *Service) ReadWriteBytes() (int64, int64) {
	return s.readBytes, s.writeBytes
}

// helper for montiorConn
func (s *Service) grabConn(c *MonConn) bool {
	atomic.AddInt64(&s.connCount, 1)
	clientIP := c.clientIP()
	// limit IPs
	if s.IPLimit > 0 {
		if ok := s.IPBucket.Add(clientIP); !ok {
			logf("Add ip %s to IPBucket failed.", clientIP)
		}
		atomic.StoreInt64(&s.ipCount, s.IPBucket.Count())
	}
	// check reject ip
	for _, ip := range s.ipBlackList {
		if ip == clientIP {
			logf("client ip: %s in blacklist, closing connection.", clientIP)
			c.Close()
			return false
		}
	}
	return true
}

// helper for montiorConn
func (s *Service) dropConn(c *MonConn) {
	atomic.AddInt64(&s.connCount, -1)
	clientIP := c.clientIP()
	if s.IPLimit > 0 {
		s.IPBucket.Remove(clientIP)
		atomic.StoreInt64(&s.ipCount, s.IPBucket.Count())
	}
	c = nil
}

// monitorConn extract conn data and monitor conn
func (s *Service) monitorConn(c *MonConn) {
	defer func() {
		s.dropConn(c)
		s.wg.Done()
	}()
	if !s.grabConn(c) {
		return
	}
	if Debug {
		logf("service %s monitored connection: %s", s.sid, c.label)
	}
	if s.IdleInterval < 5 {
		s.IdleInterval = 5
		logf("IdleInterval must >= 5")
	}
	heartbeat := time.Tick(time.Second * time.Duration(s.IdleInterval))
	for {
		select {
		case <-s.stopChan:
			logf("disconnecting connection by service: %s", c.label)
			c.Close()
			return
		case <-c.ch:
			// quit monitor by c.Close() proactive call
			if Debug {
				logf("connection monitor finished.")
			}
			return
		case <-heartbeat:
			if c.Idle() {
				c.Close()
				if Debug {
					logf("connection idle too long: %s", c.Log())
				}
				return
			}
			if Debug {
				c.updateService() // see realtime updates on debug mode
				logf("service stats: %s", s.Log())
				logf("connection stats: %s", c.Log())
			}
		}
	}
}

// IPs list connecting ip
func (s *Service) IPs() []string {
	return s.IPBucket.IPs()
}

// Uptime service up time in seconds
func (s *Service) Uptime() int64 {
	return time.Now().Unix() - s.bootAt
}

func (s *Service) Sid() string {
	return s.sid
}

func (s *Service) AccessAt() int64 {
	return s.accessAt
}

func (s *Service) BootAt() int64 {
	return s.bootAt
}

func (s *Service) ConnCount() int64 {
	return s.connCount
}

func (s *Service) IPCount() int64 {
	return s.ipCount
}

// Stats output json format stats
//     "sid": service name or id
//     "uptime": service run time
//     "ips": connecting ips
//     "connections": connections count
//     "accessed": client latest accessed time
//     "up": client upload
//     "down": client download
func (s *Service) Stats() string {
	format := `
{
  "sid": "%s",
  "uptime": %d,
  "ips": "%s",
  "connections": %d,
  "accessed": %d,
  "up": %d,
  "down": %d
}`
	return fmt.Sprintf(format,
		s.sid,
		s.Uptime(),
		strings.Join(s.IPs(), ","),
		s.connCount,
		s.accessAt,
		s.readBytes,
		s.writeBytes)
}

// Log output service detail in oneline log message
func (s *Service) Log() string {
	format := `sid: %s, up: %d, c: %d, ip: %d, r: %d, w: %d, t: %s`
	return fmt.Sprintf(format,
		s.sid,
		s.Uptime(),
		s.connCount,
		s.ipCount,
		s.readBytes,
		s.writeBytes,
		tsFormat(s.accessAt))
}

func tsFormat(ts int64) string {
	return time.Unix(ts, 0).In(time.Local).Format(time.RFC3339)
}
