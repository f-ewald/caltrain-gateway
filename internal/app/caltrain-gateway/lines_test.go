package caltraingateway_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	caltraingateway "caltrain-gateway/internal/app/caltrain-gateway"
)

func TestLoadLinesFromFile(t *testing.T) {
	lines, err := caltraingateway.LoadLinesFromFile("example_lines.json")
	if err != nil {
		t.Fatalf("failed to load lines: %v", err)
	}

	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	// Verify first line
	if lines[0].ID != "Limited" {
		t.Errorf("expected first line ID 'Limited', got '%s'", lines[0].ID)
	}
	if lines[0].SiriLineRef != "LIM" {
		t.Errorf("expected SiriLineRef 'LIM', got '%s'", lines[0].SiriLineRef)
	}
	if lines[0].TransportMode != "rail" {
		t.Errorf("expected TransportMode 'rail', got '%s'", lines[0].TransportMode)
	}
	if !lines[0].Monitored {
		t.Error("expected Monitored to be true")
	}
	if lines[0].OperatorRef != "CT" {
		t.Errorf("expected OperatorRef 'CT', got '%s'", lines[0].OperatorRef)
	}

	// Verify South County line has a name
	if lines[1].ID != "South County" {
		t.Errorf("expected second line ID 'South County', got '%s'", lines[1].ID)
	}
	if lines[1].Name != "South Santa Clara County Connector" {
		t.Errorf("expected Name 'South Santa Clara County Connector', got '%s'", lines[1].Name)
	}
}

func TestLoadLinesFromFile_FileNotFound(t *testing.T) {
	_, err := caltraingateway.LoadLinesFromFile("nonexistent.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadLinesFromURL(t *testing.T) {
	// Create a mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[
			{
				"Id": "TestLine",
				"Name": "Test Line Name",
				"FromDate": "2026-01-01T00:00:00-08:00",
				"ToDate": "2026-12-31T23:59:00-08:00",
				"TransportMode": "rail",
				"PublicCode": "TEST",
				"SiriLineRef": "TST",
				"Monitored": true,
				"OperatorRef": "CT"
			}
		]`))
	}))
	defer mockServer.Close()

	lines, err := caltraingateway.LoadLinesFromURL(mockServer.URL)
	if err != nil {
		t.Fatalf("failed to load lines from URL: %v", err)
	}

	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}

	if lines[0].ID != "TestLine" {
		t.Errorf("expected line ID 'TestLine', got '%s'", lines[0].ID)
	}
}

func TestLoadLinesFromURL_Error(t *testing.T) {
	// Create a mock server that returns an error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	_, err := caltraingateway.LoadLinesFromURL(mockServer.URL)
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestGetLineIDs(t *testing.T) {
	lines, err := caltraingateway.LoadLinesFromFile("example_lines.json")
	if err != nil {
		t.Fatalf("failed to load lines: %v", err)
	}

	ids := caltraingateway.GetLineIDs(lines)

	expectedIDs := []string{"Limited", "South County", "Local Weekday", "Local Weekend", "Express"}
	if len(ids) != len(expectedIDs) {
		t.Errorf("expected %d IDs, got %d", len(expectedIDs), len(ids))
	}

	for i, expected := range expectedIDs {
		if ids[i] != expected {
			t.Errorf("expected ID[%d] = '%s', got '%s'", i, expected, ids[i])
		}
	}
}

func TestGetMonitoredLines(t *testing.T) {
	lines, err := caltraingateway.LoadLinesFromFile("example_lines.json")
	if err != nil {
		t.Fatalf("failed to load lines: %v", err)
	}

	monitored := caltraingateway.GetMonitoredLines(lines)

	// All lines in the example are monitored
	if len(monitored) != 5 {
		t.Errorf("expected 5 monitored lines, got %d", len(monitored))
	}
}
