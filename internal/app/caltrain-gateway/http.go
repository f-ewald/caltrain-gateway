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
	defaultAPIBaseURL = "http://api.511.org/"
)

var (
	// requestGroup manages the "inflight" requests
	requestGroup singleflight.Group
	// apiBaseURL can be overridden for testing
	apiBaseURL = defaultAPIBaseURL
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

// apiResponse holds the response from the upstream API
type apiResponse struct {
	statusCode  int
	contentType string
	body        []byte
}

// proxyHandlerWithBaseURL handles proxying requests to the 511 API with a configurable base URL
func proxyHandlerWithBaseURL(apiKeyPool *KeyPool, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cacheKey := r.URL.String()

		// 1. Check Cache
		if cachedData, found := Cache.Get(cacheKey); found {
			cached := cachedData.(*apiResponse)
			if cached.contentType != "" {
				w.Header().Set("Content-Type", cached.contentType)
			}
			w.Header().Set("X-Cache", "HIT")
			w.Write(cached.body)
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

			// Remove existing api_key if present
			q.Del("api_key")

			q.Add("api_key", apiKey.Value)
			r.URL.RawQuery = q.Encode()

			realApiUrl := baseURL + r.URL.Path + "?" + r.URL.RawQuery
			resp, err := http.Get(realApiUrl)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}

			response := &apiResponse{
				statusCode:  resp.StatusCode,
				contentType: resp.Header.Get("Content-Type"),
				body:        body,
			}

			// 3. Store in cache only if status code is 200
			if resp.StatusCode == http.StatusOK {
				Cache.Set(cacheKey, response, DefaultExpiration)
			}
			return response, nil
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
		response := data.(*apiResponse)
		if response.contentType != "" {
			w.Header().Set("Content-Type", response.contentType)
		}
		w.Header().Set("X-Cache", "MISS")
		if shared {
			w.Header().Set("X-Collapsed", "TRUE")
		}
		w.WriteHeader(response.statusCode)
		w.Write(response.body)
	}
}

// proxyHandler handles proxying requests to the 511 API using the default base URL
func proxyHandler(apiKeyPool *KeyPool) http.HandlerFunc {
	return proxyHandlerWithBaseURL(apiKeyPool, apiBaseURL)
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
