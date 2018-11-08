// Package monconn provides monitors for cnnections from tcp listener
package monconn

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/netutil"
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

// try listen for service restart
func tryListen(network, addr string) (ln net.Listener, err error) {
	try := func() error {
		ln, err = net.Listen(network, addr)
		if err != nil {
			return err
		}
		return nil
	}
	// simple backoff, retry 3 times
	for i := 1; i <= 3; i++ {
		err = try()
		if err == nil {
			break
		} else {
			dur := time.Duration(i*500) * time.Millisecond
			time.Sleep(dur)
		}
	}
	return
}

// Service monitor listener and all of connections
type Service struct {
	*IPBucket
	stats
	sid          string
	ln           net.Listener
	stopCh       chan struct{}
	stopped      bool
	mutex        sync.Mutex
	wg           *sync.WaitGroup
	ipBlackList  map[string]bool // reject connection from these client ip
	ReadTimeout  int             // in seconds, default 10 minutes
	WriteTimeout int             // in seconds, default 10 minutes
	WaitTimeout  int             // timeout to wait for Close, default 1 minute
	MaxIdle      int             // in seconds, default 15 minutes
	IdleInterval int             // check idle every N seconds, default 15 seconds
	ConnLimit    int             // 0 means no limit
	IPLimit      int             // 0 means no limit
	KeepAlive    bool            // keep conn alive default true
	PrintBytes   bool            // output the read write bytes
}

type stats struct {
	accepted   int64 // all wrapped/monitored connections count
	dropped    int64 // not acquirable count
	bootAt     int64 // service start time
	accessAt   int64 // client latest accessed(read) time
	connCount  int64 // active connections count
	ipCount    int64 // active connecting ips count
	readBytes  int64 // connections read total bytes
	writeBytes int64 // connections write total bytes
	counterCh  chan int
}

// counterCh enum
const (
	countAccept int = iota
	countDrop
	countConn
	countConnDone
)

// call monconn.NewService(SID) to construct
// and mondify exported attrs from returned instance
func initService() (s *Service) {
	s = &Service{
		stopCh:       make(chan struct{}),
		wg:           &sync.WaitGroup{},
		ipBlackList:  map[string]bool{},
		stats:        stats{counterCh: make(chan int, 1024)},
		IPBucket:     &IPBucket{&sync.Map{}, MaxIPLimit},
		ReadTimeout:  600,
		WriteTimeout: 600,
		WaitTimeout:  60,
		KeepAlive:    true,
		MaxIdle:      900,
		IdleInterval: 15,
	}
	return
}

// AcquireConn accept new connection and wrap in MonConn
func (s *Service) AcquireConn() (net.Conn, error) {
	conn, err := s.ln.Accept()
	if err == nil {
		if s.acquirable(conn) {
			conn = s.WrapMonConn(conn)
		} else {
			err = fmt.Errorf("S[%s] can not acquire new connections", s.sid)
			return nil, err
		}
	}
	return conn, err
}

// acquirable check ip blacklist and ip count
func (s *Service) acquirable(c net.Conn) (yes bool) {
	yes = true
	defer func() {
		if !yes {
			if c != nil {
				c.Close()
			}
			s.counterCh <- countDrop
			if Debug {
				logf("S[%s] acquire connection failed!", s.sid)
			}
		}
	}()
	if s.stopped {
		return false
	}
	if c != nil && yes {
		clientIP, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if len(s.ipBlackList) > 0 {
			if s.blockedIP(clientIP) {
				logf("S[%s] client ip: %s is blocked!.", s.sid, clientIP)
				yes = false
			}
		}
		if yes && s.IPLimit > 0 {
			ipCount := atomic.LoadInt64(&s.ipCount)
			if ipCount > int64(s.IPLimit) {
				// ip count limited, reject new ip
				yes = s.IPBucket.Contains(clientIP)
			}
		}
	}
	return yes
}

// WrapMonConn wrap net.Conn return (net.Conn, ok)
// Do nil check for net.Conn before call
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

// Listen start monitor listener
func (s *Service) Listen(network, addr string) (err error) {
	ln, err := tryListen(network, addr)
	if err != nil {
		logf("S[%s] service try to listen %s failed.", addr)
		return err
	}
	if s.ConnLimit > 0 {
		s.ln = netutil.LimitListener(ln, s.ConnLimit)
	} else {
		s.ln = ln
	}
	s.wg.Add(1)
	go s.monitorListener()
	s.bootAt = time.Now().Unix()
	s.accessAt = s.bootAt
	return
}

// Close stop the listener and close all of connections
func (s *Service) Close() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.stopped {
		logf("S[%s] stopping service...", s.sid)
		logf("S[%s] stats: %s", s.sid, s.Log())
		close(s.stopCh)
		s.stopped = true
		s.wg.Wait()
		logf("S[%s] stopped.", s.sid)
	}
}

// monitorListener wait listener
func (s *Service) monitorListener() {
	defer func() {
		s.wg.Done()
		logf("S[%s] stopping listening on %s", s.sid, s.ln.Addr())
		s.ln.Close()
	}()
	for {
		select {
		// wait for service Stop
		case <-s.stopCh:
			return
		case c := <-s.counterCh:
			switch c {
			case countAccept:
				s.accepted++
			case countDrop:
				s.dropped++
			case countConn:
				s.connCount++
			case countConnDone:
				s.connCount--
			}
		}
	}
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
	return atomic.LoadInt64(&s.readBytes), atomic.LoadInt64(&s.writeBytes)
}

// helper for montiorConn
func (s *Service) grabConn(c *MonConn) {
	s.counterCh <- countConn
	s.counterCh <- countAccept
	// limit IPs
	if s.IPLimit > 0 {
		clientIP := c.clientIP()
		if ok := s.IPBucket.Add(clientIP); !ok {
			logf("S[%s] add ip %s to IPBucket failed.", s.sid, clientIP)
		}
		atomic.StoreInt64(&s.ipCount, s.IPBucket.Count())
	}
}

// helper for montiorConn
func (s *Service) dropConn(c *MonConn) {
	s.counterCh <- countConnDone
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
		logf("S[%s] *IdleInterval* must >= 5", s.sid)
	}
	ticker := time.NewTicker(time.Second * time.Duration(s.IdleInterval))
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			logf("S[%s] disconnecting connection %s in %d(s).",
				s.sid,
				c.label,
				s.WaitTimeout)
			if !c.closed {
				time.Sleep(time.Duration(s.WaitTimeout) * time.Second)
				c.Close()
			}
			return
		case <-c.ch:
			// quit monitor by c.Close() proactive/auto call
			if Debug {
				logf("S[%s] connection %s monitor finished.", s.sid, c.label)
			}
			return
		case <-ticker.C:
			if c.Idle() {
				if Debug {
					logf("S[%s] connection idle too long: %s", s.sid, c.Log())
				}
				c.Close()
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

// format a timestamp
func tsFormat(ts int64) string {
	return time.Unix(ts, 0).In(time.Local).Format(time.RFC3339)
}

// ResetStats ...
func (s *Service) ResetStats() {
	st := stats{bootAt: s.bootAt, accessAt: s.accessAt}
	s.stats = st
}

// Stats output json format stats
//     "sid": service name or id
//     "uptime": service run time
//     "ips": connecting ips
//     "connections": connections count
//     "accessed": client latest accessed time
//     "up": client uploaded bytes
//     "down": client downloaded bytes
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
