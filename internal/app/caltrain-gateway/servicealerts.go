package caltraingateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// ServiceAlertsResponse represents the root GTFS-RT service alerts response
type ServiceAlertsResponse struct {
	Header ServiceAlertsHeader `json:"header"`
	Entity []ServiceAlertEntity `json:"Entities"`
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
	ActivePeriod    []TimeRange        `json:"ActivePeriods"`
	InformedEntity  []EntitySelector   `json:"InformedEntities"`
	Cause           int                `json:"cause"`
	Effect          int                `json:"effect"`
	HeaderText      *TranslatedString  `json:"headerText,omitempty"`
	DescriptionText *TranslatedString  `json:"descriptionText,omitempty"`
	URL             *TranslatedString  `json:"url,omitempty"`
	SeverityLevel   int                `json:"severity_level,omitempty"`
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
	Translation []Translation `json:"Translations"`
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

// LoadServiceAlertsFromURL fetches and parses service alerts JSON from a URL.
// On success it also persists each entity to the database via persistServiceAlerts;
// the persistence step is a no-op when no database is configured.
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

	parsed, err := parseServiceAlertsJSON(data)
	if err != nil {
		return nil, err
	}

	persistServiceAlerts(parsed)
	return parsed, nil
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

// pickEnglish returns the English translation of ts when present; otherwise it
// falls back to the first available translation, or "" when ts has no usable text.
func pickEnglish(ts *TranslatedString) string {
	if ts == nil || len(ts.Translation) == 0 {
		return ""
	}
	for _, t := range ts.Translation {
		if t.Language == "en" {
			return t.Text
		}
	}
	return ts.Translation[0].Text
}

// contentHash returns a hex-encoded SHA-256 of header and description joined by
// the unit-separator byte (0x1F) to avoid boundary collisions.
func contentHash(header, description string) string {
	h := sha256.New()
	h.Write([]byte(header))
	h.Write([]byte{0x1F})
	h.Write([]byte(description))
	return hex.EncodeToString(h.Sum(nil))
}

// persistServiceAlerts upserts each entity in resp to the database. Each unique
// (entity_id, content_hash) pair is stored once; identical refreshes only bump
// last_seen_at. Errors on individual entities are logged and skipped so a single
// bad row does not abort the batch. No-op when resp is nil or DB is not configured.
func persistServiceAlerts(resp *ServiceAlertsResponse) {
	if resp == nil {
		return
	}
	if DB == nil {
		log.Printf("skipping service alerts persistence: no database configured (%d entities in response)", len(resp.Entity))
		return
	}
	if len(resp.Entity) == 0 {
		log.Println("no service alert entities to persist")
		return
	}
	stored, failed, skipped := 0, 0, 0
	for _, entity := range resp.Entity {
		if entity.ID == "" {
			skipped++
			continue
		}
		header := pickEnglish(entity.Alert.HeaderText)
		description := pickEnglish(entity.Alert.DescriptionText)
		urlText := pickEnglish(entity.Alert.URL)

		agencyID := ""
		if len(entity.Alert.InformedEntity) > 0 {
			agencyID = entity.Alert.InformedEntity[0].AgencyID
		}

		hash := contentHash(header, description)
		if err := UpsertServiceAlert(
			entity.ID, hash, agencyID,
			entity.Alert.Cause, entity.Alert.Effect, entity.Alert.SeverityLevel,
			header, description, urlText,
			resp.Header.Timestamp,
		); err != nil {
			log.Printf("failed to persist service alert %q: %v", entity.ID, err)
			failed++
			continue
		}
		stored++
	}
	log.Printf("service alerts persisted: %d stored, %d failed, %d skipped (no id)", stored, failed, skipped)
}
