package caltraingateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const exampleAgenciesJSON = `[
	{"Id": "CT", "Name": "Caltrain", "LastGenerated": "6/11/2026 2:08:38 AM"},
	{"Id": "BA", "Name": "Bay Area Rapid Transit", "LastGenerated": "8/9/2026 9:18:37 PM"}
]`

func TestParseAgenciesJSON(t *testing.T) {
	agencies, err := parseAgenciesJSON([]byte(exampleAgenciesJSON))
	if err != nil {
		t.Fatalf("failed to parse agencies JSON: %v", err)
	}
	if len(agencies) != 2 {
		t.Fatalf("expected 2 agencies, got %d", len(agencies))
	}
	if agencies[0].ID != "CT" || agencies[0].Name != "Caltrain" {
		t.Errorf("unexpected first agency: %+v", agencies[0])
	}
}

func TestParseAgenciesJSON_WithBOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(exampleAgenciesJSON)...)
	agencies, err := parseAgenciesJSON(data)
	if err != nil {
		t.Fatalf("failed to parse agencies JSON with BOM: %v", err)
	}
	if len(agencies) != 2 {
		t.Errorf("expected 2 agencies, got %d", len(agencies))
	}
}

func TestParseAgenciesJSON_Invalid(t *testing.T) {
	if _, err := parseAgenciesJSON([]byte("not json")); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestLoadAgenciesFromURL(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleAgenciesJSON))
	}))
	defer mockServer.Close()

	agencies, err := LoadAgenciesFromURL(mockServer.URL)
	if err != nil {
		t.Fatalf("failed to load agencies from URL: %v", err)
	}
	if len(agencies) != 2 {
		t.Errorf("expected 2 agencies, got %d", len(agencies))
	}
}

func TestLoadAgenciesFromURL_ServerError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	if _, err := LoadAgenciesFromURL(mockServer.URL); err == nil {
		t.Error("expected an error for a server error response")
	}
}

func TestLoadAgenciesFromFile_NotFound(t *testing.T) {
	if _, err := LoadAgenciesFromFile("nonexistent.json"); err == nil {
		t.Error("expected an error for a nonexistent file")
	}
}

func TestAgencyDirectory_SetAllGetIsKnown(t *testing.T) {
	SetAgencies([]Agency{
		{ID: "CT", Name: "Caltrain"},
		{ID: "BA", Name: "Bay Area Rapid Transit"},
	})
	t.Cleanup(func() { SetAgencies(nil) })

	all := AllAgencies()
	if len(all) != 2 {
		t.Fatalf("expected 2 agencies, got %d", len(all))
	}

	name, ok := AgencyName("ba")
	if !ok || name != "Bay Area Rapid Transit" {
		t.Errorf("expected case-insensitive lookup to find BART, got (%q, %v)", name, ok)
	}
	if !IsKnownAgency("CT") {
		t.Error("expected CT to be known")
	}
	if IsKnownAgency("ZZ") {
		t.Error("expected ZZ to be unknown")
	}
}

func TestAgencyDirectory_EmptyByDefault(t *testing.T) {
	SetAgencies(nil)

	if all := AllAgencies(); all != nil {
		t.Errorf("expected nil directory, got %v", all)
	}
	if IsKnownAgency("CT") {
		t.Error("expected nothing to be known when the directory is empty")
	}
	if _, ok := AgencyName("CT"); ok {
		t.Error("expected AgencyName to report unknown when the directory is empty")
	}
}

func TestSupportedAgencies_FallsBackWhenDirectoryEmpty(t *testing.T) {
	SetAgencies(nil)

	agencies := SupportedAgencies()
	if len(agencies) != 2 {
		t.Fatalf("expected 2 supported agencies, got %d", len(agencies))
	}
	byID := map[string]string{}
	for _, a := range agencies {
		byID[a.ID] = a.Name
	}
	if byID["CT"] != "Caltrain" {
		t.Errorf("expected fallback name 'Caltrain' for CT, got %q", byID["CT"])
	}
	if byID["BA"] != "BART" {
		t.Errorf("expected fallback name 'BART' for BA, got %q", byID["BA"])
	}
}

func TestSupportedAgencies_UsesDirectoryNamesWhenLoaded(t *testing.T) {
	SetAgencies([]Agency{
		{ID: "CT", Name: "Caltrain"},
		{ID: "BA", Name: "Bay Area Rapid Transit"},
	})
	t.Cleanup(func() { SetAgencies(nil) })

	agencies := SupportedAgencies()
	byID := map[string]string{}
	for _, a := range agencies {
		byID[a.ID] = a.Name
	}
	if byID["BA"] != "Bay Area Rapid Transit" {
		t.Errorf("expected the directory's full name for BA, got %q", byID["BA"])
	}
}
