package irc

import (
	"sync/atomic"
)

type StatsValues struct {
	Unknown   int32 // unregistered clients
	Total     int32 // registered clients, including invisible
	Max       int32 // high-water mark of registered clients
	Invisible int32
	Operators int32
}

// Stats tracks statistics for a running server
type Stats struct {
	StatsValues
}

// Add Adds an unregistered client
func (s *Stats) Add() {
	atomic.AddInt32(&s.Unknown, 1)
}

// AddRegistered Activates a registered client, e.g., for the initial attach to a persistent client
func (s *Stats) AddRegistered(invisible, operator bool) {
	if invisible {
		atomic.AddInt32(&s.Invisible, 1)
	}
	if operator {
		atomic.AddInt32(&s.Operators, 1)
	}
	atomic.AddInt32(&s.Total, 1)
	s.setMax()
}

// Register Transition a client from unregistered to registered
func (s *Stats) Register(invisible bool) {
	atomic.AddInt32(&s.Unknown, -1)
	if invisible {
		atomic.AddInt32(&s.Invisible, 1)
	}
	atomic.AddInt32(&s.Total, 1)
	s.setMax()
}

func (s *Stats) setMax() {
	if s.Max < s.Total {
		s.Max = s.Total
	}
}

// ChangeInvisible Modify the Invisible count
func (s *Stats) ChangeInvisible(increment int) {
	atomic.AddInt32(&s.Invisible, int32(increment))
}

// ChangeOperators Modify the Operator count
func (s *Stats) ChangeOperators(increment int) {
	atomic.AddInt32(&s.Operators, int32(increment))
}

// Remove a user from the server
func (s *Stats) Remove(registered, invisible, operator bool) {
	switch registered {
	case true:
		atomic.AddInt32(&s.Total, -1)
	default:
		atomic.AddInt32(&s.Unknown, -1)
	}
	if invisible {
		atomic.AddInt32(&s.Invisible, -1)
	}
	if operator {
		atomic.AddInt32(&s.Operators, -1)
	}
}

// GetValues GetStats retrives total, invisible and oper count
func (s *Stats) GetValues() (result StatsValues) {
	result = s.StatsValues
	return
}
