package caltraingateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Agency is a transit operator known to 511, as returned by
// GET transit/gtfsoperators (e.g. {"Id": "CT", "Name": "Caltrain"}).
type Agency struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// LoadAgenciesFromFile reads and parses an agencies JSON file.
func LoadAgenciesFromFile(filename string) ([]Agency, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read agencies file: %w", err)
	}
	return parseAgenciesJSON(data)
}

// LoadAgenciesFromURL fetches and parses agencies JSON from a URL.
func LoadAgenciesFromURL(url string) ([]Agency, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch agencies from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return parseAgenciesJSON(data)
}

// parseAgenciesJSON parses the JSON data into a slice of Agency.
func parseAgenciesJSON(data []byte) ([]Agency, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var agencies []Agency
	if err := json.Unmarshal(data, &agencies); err != nil {
		return nil, fmt.Errorf("failed to parse agencies JSON: %w", err)
	}
	return agencies, nil
}

// agencyDirectory holds every agency 511 knows about. It exists only to make
// route-gating error messages and the admin UI's agency picker accurate; it is
// not required for the currently-supported "CT" agency to keep working, so a
// failed or not-yet-completed load never breaks Caltrain-only functionality.
type agencyDirectory struct {
	mu       sync.RWMutex
	agencies []Agency
	byID     map[string]Agency
}

var agencies = &agencyDirectory{}

// SetAgencies publishes the known-agency directory, replacing any previous one.
func SetAgencies(list []Agency) {
	byID := make(map[string]Agency, len(list))
	for _, a := range list {
		byID[strings.ToUpper(a.ID)] = a
	}

	agencies.mu.Lock()
	defer agencies.mu.Unlock()
	agencies.agencies = list
	agencies.byID = byID
}

// AllAgencies returns every known agency, or nil if the directory has not
// loaded yet.
func AllAgencies() []Agency {
	agencies.mu.RLock()
	defer agencies.mu.RUnlock()
	return agencies.agencies
}

// AgencyName returns the display name for a known agency ID (case-insensitive),
// and false when the directory has not loaded or does not contain that ID.
func AgencyName(id string) (string, bool) {
	agencies.mu.RLock()
	defer agencies.mu.RUnlock()
	agency, ok := agencies.byID[strings.ToUpper(id)]
	return agency.Name, ok
}

// IsKnownAgency reports whether id (case-insensitive) is in the directory.
func IsKnownAgency(id string) bool {
	_, ok := AgencyName(id)
	return ok
}

// loadedAgencyIDs are the agencies with real, locally-loaded timetable/stops
// data (schedule_state.go's scheduleFor, stops.go's stopsByOperator). This is
// the single source of truth both route gating (http.go's agencyGatedHandler
// wiring) and SupportedAgencies use, so the two can't drift out of sync.
var loadedAgencyIDs = []string{departureOperatorID, bartOperatorID}

// loadedAgencyFallbackNames covers SupportedAgencies before the agency
// directory (511's transit/gtfsoperators) has loaded, so the UI never shows a
// blank name for an agency this service actually serves data for.
var loadedAgencyFallbackNames = map[string]string{
	departureOperatorID: "Caltrain",
	bartOperatorID:      "BART",
}

// SupportedAgencies returns the agencies with real, locally-loaded timetable
// and stops data, in a stable order. Display names come from the agency
// directory when it has loaded, falling back to a short hardcoded label
// otherwise.
func SupportedAgencies() []Agency {
	result := make([]Agency, 0, len(loadedAgencyIDs))
	for _, id := range loadedAgencyIDs {
		name, ok := AgencyName(id)
		if !ok {
			name = loadedAgencyFallbackNames[id]
		}
		result = append(result, Agency{ID: id, Name: name})
	}
	return result
}
