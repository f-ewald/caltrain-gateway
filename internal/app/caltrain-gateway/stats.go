package caltraingateway

import (
	"sync"
	"time"
)

// RequestStats tracks per-endpoint request counts and server uptime.
type RequestStats struct {
	mu        sync.Mutex
	counts    map[string]int64
	startTime time.Time
}

var requestStats = &RequestStats{
	counts:    make(map[string]int64),
	startTime: time.Now(),
}

// RecordRequest increments the request count for the given path.
func (s *RequestStats) RecordRequest(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[path]++
}

// GetSnapshot returns the uptime in seconds and a copy of the endpoint counts.
func (s *RequestStats) GetSnapshot() (int64, map[string]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	uptimeSeconds := int64(time.Since(s.startTime).Seconds())
	counts := make(map[string]int64, len(s.counts))
	for k, v := range s.counts {
		counts[k] = v
	}
	return uptimeSeconds, counts
}
