package caltraingateway

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// resetCache clears the shared response cache so a response cached by one test
// cannot mask a missing upstream call in another.
func resetCache(t *testing.T) {
	t.Helper()
	Cache.Flush()
	t.Cleanup(Cache.Flush)
}

// openStubDB returns a non-nil *sql.DB without contacting a database. sql.Open
// is lazy, so this is enough for code that only checks whether a handle exists.
func openStubDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", "postgres://127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatalf("failed to create a stub database handle: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newRoutingHarness builds the real routing table against a mock upstream, and
// reports which paths the upstream was asked for.
//
// Recording upstream calls is the point: asserting a 404 alone would not prove
// the catch-all is gone, only that the response looked right. A path that must
// not be proxied has to leave the upstream untouched.
func newRoutingHarness(t *testing.T, dbUsername, dbPassword string) (*http.ServeMux, *[]string, *atomic.Int32) {
	t.Helper()

	var upstreamPaths []string
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		upstreamPaths = append(upstreamPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	originalBaseURL := apiBaseURL
	apiBaseURL = upstream.URL + "/"
	t.Cleanup(func() { apiBaseURL = originalBaseURL })

	// A fresh cache per harness, so a response cached by one case cannot mask a
	// missing upstream call in another.
	resetCache(t)

	mux := SetupRoutes(NewKeyPool([]string{"test-key"}, 100, 100), "", dbUsername, dbPassword)
	return mux, &upstreamPaths, &upstreamCalls
}

// TestRoutingProxyPaths pins which paths reach 511 and which no longer do.
func TestRoutingProxyPaths(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantStatus   int
		wantUpstream string // the path the upstream should see; "" means it must not be called
	}{
		{
			name:       "root transit path is still proxied unchanged",
			path:       "/transit/lines?operator_id=CT",
			wantStatus: http.StatusOK,
			// The doubled slash is long-standing behaviour: the proxy joins
			// baseURL, which ends in "/", with the request path, which begins
			// with one. 511 accepts it, and both supported forms produce the
			// same upstream URL.
			wantUpstream: "//transit/lines",
		},
		{
			name:         "proxy prefix is stripped before forwarding",
			path:         "/proxy/transit/lines?operator_id=CT",
			wantStatus:   http.StatusOK,
			wantUpstream: "//transit/lines",
		},
		{
			name:       "bare root is no longer proxied",
			path:       "/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unrelated path is no longer proxied",
			path:       "/favicon.ico",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown top level path is no longer proxied",
			path:       "/traffic/events",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, upstreamPaths, upstreamCalls := newRoutingHarness(t, "", "")

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, recorder.Code)
			}
			if tt.wantUpstream == "" {
				if calls := upstreamCalls.Load(); calls != 0 {
					t.Errorf("expected no upstream call, got %d for %v", calls, *upstreamPaths)
				}
				return
			}
			if calls := upstreamCalls.Load(); calls != 1 {
				t.Fatalf("expected exactly 1 upstream call, got %d", calls)
			}
			if got := (*upstreamPaths)[0]; got != tt.wantUpstream {
				t.Errorf("expected the upstream to receive %s, got %s", tt.wantUpstream, got)
			}
		})
	}
}

// TestRoutingTransitAndProxyShareCache confirms the two supported forms remain
// interchangeable: /transit/ is deliberately not prefix-stripped, so it produces
// the same cache key as /proxy/transit/ and the second request is served from
// cache rather than hitting 511 again.
func TestRoutingTransitAndProxyShareCache(t *testing.T) {
	mux, _, upstreamCalls := newRoutingHarness(t, "", "")

	for _, path := range []string{"/transit/lines?operator_id=CT", "/proxy/transit/lines?operator_id=CT"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", path, recorder.Code)
		}
	}

	if calls := upstreamCalls.Load(); calls != 1 {
		t.Errorf("expected the two forms to share one cached response, got %d upstream calls", calls)
	}
}

// TestRoutingRegisteredEndpoints checks the gateway's own endpoints are
// untouched by removing the catch-all, and in particular that they are answered
// locally rather than forwarded.
func TestRoutingRegisteredEndpoints(t *testing.T) {
	paths := []string{
		"/up",
		"/caltrain/timetable",
		"/caltrain/timetable/version",
		"/caltrain/stops",
		"/caltrain/servicealerts",
		"/ui",
		"/ui/stats",
		"/ui/health",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			mux, _, upstreamCalls := newRoutingHarness(t, "", "")

			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

			if recorder.Code == http.StatusNotFound {
				t.Errorf("%s should still be registered, got 404", path)
			}
			if calls := upstreamCalls.Load(); calls != 0 {
				t.Errorf("%s must be served locally, but reached the upstream", path)
			}
		})
	}
}

// TestRoutingAdminUnavailable covers the case that previously produced a
// misleading upstream 401: with no database credentials the admin routes were
// unregistered, so /admin fell through to the catch-all and was proxied.
func TestRoutingAdminUnavailable(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"admin root", "/admin/"},
		{"admin root without trailing slash", "/admin"},
		{"admin subpage", "/admin/departures"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _, upstreamCalls := newRoutingHarness(t, "", "")

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			mux.ServeHTTP(recorder, request)

			// "/admin" redirects to "/admin/" before the handler runs.
			if recorder.Code == http.StatusMovedPermanently {
				return
			}
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", recorder.Code)
			}
			if calls := upstreamCalls.Load(); calls != 0 {
				t.Error("the admin notice must never be proxied upstream")
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "Admin is unavailable") {
				t.Error("expected the notice to explain that admin is unavailable")
			}
			if !strings.Contains(body, "DATABASE_URL") {
				t.Error("expected the notice to name the setting that enables it")
			}
		})
	}
}

// TestRoutingAdminRegisteredWithCredentials confirms the real admin routes are
// still wired when credentials exist, and are protected.
func TestRoutingAdminRegisteredWithCredentials(t *testing.T) {
	mux, _, _ := newRoutingHarness(t, "dbuser", "dbpass")

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected the admin section to require auth, got %d", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request.SetBasicAuth("dbuser", "dbpass")
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Errorf("expected the admin root to redirect to the first tab, got %d", recorder.Code)
	}
}

// TestAdminUnavailableExplainsMissingCredentials distinguishes the two causes,
// which need different fixes.
func TestAdminUnavailableExplainsMissingCredentials(t *testing.T) {
	tests := []struct {
		name       string
		connected  bool
		wantPhrase string
	}{
		{"no connection", false, "No database connection"},
		{"connected without credentials", true, "no credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := DB
			if tt.connected {
				// A non-nil handle is enough; the handler only checks presence.
				DB = openStubDB(t)
			} else {
				DB = nil
			}
			defer func() { DB = previous }()

			recorder := httptest.NewRecorder()
			adminUnavailableHandler(recorder, httptest.NewRequest(http.MethodGet, "/admin/", nil))

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d", recorder.Code)
			}
			if body := recorder.Body.String(); !strings.Contains(body, tt.wantPhrase) {
				t.Errorf("expected the notice to mention %q", tt.wantPhrase)
			}
		})
	}
}
