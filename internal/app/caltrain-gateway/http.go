package caltraingateway

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/sync/singleflight"
)

const (
	apiBaseURL = "http://api.511.org/"
)

var (
	// requestGroup manages the "inflight" requests
	requestGroup singleflight.Group
)

// gzipResponseWriter wraps http.ResponseWriter to provide gzip compression
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// gzipMiddleware wraps an http.Handler with gzip compression
func gzipMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip encoding
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}

		// Set the content encoding header
		w.Header().Set("Content-Encoding", "gzip")

		// Create gzip writer
		gz := gzip.NewWriter(w)
		defer gz.Close()

		// Wrap the response writer
		gzipWriter := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next(gzipWriter, r)
	}
}

// proxyHandler handles proxying requests to the 511 API
func proxyHandler(apiKeyPool *KeyPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cacheKey := r.URL.String()

		// 1. Check Cache
		if cachedResponse, found := Cache.Get(cacheKey); found {
			w.Header().Set("X-Cache", "HIT")
			w.Write(cachedResponse.([]byte))
			return
		}

		// 2. Request Collapsing
		// Only one goroutine will execute this function for a given key.
		// Others will block until the first one returns.
		data, err, shared := requestGroup.Do(cacheKey, func() (any, error) {
			// Retrieve API key from the pool
			apiKey, ok := apiKeyPool.GetAvailableKey()
			if !ok {
				return nil, fmt.Errorf("no available API keys")
			}

			fmt.Println("Fetching from API for key:", cacheKey)

			// Add API key to the request
			q := r.URL.Query()
			q.Add("api_key", apiKey.Value)
			r.URL.RawQuery = q.Encode()

			realApiUrl := apiBaseURL + r.URL.Path + "?" + r.URL.RawQuery
			resp, err := http.Get(realApiUrl)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}

			// 3. Store in cache
			Cache.Set(cacheKey, body, DefaultExpiration)
			return body, nil
		})

		if err != nil {
			switch err.Error() {
			case "no available API keys":
				http.Error(w, "Rate limit exceeded for all API keys", http.StatusTooManyRequests)
			default:
				http.Error(w, "External API Error", http.StatusBadGateway)
			}
			return
		}

		// 4. Return result
		w.Header().Set("X-Cache", "MISS")
		if shared {
			w.Header().Set("X-Collapsed", "TRUE")
		}
		w.Write(data.([]byte))
	}
}

// healthHandler returns a simple OK response for health checks
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

// setupRoutes configures all HTTP routes
func SetupRoutes(apiKeyPool *KeyPool) {
	http.HandleFunc("/", gzipMiddleware(proxyHandler(apiKeyPool)))
	http.HandleFunc("/up", healthHandler)
}
