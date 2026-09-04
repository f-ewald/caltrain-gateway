package caltraingateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const exampleHolidaysJSON = `{
	"Content": {
		"ServiceCalendar": {
			"id": "CT",
			"FromDate": "2026-01-31",
			"ToDate": "2026-08-31"
		},
		"AvailabilityConditions": [
			{
				"version": "any",
				"id": "CT:2026-02-16",
				"FromDate": "2026-02-16T00:00:00-07:00",
				"ToDate": "2026-02-16T23:59:00-07:00"
			},
			{
				"version": "any",
				"id": "CT:2026-05-25",
				"FromDate": "2026-05-25T00:00:00-07:00",
				"ToDate": "2026-05-25T23:59:00-07:00"
			}
		]
	}
}`

func TestParseHolidaysJSON(t *testing.T) {
	holidays, err := parseHolidaysJSON([]byte(exampleHolidaysJSON))
	if err != nil {
		t.Fatalf("failed to parse holidays JSON: %v", err)
	}

	if holidays.Content.ServiceCalendar.ID != "CT" {
		t.Errorf("Expected operator ID 'CT', got '%s'", holidays.Content.ServiceCalendar.ID)
	}
	if holidays.Content.ServiceCalendar.FromDate != "2026-01-31" {
		t.Errorf("Expected FromDate '2026-01-31', got '%s'", holidays.Content.ServiceCalendar.FromDate)
	}
	if holidays.Content.ServiceCalendar.ToDate != "2026-08-31" {
		t.Errorf("Expected ToDate '2026-08-31', got '%s'", holidays.Content.ServiceCalendar.ToDate)
	}
	if len(holidays.Content.AvailabilityConditions) != 2 {
		t.Fatalf("Expected 2 availability conditions, got %d", len(holidays.Content.AvailabilityConditions))
	}
	if holidays.Content.AvailabilityConditions[0].ID != "CT:2026-02-16" {
		t.Errorf("Expected first condition ID 'CT:2026-02-16', got '%s'", holidays.Content.AvailabilityConditions[0].ID)
	}
	if holidays.Content.AvailabilityConditions[1].ID != "CT:2026-05-25" {
		t.Errorf("Expected second condition ID 'CT:2026-05-25', got '%s'", holidays.Content.AvailabilityConditions[1].ID)
	}
}

func TestParseHolidaysJSON_WithBOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	data := append(bom, []byte(exampleHolidaysJSON)...)

	holidays, err := parseHolidaysJSON(data)
	if err != nil {
		t.Fatalf("failed to parse holidays JSON with BOM: %v", err)
	}

	if holidays.Content.ServiceCalendar.ID != "CT" {
		t.Errorf("Expected operator ID 'CT', got '%s'", holidays.Content.ServiceCalendar.ID)
	}
}

func TestParseHolidaysJSON_InvalidJSON(t *testing.T) {
	_, err := parseHolidaysJSON([]byte("not json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestParseHolidaysJSON_EmptyConditions(t *testing.T) {
	data := `{
		"Content": {
			"ServiceCalendar": {"id": "CT", "FromDate": "2026-01-31", "ToDate": "2026-08-31"},
			"AvailabilityConditions": []
		}
	}`
	holidays, err := parseHolidaysJSON([]byte(data))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if len(holidays.Content.AvailabilityConditions) != 0 {
		t.Errorf("Expected 0 conditions, got %d", len(holidays.Content.AvailabilityConditions))
	}
}

func TestLoadHolidaysFromURL(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleHolidaysJSON))
	}))
	defer mockServer.Close()

	holidays, err := LoadHolidaysFromURL(mockServer.URL)
	if err != nil {
		t.Fatalf("failed to load holidays from URL: %v", err)
	}

	if holidays.Content.ServiceCalendar.ID != "CT" {
		t.Errorf("Expected operator ID 'CT', got '%s'", holidays.Content.ServiceCalendar.ID)
	}
	if len(holidays.Content.AvailabilityConditions) != 2 {
		t.Errorf("Expected 2 conditions, got %d", len(holidays.Content.AvailabilityConditions))
	}
}

func TestLoadHolidaysFromURL_ServerError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	_, err := LoadHolidaysFromURL(mockServer.URL)
	if err == nil {
		t.Error("Expected error for non-200 status code")
	}
}

func TestLoadHolidaysFromURL_Unreachable(t *testing.T) {
	_, err := LoadHolidaysFromURL("http://localhost:1")
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

func TestDetermineScheduleType(t *testing.T) {
	holidays, err := parseHolidaysJSON([]byte(exampleHolidaysJSON))
	if err != nil {
		t.Fatalf("failed to parse holidays: %v", err)
	}

	tests := []struct {
		name     string
		date     string
		expected ScheduleType
	}{
		{
			name:     "holiday date (Presidents Day 2026-02-16)",
			date:     "2026-02-16",
			expected: ScheduleHoliday,
		},
		{
			name:     "holiday date (Memorial Day 2026-05-25)",
			date:     "2026-05-25",
			expected: ScheduleHoliday,
		},
		{
			name:     "regular weekday (Monday)",
			date:     "2026-03-02",
			expected: ScheduleWeekday,
		},
		{
			name:     "regular weekday (Wednesday)",
			date:     "2026-03-04",
			expected: ScheduleWeekday,
		},
		{
			name:     "regular weekday (Friday)",
			date:     "2026-03-06",
			expected: ScheduleWeekday,
		},
		{
			name:     "saturday",
			date:     "2026-03-07",
			expected: ScheduleSaturday,
		},
		{
			name:     "sunday",
			date:     "2026-03-08",
			expected: ScheduleSunday,
		},
		{
			name:     "day before holiday",
			date:     "2026-02-15",
			expected: ScheduleSunday, // 2026-02-15 is a Sunday
		},
		{
			name:     "day after holiday",
			date:     "2026-02-17",
			expected: ScheduleWeekday, // 2026-02-17 is a Tuesday
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, err := time.Parse("2006-01-02", tt.date)
			if err != nil {
				t.Fatalf("failed to parse date: %v", err)
			}

			result := DetermineScheduleType(holidays, date)
			if result != tt.expected {
				t.Errorf("Expected schedule type '%s' for %s, got '%s'", tt.expected, tt.date, result)
			}
		})
	}
}

func TestDetermineScheduleType_NoHolidays(t *testing.T) {
	holidays := &HolidaysResponse{
		Content: HolidaysContent{
			AvailabilityConditions: []HolidayAvailabilityCondition{},
		},
	}

	// A Monday with no holidays should be weekday
	date, _ := time.Parse("2006-01-02", "2026-03-02")
	result := DetermineScheduleType(holidays, date)
	if result != ScheduleWeekday {
		t.Errorf("Expected 'weekday', got '%s'", result)
	}
}

func TestDetermineScheduleType_InvalidHolidayDates(t *testing.T) {
	holidays := &HolidaysResponse{
		Content: HolidaysContent{
			AvailabilityConditions: []HolidayAvailabilityCondition{
				{
					FromDate: "invalid-date",
					ToDate:   "invalid-date",
				},
			},
		},
	}

	// Should fall through to weekday check when holiday dates are unparseable
	date, _ := time.Parse("2006-01-02", "2026-03-02") // Monday
	result := DetermineScheduleType(holidays, date)
	if result != ScheduleWeekday {
		t.Errorf("Expected 'weekday' when holiday dates are invalid, got '%s'", result)
	}
}

func TestScheduleTypeHandler(t *testing.T) {
	// Mock 511 API server
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("operator_id") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleHolidaysJSON))
	}))
	defer mockAPI.Close()

	// Point apiBaseURL to mock server
	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()

	keyPool := NewKeyPool([]string{"test-key"}, 10, 5)

	t.Run("missing date parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/caltrain/scheduletype?operator_id=CT", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("invalid date format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=not-a-date", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("holiday date", func(t *testing.T) {
		Cache.Flush()

		req := httptest.NewRequest("GET", "/caltrain/scheduletype?operator_id=CT&date=2026-02-16", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		var result ScheduleTypeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ScheduleType != ScheduleHoliday {
			t.Errorf("Expected schedule type 'holiday', got '%s'", result.ScheduleType)
		}
		if result.OperatorID != "CT" {
			t.Errorf("Expected operator_id 'CT', got '%s'", result.OperatorID)
		}
		if result.Date != "2026-02-16" {
			t.Errorf("Expected date '2026-02-16', got '%s'", result.Date)
		}
		if result.Overridden {
			t.Error("Expected overridden=false for a holiday-calendar-derived result")
		}
	})

	t.Run("weekday date", func(t *testing.T) {
		Cache.Flush()

		req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=2026-03-02", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		var result ScheduleTypeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ScheduleType != ScheduleWeekday {
			t.Errorf("Expected schedule type 'weekday', got '%s'", result.ScheduleType)
		}
		if result.OperatorID != "CT" {
			t.Errorf("Expected default operator_id 'CT', got '%s'", result.OperatorID)
		}
	})

	t.Run("saturday date", func(t *testing.T) {
		Cache.Flush()

		req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=2026-03-07", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		var result ScheduleTypeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ScheduleType != ScheduleSaturday {
			t.Errorf("Expected schedule type 'saturday', got '%s'", result.ScheduleType)
		}
	})

	t.Run("sunday date", func(t *testing.T) {
		Cache.Flush()

		req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=2026-03-08", nil)
		rec := httptest.NewRecorder()

		handler := scheduleTypeHandler(keyPool)
		handler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		var result ScheduleTypeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ScheduleType != ScheduleSunday {
			t.Errorf("Expected schedule type 'sunday', got '%s'", result.ScheduleType)
		}
	})

	t.Run("cached response", func(t *testing.T) {
		Cache.Flush()

		handler := scheduleTypeHandler(keyPool)

		// First request populates cache
		req1 := httptest.NewRequest("GET", "/caltrain/scheduletype?operator_id=CT&date=2026-02-16", nil)
		rec1 := httptest.NewRecorder()
		handler(rec1, req1)

		if rec1.Result().StatusCode != http.StatusOK {
			t.Fatalf("First request failed with status %d", rec1.Result().StatusCode)
		}

		// Second request should use cache (verify by stopping mock server)
		mockAPI.Close()

		req2 := httptest.NewRequest("GET", "/caltrain/scheduletype?operator_id=CT&date=2026-05-25", nil)
		rec2 := httptest.NewRecorder()
		handler(rec2, req2)

		resp := rec2.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected cached response, got status %d", resp.StatusCode)
		}

		var result ScheduleTypeResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.ScheduleType != ScheduleHoliday {
			t.Errorf("Expected 'holiday' from cached data, got '%s'", result.ScheduleType)
		}
	})
}

func TestScheduleTypeHandler_NoAPIKeys(t *testing.T) {
	Cache.Flush()

	// Create a pool with an exhausted rate limit
	keyPool := NewKeyPool([]string{"test-key"}, 0, 0)

	req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=2026-03-02", nil)
	rec := httptest.NewRecorder()

	handler := scheduleTypeHandler(keyPool)
	handler(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("Expected status %d, got %d", http.StatusTooManyRequests, resp.StatusCode)
	}
}

func TestScheduleTypeHandler_UpstreamError(t *testing.T) {
	Cache.Flush()

	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockAPI.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()

	keyPool := NewKeyPool([]string{"test-key"}, 10, 5)

	req := httptest.NewRequest("GET", "/caltrain/scheduletype?date=2026-03-02", nil)
	rec := httptest.NewRecorder()

	handler := scheduleTypeHandler(keyPool)
	handler(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("Expected status %d, got %d", http.StatusBadGateway, resp.StatusCode)
	}
}
