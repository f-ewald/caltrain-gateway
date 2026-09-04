package caltraingateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// agencyRouteError is the JSON body returned when an /agency/{operator}/...
// route cannot serve the requested agency.
type agencyRouteError struct {
	Error string `json:"error"`
}

// writeAgencyRouteError writes a JSON error response for an unsupported or
// unknown agency, at the given status code.
func writeAgencyRouteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(agencyRouteError{Error: message})
}

// resolveOperator reads the {operator} path value (case-insensitive),
// defaulting to Caltrain (CT) for requests that don't carry one — the legacy
// /caltrain/... routes, and any direct handler invocation such as in tests.
func resolveOperator(r *http.Request) string {
	if operator := r.PathValue("operator"); operator != "" {
		return strings.ToUpper(operator)
	}
	return departureOperatorID
}

// agencyGatedHandler wraps a handler that only serves data for a fixed set of
// agencies with locally-loaded data (today: CT and BA's timetable, its
// version, and the stop-ID map). It reads the {operator} path value
// (case-insensitive) and passes the request through unchanged when it is in
// supportedIDs; otherwise it responds 404, distinguishing an agency 511
// doesn't know about at all from one that is real but has no data loaded for,
// using the agency directory (agencies.go) when available.
//
// Adding a new agency here means it also has to have real state published via
// SetTimetableCollection/a stopsByOperator entry — this gate only controls
// which agencies are allowed to reach the handler, not what the handler does.
func agencyGatedHandler(supportedIDs []string, next http.HandlerFunc) http.HandlerFunc {
	supported := make(map[string]bool, len(supportedIDs))
	for _, id := range supportedIDs {
		supported[strings.ToUpper(id)] = true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		operator := strings.ToUpper(r.PathValue("operator"))
		if supported[operator] {
			next(w, r)
			return
		}

		if name, known := AgencyName(operator); known {
			writeAgencyRouteError(w, http.StatusNotFound, fmt.Sprintf(
				"%s (%s) is a known 511 agency, but this endpoint doesn't serve its data today", name, operator))
			return
		}
		writeAgencyRouteError(w, http.StatusNotFound, fmt.Sprintf("unknown agency %q", operator))
	}
}

// pathParamToQuery copies the {operator} path value into the named query
// parameter before calling through, so a handler that already reads its agency
// from a query parameter (servicealerts' "agency", scheduletype's
// "operator_id") needs no changes at all to be reachable via the new
// /agency/{operator}/... routes. Any existing value for that query parameter
// is overwritten, so the path segment always wins.
func pathParamToQuery(queryParam string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operator := r.PathValue("operator")
		q := r.URL.Query()
		q.Set(queryParam, operator)
		r.URL.RawQuery = q.Encode()
		next(w, r)
	}
}
