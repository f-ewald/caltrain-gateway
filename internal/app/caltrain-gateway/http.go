package caltraingateway

import (
	"compress/gzip"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

//go:embed web/index.html
var indexHTML []byte

//go:embed web/support_list.html
var supportListHTML string

//go:embed web/support_detail.html
var supportDetailHTML string

//go:embed web/servicealerts_list.html
var serviceAlertsListHTML string

//go:embed web/servicealerts_detail.html
var serviceAlertsDetailHTML string

const (
	defaultAPIBaseURL = "http://api.511.org/"
)

var (
	// requestGroup manages the "inflight" requests
	requestGroup singleflight.Group
	// apiBaseURL can be overridden for testing
	apiBaseURL = defaultAPIBaseURL

	// apiConnected tracks whether the 511.org API is reachable,
	// updated on startup and during periodic service alert refreshes.
	apiConnected   bool
	apiConnectedMu sync.RWMutex
)

// gzipResponseWriter wraps http.ResponseWriter to provide gzip compression.
// It tracks the status code so responses that must not carry a body, such as
// 304 Not Modified, are neither compressed nor advertised as gzip.
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	wroteHeader bool
	bodyAllowed bool
}

// bodyAllowedForStatus reports whether a response with this status may carry a
// body, mirroring the rules net/http enforces.
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	}
	return true
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.bodyAllowed = bodyAllowedForStatus(status)
		if !w.bodyAllowed {
			// No body will follow, so the gzip advertisement would be a lie.
			w.ResponseWriter.Header().Del("Content-Encoding")
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if !w.bodyAllowed {
		return len(b), nil
	}
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

		// Wrap the response writer
		gzipWriter := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next(gzipWriter, r)

		// Finish the gzip stream only when a body was actually permitted;
		// closing otherwise would append gzip framing to a bodyless response.
		if !gzipWriter.wroteHeader || gzipWriter.bodyAllowed {
			gz.Close()
		}
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

// timetableCollection is served through schedule; see schedule_state.go.

// timetableHandler returns all departures by stop ID as JSON
// Accepts optional query parameters:
//   - weekday (Monday, Tuesday, etc.)
//   - station (GTFS station ID to filter results)
//
// The response carries a weak ETag so clients can revalidate a cached copy with
// If-None-Match instead of re-downloading the payload.
func timetableHandler(w http.ResponseWriter, r *http.Request) {
	collection, version, _, _, _ := schedule.Snapshot()
	if collection == nil {
		http.Error(w, "Timetable not loaded", http.StatusServiceUnavailable)
		return
	}

	// Parse weekday from query parameter
	weekdayParam := r.URL.Query().Get("weekday")
	stationID := r.URL.Query().Get("station")

	if weekdayParam != "" && ParseWeekday(weekdayParam) == "" {
		http.Error(w, "Invalid weekday. Valid values: Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday", http.StatusBadRequest)
		return
	}

	// Validate before short-circuiting on the ETag, so a malformed request is
	// still reported rather than answered with 304.
	if etag := scheduleETag(version, weekdayParam, stationID); etag != "" {
		w.Header().Set("ETag", etag)
		if requestMatchesETag(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	var departures map[string][]TrainDeparture
	if weekdayParam != "" {
		departures = collection.GetDeparturesByStopAndWeekday(ParseWeekday(weekdayParam))
	} else {
		departures = collection.GetDeparturesByStop()
	}

	// Filter by station ID if provided
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

// requestMatchesETag reports whether the request's If-None-Match header covers
// the given tag. A "*" matches any existing representation, and comparison
// ignores the weak prefix as RFC 9110 requires for this header.
func requestMatchesETag(r *http.Request, etag string) bool {
	header := r.Header.Get("If-None-Match")
	if header == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		if etagsEquivalent(strings.TrimSpace(candidate), etag) {
			return true
		}
	}
	return false
}

// etagsEquivalent compares two entity tags disregarding the weakness prefix.
func etagsEquivalent(a, b string) bool {
	return strings.TrimPrefix(a, "W/") == strings.TrimPrefix(b, "W/")
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
		"version":        BuildVersion(),
	})
}

// SetAPIConnected updates the API connectivity status.
func SetAPIConnected(connected bool) {
	apiConnectedMu.Lock()
	defer apiConnectedMu.Unlock()
	apiConnected = connected
}

// IsAPIConnected returns the current API connectivity status.
func IsAPIConnected() bool {
	apiConnectedMu.RLock()
	defer apiConnectedMu.RUnlock()
	return apiConnected
}

// uiHealthHandler returns the connectivity status of external dependencies as JSON.
// No secrets or connection details are exposed.
func uiHealthHandler(w http.ResponseWriter, r *http.Request) {
	dbOk := false
	if DB != nil {
		dbOk = DB.Ping() == nil
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"api":      IsAPIConnected(),
		"database": dbOk,
	})
}

// basicAuthMiddleware protects a handler with HTTP Basic Authentication.
func basicAuthMiddleware(expectedUser, expectedPass string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// supportListHandler renders the list of all support requests.
func supportListHandler(w http.ResponseWriter, r *http.Request) {
	requests, err := GetAllSupportRequests()
	if err != nil {
		http.Error(w, "Failed to load support requests", http.StatusInternalServerError)
		return
	}
	renderAdminPage(w, "support_list", supportListHTML,
		newAdminPage(tabSupport, "Support requests",
			struct{ Requests []SupportRequestRow }{Requests: requests}))
}

// supportDetailHandler renders the details of a single support request.
func supportDetailHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return
	}
	req, err := GetSupportRequestByID(id)
	if err != nil {
		http.Error(w, "Failed to load support request", http.StatusInternalServerError)
		return
	}
	if req == nil {
		http.Error(w, "Support request not found", http.StatusNotFound)
		return
	}
	renderAdminPage(w, "support_detail", supportDetailHTML,
		newAdminPage(tabSupport, "Support request", req))
}

// supportDeleteHandler deletes a support request and redirects to the list page.
func supportDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return
	}
	if err := DeleteSupportRequest(id); err != nil {
		http.Error(w, "Failed to delete support request", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/support", http.StatusFound)
}

// adminIndexHandler serves the embedded admin landing page. Because the route
// adminIndexHandler redirects the admin root to the first tab. Because the route
// is registered as "/admin/" — a subtree pattern — this handler also receives
// any unknown /admin/... path; those are rejected with 404 so they cannot
// silently fall through.
func adminIndexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/" && r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, adminTabs[0].Href, http.StatusFound)
}

// serviceAlertsListHandler renders the list of all persisted service alerts.
func serviceAlertsListHandler(w http.ResponseWriter, r *http.Request) {
	alerts, err := GetAllServiceAlerts()
	if err != nil {
		http.Error(w, "Failed to load service alerts", http.StatusInternalServerError)
		return
	}
	renderAdminPage(w, "servicealerts_list", serviceAlertsListHTML,
		newAdminPage(tabServiceAlerts, "Service alerts",
			struct{ Alerts []ServiceAlertRow }{Alerts: alerts}))
}

// serviceAlertDetailView extends a ServiceAlertRow with derived fields used by
// the detail template.
type serviceAlertDetailView struct {
	*ServiceAlertRow
	FeedTimestampUTC string
}

// serviceAlertsDetailHandler renders the details of a single persisted alert.
func serviceAlertsDetailHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return
	}
	alert, err := GetServiceAlertByID(id)
	if err != nil {
		http.Error(w, "Failed to load service alert", http.StatusInternalServerError)
		return
	}
	if alert == nil {
		http.Error(w, "Service alert not found", http.StatusNotFound)
		return
	}
	view := serviceAlertDetailView{ServiceAlertRow: alert}
	if alert.FeedTimestamp > 0 {
		view.FeedTimestampUTC = time.Unix(alert.FeedTimestamp, 0).UTC().Format("2006-01-02 15:04:05 MST")
	}
	renderAdminPage(w, "servicealerts_detail", serviceAlertsDetailHTML,
		newAdminPage(tabServiceAlerts, "Service alert", view))
}

// serviceAlertsDeleteHandler deletes a persisted alert and redirects to the list page.
func serviceAlertsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return
	}
	if err := DeleteServiceAlert(id); err != nil {
		http.Error(w, "Failed to delete service alert", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/servicealerts", http.StatusFound)
}

// serviceAlertsExportHandler streams every persisted service alert as JSONL
// (one JSON object per line). The response uses Content-Disposition: attachment
// so browsers offer a download dialog.
func serviceAlertsExportHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="service-alerts.jsonl"`)

	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)

	count := 0
	err := StreamAllServiceAlerts(func(row ServiceAlertRow) error {
		if err := encoder.Encode(row); err != nil {
			return err
		}
		count++
		if flusher != nil && count%200 == 0 {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		// Headers are already sent by this point if any rows were written;
		// the best we can do is log and let the client see a truncated stream.
		fmt.Printf("service alerts export failed after %d rows: %v\n", count, err)
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// SetupRoutes builds the routing table and returns it.
//
// Routes are registered on a dedicated mux rather than http.DefaultServeMux so
// the table can be constructed in tests without duplicate-registration panics or
// state leaking between them.
//
// There is deliberately no "/" pattern: in net/http that is the catch-all, and
// registering the proxy there made the service forward every unrecognised path
// upstream. Root-level 511 traffic is served by an explicit "/transit/" route
// instead, and anything else now returns 404.
func SetupRoutes(apiKeyPool *KeyPool, secret, dbUsername, dbPassword string) *http.ServeMux {
	mux := http.NewServeMux()

	proxy := statsMiddleware(authMiddleware(secret, gzipMiddleware(proxyHandler(apiKeyPool))))
	// The prefix is intentionally left in place, so this reaches the proxy with
	// the same path it has today and keeps the cache key identical to the
	// equivalent /proxy/transit/... request.
	mux.HandleFunc("/transit/", proxy)
	mux.HandleFunc("/proxy/", statsMiddleware(authMiddleware(secret, gzipMiddleware(http.StripPrefix("/proxy", proxyHandler(apiKeyPool)).ServeHTTP))))

	mux.HandleFunc("/up", healthHandler)
	mux.HandleFunc("/caltrain/timetable", statsMiddleware(authMiddleware(secret, gzipMiddleware(timetableHandler))))
	mux.HandleFunc("/caltrain/timetable/version", statsMiddleware(authMiddleware(secret, gzipMiddleware(scheduleVersionHandler))))
	mux.HandleFunc("/caltrain/stops", statsMiddleware(authMiddleware(secret, gzipMiddleware(stopsHandler))))
	mux.HandleFunc("/caltrain/servicealerts", statsMiddleware(authMiddleware(secret, gzipMiddleware(serviceAlertsHandler))))
	mux.HandleFunc("/caltrain/scheduletype", statsMiddleware(authMiddleware(secret, gzipMiddleware(scheduleTypeHandler(apiKeyPool)))))
	mux.HandleFunc("/ui", uiHandler)
	mux.HandleFunc("/ui/stats", uiStatsHandler)
	mux.HandleFunc("/ui/health", uiHealthHandler)
	mux.HandleFunc("/support", statsMiddleware(logRequestMiddleware(supportHandler)))

	registerAdminRoutes(mux, dbUsername, dbPassword)
	return mux
}

// registerAdminRoutes wires the admin section, or a notice explaining why it is
// unavailable. The pages authenticate with the database credentials, so without
// a username there is nothing to authenticate against.
func registerAdminRoutes(mux *http.ServeMux, dbUsername, dbPassword string) {
	if dbUsername == "" {
		mux.HandleFunc("/admin/", adminUnavailableHandler)
		return
	}

	// Admin pages — protected by basic auth using database credentials
	mux.HandleFunc("/admin/", basicAuthMiddleware(dbUsername, dbPassword, adminIndexHandler))
	mux.HandleFunc("/admin/support", basicAuthMiddleware(dbUsername, dbPassword, supportListHandler))
	mux.HandleFunc("/admin/support/detail", basicAuthMiddleware(dbUsername, dbPassword, supportDetailHandler))
	mux.HandleFunc("/admin/support/delete", basicAuthMiddleware(dbUsername, dbPassword, supportDeleteHandler))
	mux.HandleFunc("/admin/servicealerts", basicAuthMiddleware(dbUsername, dbPassword, serviceAlertsListHandler))
	mux.HandleFunc("/admin/servicealerts/detail", basicAuthMiddleware(dbUsername, dbPassword, serviceAlertsDetailHandler))
	mux.HandleFunc("/admin/servicealerts/delete", basicAuthMiddleware(dbUsername, dbPassword, serviceAlertsDeleteHandler))
	mux.HandleFunc("/admin/servicealerts/export", basicAuthMiddleware(dbUsername, dbPassword, serviceAlertsExportHandler))
	mux.HandleFunc("/admin/departures", basicAuthMiddleware(dbUsername, dbPassword, departuresListHandler))
	mux.HandleFunc("/admin/departures/detail", basicAuthMiddleware(dbUsername, dbPassword, departuresDetailHandler))
	mux.HandleFunc("/admin/departures/delete", basicAuthMiddleware(dbUsername, dbPassword, departuresDeleteHandler))
	mux.HandleFunc("/admin/departures/export", basicAuthMiddleware(dbUsername, dbPassword, departuresExportHandler))
}
