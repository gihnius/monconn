package monconn

import (
	"bytes"
	"strconv"
	"sync"
)

// MaxIPLimit max concurrency client(ip) for a service
var MaxIPLimit int64 = 64

// IPBucket key value cache for ip <-> connections
type IPBucket struct {
	ips *sync.Map
	max int64
}

// Add simple Set operates on IPBucket
func (b *IPBucket) Add(ip string) (ok bool) {
	if b.Count() > b.max {
		ok = false
		return
	}
	if value, exists := b.ips.Load(ip); exists {
		b.ips.Store(ip, value.(int)+1)
	} else {
		b.ips.Store(ip, 0)
	}
	ok = true
	return
}

// Contains check ip in bucket
func (b *IPBucket) Contains(ip string) (ok bool) {
	_, ok = b.ips.Load(ip)
	return
}

// Remove delete a key(if value <= 0) or decrement
func (b *IPBucket) Remove(ip string) (ok bool) {
	if value, exists := b.ips.Load(ip); exists {
		if value.(int) <= 0 {
			b.ips.Delete(ip)
		} else {
			b.ips.Store(ip, value.(int)-1)
		}
		ok = true
	}
	return
}

// Count total ips in bucket
func (b *IPBucket) Count() (res int64) {
	b.ips.Range(func(k, _ interface{}) bool {
		res++
		return true
	})
	return
}

// IPs list ips in bucket
func (b *IPBucket) IPs() (res []string) {
	b.ips.Range(func(k, _ interface{}) bool {
		res = append(res, k.(string))
		return true
	})
	return
}

// Log output ip and connections count
func (b *IPBucket) Log() string {
	var buffer bytes.Buffer
	buffer.WriteString("\n")
	b.ips.Range(func(k, v interface{}) bool {
		buffer.WriteString(k.(string))
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(v.(int)))
		return true
	})
	return buffer.String()
}
