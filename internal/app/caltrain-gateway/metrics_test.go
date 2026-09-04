package caltraingateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsMiddleware_RecordsRequestCountAndStatus(t *testing.T) {
	httpRequestsTotal.Reset()

	handler := metricsMiddleware("/test/route", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/route", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("/test/route", http.MethodGet, "201"))
	if got != 1 {
		t.Errorf("expected 1 recorded request, got %v", got)
	}
}

func TestMetricsMiddleware_DefaultsToStatus200WhenUnset(t *testing.T) {
	httpRequestsTotal.Reset()

	handler := metricsMiddleware("/test/default", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok")) // never calls WriteHeader explicitly
	})

	req := httptest.NewRequest(http.MethodGet, "/test/default", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("/test/default", http.MethodGet, "200"))
	if got != 1 {
		t.Errorf("expected 1 recorded request with default status 200, got %v", got)
	}
}

func TestMetricsMiddleware_RecordsDuration(t *testing.T) {
	handler := metricsMiddleware("/test/duration", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/duration", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	count := testutil.CollectAndCount(httpRequestDuration, "caltrain_gateway_http_request_duration_seconds")
	if count == 0 {
		t.Error("expected at least one duration observation to be registered")
	}
}
