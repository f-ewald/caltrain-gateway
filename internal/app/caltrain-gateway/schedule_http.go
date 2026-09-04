package caltraingateway

import (
	"encoding/json"
	"net/http"
	"time"
)

// ScheduleVersionResponse is the JSON payload of the schedule version endpoint.
// It answers both "has my copy changed?" and "when does my copy lapse?", the
// latter letting a client prefetch ahead of a timetable cutover.
type ScheduleVersionResponse struct {
	Version       string   `json:"version"`
	ValidFrom     string   `json:"valid_from,omitempty"`
	ValidTo       string   `json:"valid_to,omitempty"`
	ExpiresInDays *int     `json:"expires_in_days,omitempty"`
	Expired       bool     `json:"expired"`
	FrameIDs      []string `json:"frame_ids"`
	LineCount     int      `json:"line_count"`
	RefreshedAt   string   `json:"refreshed_at,omitempty"`
	Stale         bool     `json:"stale"`
}

// staleAfter is how long a schedule may go without a successful refresh before
// it is reported as stale. The refresh runs nightly, so a day and a half allows
// one failed run before clients are warned.
const staleAfter = 36 * time.Hour

// buildScheduleVersionResponse renders the current schedule state for
// operatorID as of now.
func buildScheduleVersionResponse(operatorID string, now time.Time) (ScheduleVersionResponse, bool) {
	state := scheduleFor(operatorID)
	if state == nil {
		return ScheduleVersionResponse{}, false
	}
	collection, version, metadata, refreshedAt, _ := state.Snapshot()
	if collection == nil {
		return ScheduleVersionResponse{}, false
	}

	response := ScheduleVersionResponse{
		Version:     version,
		ValidFrom:   formatValidityDate(metadata.ValidFrom),
		ValidTo:     formatValidityDate(metadata.ValidTo),
		Expired:     metadata.Expired(now),
		FrameIDs:    metadata.FrameIDs,
		LineCount:   metadata.LineCount,
		Stale:       refreshedAt.IsZero() || now.Sub(refreshedAt) > staleAfter,
		RefreshedAt: formatTimestamp(refreshedAt),
	}
	if days, ok := metadata.ExpiresInDays(now); ok {
		response.ExpiresInDays = &days
	}
	if response.FrameIDs == nil {
		response.FrameIDs = []string{}
	}
	return response, true
}

// formatTimestamp renders a time as RFC3339 in UTC, or "" when unset.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// scheduleVersionHandler serves a small document describing the loaded
// schedule, so clients can revalidate a cached timetable without downloading it.
func scheduleVersionHandler(w http.ResponseWriter, r *http.Request) {
	response, ok := buildScheduleVersionResponse(resolveOperator(r), time.Now())
	if !ok {
		http.Error(w, "Timetable not loaded", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}
