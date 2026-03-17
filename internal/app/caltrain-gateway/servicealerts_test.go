package caltraingateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLoadServiceAlerts(t *testing.T) {
	sa, err := LoadServiceAlerts("example_servicealerts.json")
	if err != nil {
		t.Fatalf("failed to load service alerts: %v", err)
	}

	if sa.Header.GtfsRealtimeVersion != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", sa.Header.GtfsRealtimeVersion)
	}

	if sa.Header.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	if len(sa.Entity) == 0 {
		t.Error("Expected entities in real API response")
	}
}

func TestLoadServiceAlertsFromURL(t *testing.T) {
	data, err := os.ReadFile("example_servicealerts.json")
	if err != nil {
		t.Fatalf("failed to read example file: %v", err)
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer mockServer.Close()

	sa, err := LoadServiceAlertsFromURL(mockServer.URL)
	if err != nil {
		t.Fatalf("failed to load service alerts from URL: %v", err)
	}

	if sa.Header.GtfsRealtimeVersion != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", sa.Header.GtfsRealtimeVersion)
	}

	if sa.Header.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	if len(sa.Entity) == 0 {
		t.Error("Expected entities in real API response")
	}
}

func TestLoadServiceAlertsFromURL_Error(t *testing.T) {
	// Test with server returning error status
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	_, err := LoadServiceAlertsFromURL(mockServer.URL)
	if err == nil {
		t.Error("Expected error for non-200 status code")
	}

	// Test with unreachable server
	_, err = LoadServiceAlertsFromURL("http://localhost:1")
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

func TestParseServiceAlertsWithEntities(t *testing.T) {
	jsonData := `{
		"header": {
			"gtfsRealtimeVersion": "1.0",
			"incrementality": 0,
			"timestamp": 1773636456
		},
		"entity": [
			{
				"id": "alert-2001",
				"alert": {
					"activePeriod": [
						{"start": 1710000000, "end": 1710100000}
					],
					"informedEntity": [
						{"agencyId": "CT", "stopId": "70261"}
					],
					"cause": 1,
					"effect": 6,
					"headerText": {
						"translation": [
							{"text": "Weekend track work", "language": "en"}
						]
					},
					"severityLevel": 3
				}
			}
		]
	}`

	sa, err := parseServiceAlertsJSON([]byte(jsonData))
	if err != nil {
		t.Fatalf("failed to parse service alerts: %v", err)
	}

	if sa.Header.GtfsRealtimeVersion != "1.0" {
		t.Errorf("Expected version '1.0', got '%s'", sa.Header.GtfsRealtimeVersion)
	}
	if sa.Header.Timestamp != 1773636456 {
		t.Errorf("Expected timestamp 1773636456, got %d", sa.Header.Timestamp)
	}

	if len(sa.Entity) != 1 {
		t.Fatalf("Expected 1 entity, got %d", len(sa.Entity))
	}

	alert := sa.Entity[0]
	if alert.ID != "alert-2001" {
		t.Errorf("Expected ID 'alert-2001', got '%s'", alert.ID)
	}
	if alert.Alert.Cause != 1 {
		t.Errorf("Expected cause 1, got %d", alert.Alert.Cause)
	}
	if alert.Alert.Effect != 6 {
		t.Errorf("Expected effect 6, got %d", alert.Alert.Effect)
	}
	if len(alert.Alert.ActivePeriod) != 1 {
		t.Fatalf("Expected 1 active period, got %d", len(alert.Alert.ActivePeriod))
	}
	if alert.Alert.ActivePeriod[0].Start != 1710000000 {
		t.Errorf("Expected start 1710000000, got %d", alert.Alert.ActivePeriod[0].Start)
	}
	if alert.Alert.ActivePeriod[0].End != 1710100000 {
		t.Errorf("Expected end 1710100000, got %d", alert.Alert.ActivePeriod[0].End)
	}
	if len(alert.Alert.InformedEntity) != 1 {
		t.Fatalf("Expected 1 informed entity, got %d", len(alert.Alert.InformedEntity))
	}
	if alert.Alert.InformedEntity[0].StopID != "70261" {
		t.Errorf("Expected stopId '70261', got '%s'", alert.Alert.InformedEntity[0].StopID)
	}
	if alert.Alert.HeaderText == nil || len(alert.Alert.HeaderText.Translation) == 0 {
		t.Fatal("Expected header text with translations")
	}
	if alert.Alert.HeaderText.Translation[0].Text != "Weekend track work" {
		t.Errorf("Unexpected header text: '%s'", alert.Alert.HeaderText.Translation[0].Text)
	}
	if alert.Alert.SeverityLevel != 3 {
		t.Errorf("Expected severity 3, got %d", alert.Alert.SeverityLevel)
	}
}
