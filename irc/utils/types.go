package utils

import "sync"

type SetMap struct {
	m  map[string]bool
	mu *sync.RWMutex
}

func NewSetMap() SetMap {
	return SetMap{
		m:  make(map[string]bool),
		mu: &sync.RWMutex{},
	}
}

func NewSetMapWithCap(capacity int) SetMap {
	return SetMap{
		m:  make(map[string]bool, capacity),
		mu: &sync.RWMutex{},
	}
}

func (s SetMap) Self() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m
}

func (s SetMap) Has(str string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[str]
	return ok
}

func (s SetMap) Add(str string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[str] = true
}

func (s SetMap) Subtract(str string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[str]; !ok {
		return
	}
	delete(s.m, str)
}
