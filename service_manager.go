package monconn

import (
	"sync"
)

// serviceManager for multiple services
type serviceManager struct {
	sync.Mutex
	store map[string]*Service // service["sid"] = &Service
}

var sm = serviceManager{store: map[string]*Service{}}

// GetService by service id (sid)
func GetService(sid string) (*Service, bool) {
	return sm.get(sid)
}

// NewService add a new service, named by sid
func NewService(sid string) *Service {
	if sid == "" {
		sid = "default"
	}
	if _, ok := GetService(sid); ok {
		logf("service with sid: %s already exists!", sid)
		return nil
	}
	return sm.add(sid)
}

// DelService delete a service from cache
func DelService(sid string) {
	sm.del(sid)
}

// ServicesList list all live services
func ServicesList() map[string]*Service {
	return sm.store
}

func (sm *serviceManager) add(sid string) (s *Service) {
	sm.Lock()
	defer sm.Unlock()
	s = newService()
	s.sid = sid
	sm.store[sid] = s
	logf("Added [%s] service.", sid)
	return
}

func (sm *serviceManager) get(sid string) (s *Service, ok bool) {
	sm.Lock()
	defer sm.Unlock()
	s, ok = sm.store[sid]
	return
}

func (sm *serviceManager) del(sid string) {
	sm.Lock()
	defer sm.Unlock()
	if s, ok := sm.store[sid]; ok {
		delete(sm.store, sid)
		s.Stop()
		logf("deleted %s service.", sid)
	}
}
