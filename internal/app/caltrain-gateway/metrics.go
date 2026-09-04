package caltraingateway

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metricsNamespace prefixes every custom metric this service exports, per
// Prometheus naming convention (namespace_subsystem_name).
const metricsNamespace = "caltrain_gateway"

var (
	// httpRequestsTotal counts requests per registered route. The route label
	// is the mux registration pattern (e.g. "/caltrain/timetable"), supplied at
	// the registration call site, not the raw request path: prefix routes such
	// as /proxy/ and /transit/ forward arbitrary upstream paths, which would
	// otherwise make the path an unbounded label.
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "http_requests_total",
		Help:      "Total HTTP requests, labelled by route, method and status code.",
	}, []string{"route", "method", "status"})

	// httpRequestDuration measures handler latency per route, in seconds.
	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration in seconds, labelled by route and method.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route", "method"})

	// cacheResultsTotal counts 511 proxy response cache hits and misses.
	cacheResultsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "proxy_cache_results_total",
		Help:      "511 proxy response cache results, labelled hit or miss.",
	}, []string{"result"})

	// upstreamRequestsTotal counts outbound calls to the 511 API by outcome.
	upstreamRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "upstream_requests_total",
		Help:      "Requests to the 511 upstream API, labelled by outcome (success, error, rate_limited).",
	}, []string{"outcome"})

	// upstreamRequestDuration measures latency of the actual 511 API call, in seconds.
	upstreamRequestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "upstream_request_duration_seconds",
		Help:      "Latency of outbound calls to the 511 upstream API, in seconds.",
		Buckets:   prometheus.DefBuckets,
	})
)

// metricsResponseWriter tracks the status code written, defaulting to 200 as
// net/http does when a handler never calls WriteHeader.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// metricsMiddleware records request count and duration for a route, labelled
// with the caller-supplied route name rather than the raw request path.
func metricsMiddleware(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mrw := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}

		next(mrw, r)

		httpRequestDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		httpRequestsTotal.WithLabelValues(route, r.Method, strconv.Itoa(mrw.status)).Inc()
	}
}
