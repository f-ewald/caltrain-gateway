package caltraingateway

import (
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

//go:embed web/index.html
var indexHTML []byte

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

// authMiddleware logs requests that don't provide a valid API key
func authMiddleware(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret != "" && r.Header.Get("X-API-Key") != secret {
			logRequest(r)
		}
		next(w, r)
	}
}

func logRequest(r *http.Request) {
	fmt.Printf("Received request: %s %s from %s\n", r.Method, r.URL.String(), r.RemoteAddr)
}

func logRequestMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)
		next(w, r)
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

		if response.statusCode == http.StatusOK {
			w.Write(response.body)
		} else {
			fmt.Fprintf(w, "Upstream API returned status code %d", response.statusCode)
		}
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

// timetableCollection holds the loaded timetable data for all lines
var timetableCollection *TimetableCollection

// SetTimetableCollection sets the timetable collection to be used by the timetable handler
func SetTimetableCollection(tc *TimetableCollection) {
	timetableCollection = tc
}

// timetableHandler returns all departures by stop ID as JSON
// Accepts optional query parameters:
//   - weekday (Monday, Tuesday, etc.)
//   - station (GTFS station ID to filter results)
func timetableHandler(w http.ResponseWriter, r *http.Request) {
	if timetableCollection == nil {
		http.Error(w, "Timetable not loaded", http.StatusServiceUnavailable)
		return
	}

	// Parse weekday from query parameter
	weekdayParam := r.URL.Query().Get("weekday")
	var departures map[string][]TrainDeparture

	if weekdayParam != "" {
		weekday := ParseWeekday(weekdayParam)
		if weekday == "" {
			http.Error(w, "Invalid weekday. Valid values: Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday", http.StatusBadRequest)
			return
		}
		departures = timetableCollection.GetDeparturesByStopAndWeekday(weekday)
	} else {
		departures = timetableCollection.GetDeparturesByStop()
	}

	// Filter by station ID if provided
	stationID := r.URL.Query().Get("station")
	if stationID != "" {
		if stationDepartures, exists := departures[stationID]; exists {
			departures = map[string][]TrainDeparture{stationID: stationDepartures}
		} else {
			departures = map[string][]TrainDeparture{}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(departures); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// serviceAlerts holds the loaded service alerts data
var serviceAlerts *ServiceAlertsResponse
var serviceAlertsMu sync.RWMutex

// SetServiceAlerts updates the service alerts data (thread-safe)
func SetServiceAlerts(sa *ServiceAlertsResponse) {
	serviceAlertsMu.Lock()
	defer serviceAlertsMu.Unlock()
	serviceAlerts = sa
}

// GetServiceAlerts returns the current service alerts data (thread-safe)
func GetServiceAlerts() *ServiceAlertsResponse {
	serviceAlertsMu.RLock()
	defer serviceAlertsMu.RUnlock()
	return serviceAlerts
}

// serviceAlertsHandler returns service alerts as JSON, optionally filtered by agency.
func serviceAlertsHandler(w http.ResponseWriter, r *http.Request) {
	sa := GetServiceAlerts()
	if sa == nil {
		http.Error(w, "Service alerts not loaded", http.StatusServiceUnavailable)
		return
	}

	result := sa
	if agency := r.URL.Query().Get("agency"); agency != "" {
		result = filterServiceAlertsByAgency(sa, agency)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// filterServiceAlertsByAgency returns a new ServiceAlertsResponse containing only
// entities where at least one InformedEntity has a matching AgencyID (case-insensitive).
func filterServiceAlertsByAgency(sa *ServiceAlertsResponse, agency string) *ServiceAlertsResponse {
	var filtered []ServiceAlertEntity
	for _, entity := range sa.Entity {
		for _, ie := range entity.Alert.InformedEntity {
			if strings.EqualFold(ie.AgencyID, agency) {
				filtered = append(filtered, entity)
				break
			}
		}
	}
	return &ServiceAlertsResponse{
		Header: sa.Header,
		Entity: filtered,
	}
}

// stopsHandler returns the GTFS ID to parent station name mapping as JSON
func stopsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(GTFSIDToParentName); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// statsMiddleware records each request's path in the request stats.
func statsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestStats.RecordRequest(r.URL.Path)
		next(w, r)
	}
}

// uiHandler serves the embedded dashboard HTML page.
func uiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

// uiStatsHandler returns uptime and per-endpoint request counts as JSON.
func uiStatsHandler(w http.ResponseWriter, r *http.Request) {
	uptimeSeconds, counts := requestStats.GetSnapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"uptime_seconds": uptimeSeconds,
		"endpoints":      counts,
	})
}

// SetupRoutes configures all HTTP routes.
func SetupRoutes(apiKeyPool *KeyPool, secret string) {
	http.HandleFunc("/", statsMiddleware(authMiddleware(secret, gzipMiddleware(proxyHandler(apiKeyPool)))))
	http.HandleFunc("/up", healthHandler)
	http.HandleFunc("/caltrain/timetable", statsMiddleware(authMiddleware(secret, gzipMiddleware(timetableHandler))))
	http.HandleFunc("/caltrain/stops", statsMiddleware(authMiddleware(secret, gzipMiddleware(stopsHandler))))
	http.HandleFunc("/caltrain/servicealerts", statsMiddleware(authMiddleware(secret, gzipMiddleware(serviceAlertsHandler))))
	http.HandleFunc("/caltrain/scheduletype", statsMiddleware(authMiddleware(secret, gzipMiddleware(scheduleTypeHandler(apiKeyPool)))))
	http.HandleFunc("/ui", uiHandler)
	http.HandleFunc("/ui/stats", uiStatsHandler)
}
