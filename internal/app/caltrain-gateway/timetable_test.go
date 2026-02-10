package caltraingateway_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	caltraingateway "caltrain-gateway/internal/app/caltrain-gateway"
)

func TestLoadTimetable(t *testing.T) {
	timetable, err := caltraingateway.LoadTimetable("example_timetable.json")
	if err != nil {
		t.Fatalf("failed to load timetable: %v", err)
	}

	// Verify ServiceFrame
	if timetable.Content.ServiceFrame.ID != "SF" {
		t.Errorf("expected ServiceFrame ID 'SF', got '%s'", timetable.Content.ServiceFrame.ID)
	}

	// Verify routes exist
	routes := timetable.Content.ServiceFrame.Routes.Route
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}

	// Verify first route
	if routes[0].ID != "3206643" {
		t.Errorf("expected first route ID '3206643', got '%s'", routes[0].ID)
	}
	if routes[0].LineRef.Ref != "Limited" {
		t.Errorf("expected LineRef 'Limited', got '%s'", routes[0].LineRef.Ref)
	}

	// Verify ServiceCalendarFrame
	dayTypes := timetable.Content.ServiceCalendarFrame.DayTypes.DayType
	if len(dayTypes) != 1 {
		t.Errorf("expected 1 day type, got %d", len(dayTypes))
	}
	if dayTypes[0].ID != "69802" {
		t.Errorf("expected day type ID '69802', got '%s'", dayTypes[0].ID)
	}

	// Verify TimetableFrames
	frames := timetable.Content.TimetableFrame
	if len(frames) != 2 {
		t.Errorf("expected 2 timetable frames, got %d", len(frames))
	}

	// Verify first frame has journeys
	journeys := frames[0].VehicleJourneys.ServiceJourney
	if len(journeys) == 0 {
		t.Error("expected service journeys in first frame")
	}

	// Verify first journey has calls
	if len(journeys[0].Calls.Call) == 0 {
		t.Error("expected calls in first journey")
	}

	// Verify first call has expected data
	firstCall := journeys[0].Calls.Call[0]
	if firstCall.Order != "1" {
		t.Errorf("expected first call order '1', got '%s'", firstCall.Order)
	}
	if firstCall.Arrival.Time == "" {
		t.Error("expected arrival time to be set")
	}
}

func TestLoadTimetable_FileNotFound(t *testing.T) {
	_, err := caltraingateway.LoadTimetable("nonexistent.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadTimetableFromURL(t *testing.T) {
	// Read the example timetable file to serve as mock response
	data, err := os.ReadFile("example_timetable.json")
	if err != nil {
		t.Fatalf("failed to read example timetable: %v", err)
	}

	// Create a mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer mockServer.Close()

	tt, err := caltraingateway.LoadTimetableFromURL(mockServer.URL)
	if err != nil {
		t.Fatalf("failed to load timetable from URL: %v", err)
	}

	// Verify basic structure
	if tt.Content.ServiceFrame.ID != "SF" {
		t.Errorf("expected ServiceFrame ID 'SF', got '%s'", tt.Content.ServiceFrame.ID)
	}
}

func TestLoadTimetableFromURL_Error(t *testing.T) {
	// Create a mock server that returns an error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	_, err := caltraingateway.LoadTimetableFromURL(mockServer.URL)
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestGetDeparturesByStop(t *testing.T) {
	timetable, err := caltraingateway.LoadTimetable("example_timetable.json")
	if err != nil {
		t.Fatalf("failed to load timetable: %v", err)
	}

	departures := timetable.GetDeparturesByStop()

	// Should have departures for multiple stops
	if len(departures) == 0 {
		t.Fatal("expected departures map to be non-empty")
	}

	// Check a specific stop (70261 is the first stop in the example)
	stop70261 := departures["70261"]
	if len(stop70261) == 0 {
		t.Error("expected departures for stop 70261")
	}

	// Verify the first departure has expected fields
	found := false
	for _, dep := range stop70261 {
		if dep.TrainID == "401" {
			found = true
			if dep.Line != "Limited" {
				t.Errorf("expected line 'Limited', got '%s'", dep.Line)
			}
			if dep.Direction != "N " {
				t.Errorf("expected direction 'N ', got '%s'", dep.Direction)
			}
			if dep.DepartureTime != "05:43:00" {
				t.Errorf("expected departure time '05:43:00', got '%s'", dep.DepartureTime)
			}
			if dep.Destination != "San Francisco" {
				t.Errorf("expected destination 'San Francisco', got '%s'", dep.Destination)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find train 401 at stop 70261")
	}

	// Verify southbound trains exist
	stop70012 := departures["70012"]
	if len(stop70012) == 0 {
		t.Error("expected departures for stop 70012 (southbound)")
	}
}

func TestGetDeparturesByStopAndWeekday(t *testing.T) {
	timetable, err := caltraingateway.LoadTimetable("example_timetable.json")
	if err != nil {
		t.Fatalf("failed to load timetable: %v", err)
	}

	t.Run("weekday filter Monday", func(t *testing.T) {
		departures := timetable.GetDeparturesByStopAndWeekday(caltraingateway.Monday)
		if len(departures) == 0 {
			t.Error("expected departures for Monday")
		}
		// Should have stop 70261
		if len(departures["70261"]) == 0 {
			t.Error("expected departures for stop 70261 on Monday")
		}
	})

	t.Run("weekday filter Saturday", func(t *testing.T) {
		departures := timetable.GetDeparturesByStopAndWeekday(caltraingateway.Saturday)
		// Example timetable only has weekday schedule, so Saturday should be empty
		if len(departures) != 0 {
			t.Error("expected no departures for Saturday (weekday-only schedule)")
		}
	})

	t.Run("empty weekday returns all", func(t *testing.T) {
		allDepartures := timetable.GetDeparturesByStop()
		filteredDepartures := timetable.GetDeparturesByStopAndWeekday("")
		if len(allDepartures) != len(filteredDepartures) {
			t.Errorf("expected same count: all=%d, filtered=%d", len(allDepartures), len(filteredDepartures))
		}
	})
}

func TestTimetableCollection(t *testing.T) {
	t.Run("load multiple files", func(t *testing.T) {
		tc := caltraingateway.NewTimetableCollection()
		err := tc.LoadTimetableFiles("example_timetable.json")
		if err != nil {
			t.Fatalf("failed to load timetable files: %v", err)
		}

		departures := tc.GetDeparturesByStop()
		if len(departures) == 0 {
			t.Error("expected departures from collection")
		}
	})

	t.Run("add timetable manually", func(t *testing.T) {
		tc := caltraingateway.NewTimetableCollection()
		tt, err := caltraingateway.LoadTimetable("example_timetable.json")
		if err != nil {
			t.Fatalf("failed to load timetable: %v", err)
		}
		tc.AddTimetable(tt)

		departures := tc.GetDeparturesByStop()
		if len(departures) == 0 {
			t.Error("expected departures from collection")
		}
	})

	t.Run("filter by weekday", func(t *testing.T) {
		tc := caltraingateway.NewTimetableCollection()
		tc.LoadTimetableFiles("example_timetable.json")

		mondayDepartures := tc.GetDeparturesByStopAndWeekday(caltraingateway.Monday)
		if len(mondayDepartures) == 0 {
			t.Error("expected Monday departures")
		}

		saturdayDepartures := tc.GetDeparturesByStopAndWeekday(caltraingateway.Saturday)
		if len(saturdayDepartures) != 0 {
			t.Error("expected no Saturday departures for weekday-only schedule")
		}
	})
}

func TestParseWeekday(t *testing.T) {
	tests := []struct {
		input    string
		expected caltraingateway.Weekday
	}{
		{"Monday", caltraingateway.Monday},
		{"monday", caltraingateway.Monday},
		{"Tuesday", caltraingateway.Tuesday},
		{"Wednesday", caltraingateway.Wednesday},
		{"Thursday", caltraingateway.Thursday},
		{"Friday", caltraingateway.Friday},
		{"Saturday", caltraingateway.Saturday},
		{"Sunday", caltraingateway.Sunday},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := caltraingateway.ParseWeekday(tt.input)
			if result != tt.expected {
				t.Errorf("ParseWeekday(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
