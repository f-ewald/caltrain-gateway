package caltraingateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/sync/singleflight"
)

// HolidaysResponse represents the 511 /transit/holidays API response.
type HolidaysResponse struct {
	Content HolidaysContent `json:"Content"`
}

// HolidaysContent contains the service calendar and availability conditions.
type HolidaysContent struct {
	ServiceCalendar        HolidayServiceCalendar        `json:"ServiceCalendar"`
	AvailabilityConditions []HolidayAvailabilityCondition `json:"AvailabilityConditions"`
}

// HolidayServiceCalendar identifies the operator and validity period.
type HolidayServiceCalendar struct {
	ID       string `json:"id"`
	FromDate string `json:"FromDate"`
	ToDate   string `json:"ToDate"`
}

// HolidayAvailabilityCondition represents a single holiday date range.
type HolidayAvailabilityCondition struct {
	Version  string `json:"version"`
	ID       string `json:"id"`
	FromDate string `json:"FromDate"`
	ToDate   string `json:"ToDate"`
}

// ScheduleType represents the type of schedule in effect for a given date.
type ScheduleType string

const (
	ScheduleWeekday  ScheduleType = "weekday"
	ScheduleSaturday ScheduleType = "saturday"
	ScheduleSunday   ScheduleType = "sunday"
	ScheduleHoliday  ScheduleType = "holiday"
)

// ScheduleTypeResponse is the JSON response for the schedule type endpoint.
type ScheduleTypeResponse struct {
	Date         string       `json:"date"`
	OperatorID   string       `json:"operator_id"`
	ScheduleType ScheduleType `json:"schedule_type"`
	Overridden   bool         `json:"overridden"`
}

// holidaysFlight collapses concurrent requests for the same operator's holidays.
var holidaysFlight singleflight.Group

// holidaysCacheTTL bounds how long a fetched holiday calendar is reused before
// a request for that agency triggers a fresh 511 call. Holiday calendars change
// rarely, and this is fetched lazily (only when scheduletype is actually
// requested for that agency, never proactively), so a generous TTL keeps
// per-agency quota usage to at most one request every two days regardless of
// how many agencies are queried.
const holidaysCacheTTL = 48 * time.Hour

// LoadHolidays reads and parses a holidays JSON file.
func LoadHolidays(filename string) (*HolidaysResponse, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read holidays file: %w", err)
	}

	return parseHolidaysJSON(data)
}

// LoadHolidaysFromURL fetches and parses holidays JSON from a URL.
func LoadHolidaysFromURL(url string) (*HolidaysResponse, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch holidays from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return parseHolidaysJSON(data)
}

// parseHolidaysJSON parses the JSON data into a HolidaysResponse.
func parseHolidaysJSON(data []byte) (*HolidaysResponse, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var holidays HolidaysResponse
	if err := json.Unmarshal(data, &holidays); err != nil {
		return nil, fmt.Errorf("failed to parse holidays JSON: %w", err)
	}
	return &holidays, nil
}

// DetermineScheduleType returns the schedule type for a given date based on
// the operator's holiday calendar and the day of the week.
func DetermineScheduleType(holidays *HolidaysResponse, date time.Time) ScheduleType {
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	for _, condition := range holidays.Content.AvailabilityConditions {
		fromDate, err := time.Parse(time.RFC3339, condition.FromDate)
		if err != nil {
			continue
		}
		toDate, err := time.Parse(time.RFC3339, condition.ToDate)
		if err != nil {
			continue
		}

		// Compare calendar dates only
		from := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, time.UTC)
		to := time.Date(toDate.Year(), toDate.Month(), toDate.Day(), 0, 0, 0, 0, time.UTC)

		if !dateOnly.Before(from) && !dateOnly.After(to) {
			return ScheduleHoliday
		}
	}

	switch date.Weekday() {
	case time.Saturday:
		return ScheduleSaturday
	case time.Sunday:
		return ScheduleSunday
	default:
		return ScheduleWeekday
	}
}

// ResolveScheduleType returns the schedule type in effect for an operator's
// date, preferring a manual calendar override over the 511 holiday calendar.
// Overrides let an admin force a schedule for a date 511 doesn't already flag
// (e.g. a Monday holiday that should run the weekend schedule). overridden
// reports whether an override was applied, so callers can surface that for
// transparency.
func ResolveScheduleType(apiKeyPool *KeyPool, operatorID string, date time.Time) (ScheduleType, bool, error) {
	override, err := GetCalendarOverride(operatorID, date)
	if err != nil {
		return "", false, err
	}
	if override != nil {
		return ScheduleType(override.ScheduleType), true, nil
	}

	holidays, err := getHolidays(apiKeyPool, operatorID)
	if err != nil {
		return "", false, err
	}
	return DetermineScheduleType(holidays, date), false, nil
}

// getHolidays fetches holidays for an operator, using cache and request collapsing.
func getHolidays(apiKeyPool *KeyPool, operatorID string) (*HolidaysResponse, error) {
	cacheKey := "holidays:" + operatorID

	// Check cache first
	if cached, found := Cache.Get(cacheKey); found {
		return cached.(*HolidaysResponse), nil
	}

	// Request collapsing per operator
	data, err, _ := holidaysFlight.Do(cacheKey, func() (any, error) {
		apiKey, ok := apiKeyPool.GetAvailableKey()
		if !ok {
			return nil, fmt.Errorf("no available API keys")
		}

		url := fmt.Sprintf("%stransit/holidays?operator_id=%s&format=json&api_key=%s",
			apiBaseURL, operatorID, apiKey.Value)

		holidays, err := LoadHolidaysFromURL(url)
		if err != nil {
			return nil, err
		}

		// Cache for holidaysCacheTTL (fetched lazily, on request only).
		Cache.Set(cacheKey, holidays, holidaysCacheTTL)
		return holidays, nil
	})
	if err != nil {
		return nil, err
	}

	return data.(*HolidaysResponse), nil
}

// scheduleTypeHandler returns the schedule type for a given operator and date.
// Query parameters:
//   - operator_id: operator identifier (default: "CT")
//   - date: date in YYYY-MM-DD format (required)
func scheduleTypeHandler(apiKeyPool *KeyPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operatorID := r.URL.Query().Get("operator_id")
		if operatorID == "" {
			operatorID = "CT"
		}

		dateParam := r.URL.Query().Get("date")
		if dateParam == "" {
			http.Error(w, "Missing required query parameter: date (format: YYYY-MM-DD)", http.StatusBadRequest)
			return
		}

		date, err := time.Parse("2006-01-02", dateParam)
		if err != nil {
			http.Error(w, "Invalid date format. Expected: YYYY-MM-DD", http.StatusBadRequest)
			return
		}

		scheduleType, overridden, err := ResolveScheduleType(apiKeyPool, operatorID, date)
		if err != nil {
			switch err.Error() {
			case "no available API keys":
				http.Error(w, "Rate limit exceeded for all API keys", http.StatusTooManyRequests)
			default:
				http.Error(w, "Failed to fetch holidays", http.StatusBadGateway)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ScheduleTypeResponse{
			Date:         dateParam,
			OperatorID:   operatorID,
			ScheduleType: scheduleType,
			Overridden:   overridden,
		})
	}
}
