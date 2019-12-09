package cmd

import (
	"sync"

	"go.uber.org/atomic"
)

// Metrics - represents bytes served
type Metrics struct {
	BytesReceived atomic.Uint64
	BytesSent     atomic.Uint64
	RequestStats  map[string]int
	sync.RWMutex
}

// IncBytesReceived - Increase total bytes received from radio backend
func (s *Metrics) IncBytesReceived(n int64) {
	s.BytesReceived.Add(uint64(n))
}

// GetBytesReceived - Get total bytes received from radio backend
func (s *Metrics) GetBytesReceived() uint64 {
	return s.BytesReceived.Load()
}

// IncBytesSent - Increase total bytes sent to radio backend
func (s *Metrics) IncBytesSent(n int64) {
	s.BytesSent.Add(uint64(n))
}

// GetBytesSent - Get total bytes received from radio backend
func (s *Metrics) GetBytesSent() uint64 {
	return s.BytesSent.Load()
}

// IncRequests - Increase request sent to radio backend by 1
func (s *Metrics) IncRequests(method string) {
	s.Lock()
	defer s.Unlock()
	if s == nil {
		return
	}
	if s.RequestStats == nil {
		s.RequestStats = make(map[string]int)
	}
	if _, ok := s.RequestStats[method]; ok {
		s.RequestStats[method]++
		return
	}
	s.RequestStats[method] = 1
}

// GetRequests - Get total number of requests sent to radio backend
func (s *Metrics) GetRequests() map[string]int {
	return s.RequestStats
}

// NewMetrics - Prepare new Metrics structure
func NewMetrics() *Metrics {
	return &Metrics{}
}
