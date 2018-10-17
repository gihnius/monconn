package monconn

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var bufioReaderPool sync.Pool
var bufioWriterPool sync.Pool

// ReadBufSize net.Conn.Read buffer size, 4k default
var ReadBufSize = 4 << 10

// WriteBufSize net.Conn.Write buffer size, 4k default
var WriteBufSize = 4 << 10

func newBufioReader(r io.Reader) *bufio.Reader {
	if v := bufioReaderPool.Get(); v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	return bufio.NewReaderSize(r, ReadBufSize)
}

func putBufioReader(br *bufio.Reader) {
	br.Reset(nil)
	bufioReaderPool.Put(br)
}

func newBufioWriter(w io.Writer) *bufio.Writer {
	if v := bufioWriterPool.Get(); v != nil {
		bw := v.(*bufio.Writer)
		bw.Reset(w)
		return bw
	}
	return bufio.NewWriterSize(w, WriteBufSize)
}

func putBufioWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	bufioWriterPool.Put(bw)
}

// MonConn wrap net.Conn for extra monitor data
type MonConn struct {
	net.Conn
	bufr       *bufio.Reader
	bufw       *bufio.Writer
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
	if c.service.ReadTimeout != 0 {
		dur := time.Now().Add(time.Second * time.Duration(c.service.ReadTimeout))
		err := c.Conn.SetReadDeadline(dur)
		if err != nil {
			return 0, err
		}
	}
	n, err = c.bufr.Read(b)
	if err == nil {
		c.readBytes += int64(n)
		c.readAt = time.Now().Unix()
		if c.service.PrintBytes {
			logf("S[%s] R %s >>: %#v", c.service.sid, c.remoteAddr(), b[:n])
		}
	}
	return
}

func (c *MonConn) Write(b []byte) (n int, err error) {
	if c.service.WriteTimeout != 0 {
		dur := time.Now().Add(time.Second * time.Duration(c.service.WriteTimeout))
		err := c.Conn.SetWriteDeadline(dur)
		if err != nil {
			return 0, err
		}
	}
	n, err = c.bufw.Write(b)
	if err == nil {
		c.writeBytes += int64(n)
		c.writeAt = time.Now().Unix()
		c.bufw.Flush()
		if c.service.PrintBytes {
			logf("S[%s] W %s <<: %#v", c.service.sid, c.remoteAddr(), b[:n])
		}
	}
	return
}

func (c *MonConn) finalFlush() {
	if c.bufw != nil {
		c.bufw.Flush()
		putBufioWriter(c.bufw)
		c.bufw = nil
	}
	if c.bufr != nil {
		putBufioReader(c.bufr)
		c.bufr = nil
	}
}

// Close close once
func (c *MonConn) Close() (err error) {
	c.mutex.Lock()
	defer func() {
		c.mutex.Unlock()
	}()
	if !c.closed {
		c.finalFlush()
		err = c.Conn.Close()
		c.updateService()
		close(c.ch)
		c.closed = true
		if Debug {
			logf("S[%s] connection %s closed. elapsed: %d(s).",
				c.service.sid,
				c.label,
				time.Now().Unix()-c.createdAt)
		}
	}
	return
}

func (c *MonConn) remoteAddr() string {
	if c.Conn != nil {
		return c.Conn.RemoteAddr().String()
	}
	return ""
}

// init after construct a MonConn, call init()
func (c *MonConn) init() {
	if c.bufr == nil {
		c.bufr = newBufioReader(c.Conn)
	}
	if c.bufw == nil {
		c.bufw = newBufioWriter(c.Conn)
	}
	if c.service.KeepAlive {
		KeepAlive(c.Conn)
	}
	// label format: remote_ip:remote_port <-> server_ip:server_port
	c.label = fmt.Sprintf("%s <-> %s",
		c.remoteAddr(),
		c.service.ln.Addr().String())
}

func (c *MonConn) clientIP() string {
	clientIP, _, _ := net.SplitHostPort(c.remoteAddr())
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
		c.service.sid,
		c.readBytes,
		c.writeBytes,
		c.readAt,
		c.writeAt)
}

// Log ...
func (c *MonConn) Log() string {
	format := `sid: %s, conn: %s, r: %d, w: %d, rt: %s, wt: %s`
	return fmt.Sprintf(format,
		c.service.sid,
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
		logf("S[%s] updateService: %s", c.service.sid, c.service.Log())
	}
}

// KeepAlive helper to set tcp KEEP_ALIVE param
func KeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		tcp.SetKeepAlive(true)
		tcp.SetKeepAlivePeriod(30 * time.Second)
	}
}
