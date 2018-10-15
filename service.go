// Package monconn provides monitors for cnnections from tcp listener
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
	stopCh       chan struct{}
	wg           *sync.WaitGroup
	ipBlackList  map[string]bool // reject connection from these client ip
	bootAt       int64           // service start time
	accessAt     int64           // client latest accessed(read) time
	connCount    int64           // current connections count
	ipCount      int64           // current connecting ips count
	readBytes    int64           // connections read total bytes
	writeBytes   int64           // connections write total bytes
	ReadTimeout  int             // in seconds, default 10 minutes
	WriteTimeout int             // in seconds, default 10 minutes
	MaxIdle      int             // in seconds, default 15 minutes
	IdleInterval int             // check idle every N seconds, default 15 seconds
	ConnLimit    int64           // 0 means no limit
	IPLimit      int64           // 0 means no limit
	KeepAlive    bool            // keep conn alive default true
}

// call monconn.NewService(SID) to construct
// and mondify exported attrs from returned instance
func initService() (s *Service) {
	s = &Service{
		stopCh:       make(chan struct{}),
		wg:           &sync.WaitGroup{},
		ipBlackList:  map[string]bool{},
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
	for _, addr := range ip {
		s.ipBlackList[addr] = true
	}
	logf("added ip:%v to blacklist", s.ipBlackList)
}

// ReleaseIP remove ip from blacklist
func (s *Service) ReleaseIP(ip ...string) {
	for _, addr := range ip {
		if _, ok := s.ipBlackList[addr]; ok {
			delete(s.ipBlackList, addr)
		}
	}
	logf("removed ip:%v from blacklist", ip)
}

// check reject ip
func (s *Service) blockedIP(ip string) bool {
	_, ok := s.ipBlackList[ip]
	return ok
}

// Acquirable check ConnLimit or IPLimit if exceed
// call with nil if before accepted connection
func (s *Service) Acquirable(c net.Conn) bool {
	yes := true
	if s.ConnLimit > 0 {
		yes = s.connCount <= s.ConnLimit
	}
	if yes && s.IPLimit > 0 {
		yes = s.ipCount <= s.IPLimit
	}
	if c != nil {
		clientIP, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if s.blockedIP(clientIP) {
			logf("S[%s] client ip: %s is blocked!.", s.sid, clientIP)
			c.Close()
			return false
		}
	}
	if Debug && !yes {
		logf("S[%s] acquire connection failed: %d(ip) and %d(conn). %s",
			s.sid,
			s.ipCount,
			s.connCount,
			s.IPBucket.Log())
	}
	return yes
}

// WrapMonConn wrap net.Conn return (net.Conn, ok)
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
	logf("S[%s] stopping...", s.sid)
	logf("S[%s] stats: %s", s.sid, s.Log())
	logf("S[%s] connecting ips: %s", s.sid, s.IPBucket.Log())
	close(s.stopCh)
	s.wg.Wait()
	logf("S[%s] stopped.", s.sid)
}

// monitorListener wait listener
func (s *Service) monitorListener() {
	defer s.wg.Done()
	// wait for service Stop
	<-s.stopCh
	logf("S[%s] stopping listening on %s", s.sid, s.ln.Addr())
	s.ln.Close()
}

// EliminateBytes reduce the num record of rw bytes
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
func (s *Service) grabConn(c *MonConn) {
	atomic.AddInt64(&s.connCount, 1)
	clientIP := c.clientIP()
	// limit IPs
	if s.IPLimit > 0 {
		if ok := s.IPBucket.Add(clientIP); !ok {
			logf("S[%s] Add ip %s to IPBucket failed.", s.sid, clientIP)
		}
		atomic.StoreInt64(&s.ipCount, s.IPBucket.Count())
	}
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
	s.grabConn(c)
	if Debug {
		logf("S[%s] monitored connection: %s", s.sid, c.label)
	}
	if s.IdleInterval < 5 {
		s.IdleInterval = 5
		logf("S[%s] IdleInterval must >= 5", s.sid)
	}
	heartbeat := time.Tick(time.Second * time.Duration(s.IdleInterval))
	for {
		select {
		case <-s.stopCh:
			logf("S[%s] disconnecting connection %s by service.", s.sid, c.label)
			c.Close()
			return
		case <-c.ch:
			// quit monitor by c.Close() proactive call
			if Debug {
				logf("S[%s] connection %s monitor finished.", s.sid, c.label)
			}
			return
		case <-heartbeat:
			if c.Idle() {
				c.Close()
				if Debug {
					logf("S[%s] connection idle too long: %s", s.sid, c.Log())
				}
				return
			}
			if Debug {
				c.updateService() // see realtime updates on debug mode
				logf("S[%s] stats: %s", s.sid, s.Log())
				logf("S[%s] connection stats: %s", s.sid, c.Log())
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

// Sid get sid
func (s *Service) Sid() string {
	return s.sid
}

// AccessAt get accessAt
func (s *Service) AccessAt() int64 {
	return s.accessAt
}

// BootAt get bootAt
func (s *Service) BootAt() int64 {
	return s.bootAt
}

// ConnCount get connCount
func (s *Service) ConnCount() int64 {
	return s.connCount
}

// IPCount get ipCount
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
