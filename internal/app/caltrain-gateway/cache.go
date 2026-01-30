package caltraingateway

import (
	"time"

	"github.com/patrickmn/go-cache"
)

// Cache is a global cache instance with 2 minute TTL and 10 minute cleanup interval.
var Cache = cache.New(2*time.Minute, 10*time.Minute)

var DefaultExpiration = cache.DefaultExpiration
