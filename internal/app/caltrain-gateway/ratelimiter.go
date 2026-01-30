package caltraingateway

import (
	"sync"

	"golang.org/x/time/rate"
)

// APIKey holds the actual string and its independent limiter
type APIKey struct {
	Value   string
	Limiter *rate.Limiter
}

// KeyPool manages our set of keys
type KeyPool struct {
	Keys []*APIKey
	mu   sync.Mutex
	last int // For round-robin starting point
}

func NewKeyPool(strings []string, r rate.Limit, b int) *KeyPool {
	pool := &KeyPool{}
	for _, s := range strings {
		pool.Keys = append(pool.Keys, &APIKey{
			Value:   s,
			Limiter: rate.NewLimiter(r, b),
		})
	}
	return pool
}

// GetAvailableKey looks for any key that has a token available
func (p *KeyPool) GetAvailableKey() (*APIKey, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try keys starting from the one after our last successful pick
	n := len(p.Keys)
	for i := range n {
		idx := (p.last + i) % n
		if p.Keys[idx].Limiter.Allow() {
			p.last = idx
			return p.Keys[idx], true
		}
	}

	return nil, false
}
