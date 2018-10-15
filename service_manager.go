package monconn

import (
	"bytes"
	"strings"
	"sync"
)

// serviceManager for multiple services
type serviceManager struct {
	sync.Mutex
	store map[string]*Service // service["sid"] = &Service
}

var sm = serviceManager{store: map[string]*Service{}}

// NewService add a new service, named by sid
func NewService(sid string) (s *Service) {
	sm.Lock()
	defer sm.Unlock()
	if sid == "" {
		sid = "default"
	}
	s = newService()
	s.sid = sid
	sm.store[sid] = s
	logf("Added service %s.", sid)
	return
}

// GetService
func GetService(sid string) (s *Service, ok bool) {
	sm.Lock()
	defer sm.Unlock()
	s, ok = sm.store[sid]
	return
}

// DelService delete a service
func DelService(sid string) {
	sm.Lock()
	defer sm.Unlock()
	if s, ok := sm.store[sid]; ok {
		delete(sm.store, sid)
		s.Stop()
		logf("deleted service %s.", sid)
	}
}

// ServicesList list all live services
func ServicesList() map[string]*Service {
	return sm.store
}

// TotalConns total connections count
func TotalConns() (res int64) {
	for _, s := range sm.store {
		res += s.connCount
	}
	return
}

// IPs all client ips, return a comma-seperated string
func IPs() string {
	var buffer bytes.Buffer
	for _, s := range sm.store {
		buffer.WriteString(strings.Join(s.IPs(), ","))
	}
	return buffer.String()
}

// Shutdown stop all services
func Shutdown() {
	for _, s := range sm.store {
		s.Stop()
	}
}
