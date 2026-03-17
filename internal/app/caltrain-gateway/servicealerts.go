package caltraingateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// ServiceAlertsResponse represents the root GTFS-RT service alerts response
type ServiceAlertsResponse struct {
	Header ServiceAlertsHeader `json:"header"`
	Entity []ServiceAlertEntity `json:"entity"`
}

// ServiceAlertsHeader contains metadata about the feed
type ServiceAlertsHeader struct {
	GtfsRealtimeVersion string `json:"gtfsRealtimeVersion"`
	Incrementality      int    `json:"incrementality"`
	Timestamp           int64  `json:"timestamp"`
}

// ServiceAlertEntity wraps a single alert with its ID
type ServiceAlertEntity struct {
	ID    string       `json:"id"`
	Alert ServiceAlert `json:"alert"`
}

// ServiceAlert represents the alert details
type ServiceAlert struct {
	ActivePeriod    []TimeRange        `json:"activePeriod"`
	InformedEntity  []EntitySelector   `json:"informedEntity"`
	Cause           int                `json:"cause"`
	Effect          int                `json:"effect"`
	HeaderText      *TranslatedString  `json:"headerText,omitempty"`
	DescriptionText *TranslatedString  `json:"descriptionText,omitempty"`
	URL             *TranslatedString  `json:"url,omitempty"`
	SeverityLevel   int                `json:"severityLevel,omitempty"`
}

// TimeRange represents an active period with start and end timestamps
type TimeRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end,omitempty"`
}

// EntitySelector identifies the transit entity affected by an alert
type EntitySelector struct {
	AgencyID    string `json:"agencyId,omitempty"`
	RouteID     string `json:"routeId,omitempty"`
	RouteType   int    `json:"routeType,omitempty"`
	StopID      string `json:"stopId,omitempty"`
	DirectionID int    `json:"directionId,omitempty"`
}

// TranslatedString holds translations for a text field
type TranslatedString struct {
	Translation []Translation `json:"translation"`
}

// Translation is a single translated text
type Translation struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

// LoadServiceAlerts reads and parses a service alerts JSON file
func LoadServiceAlerts(filename string) (*ServiceAlertsResponse, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read service alerts file: %w", err)
	}

	return parseServiceAlertsJSON(data)
}

// LoadServiceAlertsFromURL fetches and parses service alerts JSON from a URL
func LoadServiceAlertsFromURL(url string) (*ServiceAlertsResponse, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch service alerts from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return parseServiceAlertsJSON(data)
}

// parseServiceAlertsJSON parses the JSON data into a ServiceAlertsResponse
func parseServiceAlertsJSON(data []byte) (*ServiceAlertsResponse, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var alerts ServiceAlertsResponse
	if err := json.Unmarshal(data, &alerts); err != nil {
		return nil, fmt.Errorf("failed to parse service alerts JSON: %w", err)
	}
	return &alerts, nil
}
