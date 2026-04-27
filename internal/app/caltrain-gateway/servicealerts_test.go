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
		t.Fatal("Expected entities in real API response")
	}
	first := sa.Entity[0]
	if first.ID == "" {
		t.Error("Expected non-empty entity ID")
	}
	if len(first.Alert.InformedEntity) == 0 {
		t.Error("Expected at least one InformedEntity on the first alert")
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
		"Header": {
			"GtfsRealtimeVersion": "1.0",
			"incrementality": 0,
			"Timestamp": 1773636456
		},
		"Entities": [
			{
				"Id": "alert-2001",
				"Alert": {
					"ActivePeriods": [
						{"Start": 1710000000, "End": 1710100000}
					],
					"InformedEntities": [
						{"AgencyId": "CT", "StopId": "70261"}
					],
					"cause": 1,
					"effect": 6,
					"HeaderText": {
						"Translations": [
							{"Text": "Weekend track work", "Language": "en"}
						]
					},
					"severity_level": 3
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

func TestPickEnglish(t *testing.T) {
	tests := []struct {
		name string
		in   *TranslatedString
		want string
	}{
		{"nil pointer", nil, ""},
		{"no translations", &TranslatedString{}, ""},
		{
			name: "english only",
			in: &TranslatedString{Translation: []Translation{
				{Text: "hello", Language: "en"},
			}},
			want: "hello",
		},
		{
			name: "multiple with english",
			in: &TranslatedString{Translation: []Translation{
				{Text: "hola", Language: "es"},
				{Text: "hello", Language: "en"},
				{Text: "bonjour", Language: "fr"},
			}},
			want: "hello",
		},
		{
			name: "multiple without english falls back to first",
			in: &TranslatedString{Translation: []Translation{
				{Text: "hola", Language: "es"},
				{Text: "bonjour", Language: "fr"},
			}},
			want: "hola",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickEnglish(tc.in)
			if got != tc.want {
				t.Errorf("pickEnglish(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestContentHash(t *testing.T) {
	a := contentHash("Header A", "Description A")
	b := contentHash("Header A", "Description A")
	if a != b {
		t.Errorf("identical input produced different hashes: %s vs %s", a, b)
	}

	c := contentHash("Header B", "Description A")
	if a == c {
		t.Errorf("different headers produced same hash: %s", a)
	}

	d := contentHash("Header A", "Description B")
	if a == d {
		t.Errorf("different descriptions produced same hash: %s", a)
	}

	// The unit-separator must prevent boundary collisions: "AB" + "" must not
	// hash the same as "A" + "B".
	if contentHash("AB", "") == contentHash("A", "B") {
		t.Errorf("boundary collision: header/description join is not unambiguous")
	}
}
