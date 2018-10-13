package monconn

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// MonConn wrap net.Conn for extra monitor data
type MonConn struct {
	net.Conn
	buf        *bufio.ReadWriter
	service    *Service
	label      string
	readBytes  int64
	writeBytes int64
	readAt     int64         // Read() call timestamp
	writeAt    int64         // Write() call timestamp
	createdAt  int64         // connection start timestamp
	ch         chan struct{} // close notify chan
	closed     bool
	mutex      sync.Mutex
}

func (c *MonConn) Read(b []byte) (n int, err error) {
	if c.service.ReadTimeout > 0 {
		dur := time.Now().Add(time.Second * time.Duration(c.service.ReadTimeout))
		err := c.Conn.SetReadDeadline(dur)
		if err != nil {
			return 0, err
		}
	}
	n, err = c.buf.Read(b)
	if err == nil {
		c.readBytes += int64(n)
		c.readAt = time.Now().Unix()
	}
	return
}

func (c *MonConn) Write(b []byte) (n int, err error) {
	if c.service.WriteTimeout > 0 {
		dur := time.Now().Add(time.Second * time.Duration(c.service.WriteTimeout))
		err := c.Conn.SetWriteDeadline(dur)
		if err != nil {
			return 0, err
		}
	}
	n, err = c.buf.Write(b)
	if err == nil {
		c.writeBytes += int64(n)
		c.writeAt = time.Now().Unix()
		c.buf.Flush()
	}
	return
}

// Close close once
func (c *MonConn) Close() (err error) {
	c.mutex.Lock()
	defer func() {
		c.mutex.Unlock()
	}()
	if !c.closed {
		err = c.Conn.Close()
		c.updateService()
		close(c.ch)
		c.closed = true
		if Debug {
			logf("service: %s connection %s closed. elapsed: %d(s).",
				c.service.Sid(),
				c.label,
				time.Now().Unix()-c.createdAt)
		}
	}
	return
}

// init after construct a MonConn, call init()
func (c *MonConn) init() {
	if c.buf == nil {
		c.buf = bufio.NewReadWriter(bufio.NewReader(c.Conn), bufio.NewWriter(c.Conn))
	}
	if c.service.KeepAlive {
		KeepAlive(c.Conn)
	}
	// label format: remote_ip:remote_port <-> server_ip:server_port
	c.label = fmt.Sprintf("%s <-> %s",
		c.Conn.RemoteAddr().String(),
		c.service.ln.Addr().String())
}

func (c *MonConn) clientIP() string {
	clientIP, _, _ := net.SplitHostPort(c.Conn.RemoteAddr().String())
	return clientIP
}

// Idle client read idle
func (c *MonConn) Idle() bool {
	duration := time.Now().Unix() - int64(c.service.MaxIdle)
	if duration < c.readAt {
		return false
	}
	return true
}

// Stats output json format stats
func (c *MonConn) Stats() string {
	format := `
{
  "label": "%s",
  "sid": "%s",
  "read_bytes": %d,
  "write_bytes": %d,
  "read_at": %d,
  "write_at": %d
}`
	return fmt.Sprintf(format,
		c.label,
		c.service.Sid(),
		c.readBytes,
		c.writeBytes,
		c.readAt,
		c.writeAt)
}

// log
func (c *MonConn) Log() string {
	format := `sid: %s, conn: %s, r: %d, w: %d, rt: %s, wt: %s`
	return fmt.Sprintf(format,
		c.service.Sid(),
		c.label,
		c.readBytes,
		c.writeBytes,
		tsFormat(c.readAt),
		tsFormat(c.writeAt))
}

// updateService when connection close
func (c *MonConn) updateService() {
	atomic.AddInt64(&c.service.readBytes, c.readBytes)
	atomic.AddInt64(&c.service.writeBytes, c.writeBytes)
	// log client access time
	if c.service.accessAt < c.readAt {
		atomic.StoreInt64(&c.service.accessAt, c.readAt)
	}
	if Debug {
		logf("MonConn updateService: %s", c.service.Log())
	}
}

// KeepAlive helper to set tcp KEEP_ALIVE param
func KeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		tcp.SetKeepAlive(true)
		tcp.SetKeepAlivePeriod(30 * time.Second)
	}
}
