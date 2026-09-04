package caltraingateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// requireCalendarOverridesTestDatabase connects to the shared test database and
// clears calendar_overrides, so each test starts from an empty table regardless
// of what earlier tests left behind.
func requireCalendarOverridesTestDatabase(t *testing.T) {
	t.Helper()
	requireTestDatabase(t)

	if _, err := DB.Exec(`DELETE FROM calendar_overrides`); err != nil {
		t.Fatalf("failed to clear calendar_overrides: %v", err)
	}
	t.Cleanup(func() {
		if DB != nil {
			DB.Exec(`DELETE FROM calendar_overrides`)
		}
	})
}

func TestCalendarOverrides_MigrationCreatesTable(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	if _, err := DB.Exec(`SELECT id, agency_id, override_date, schedule_type, note, created_at, updated_at FROM calendar_overrides LIMIT 0`); err != nil {
		t.Fatalf("calendar_overrides table missing expected columns: %v", err)
	}
}

func TestUpsertCalendarOverride_InsertsAndReturnsViaGet(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	date := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC) // Labor Day 2026 (Monday)
	if err := UpsertCalendarOverride("CT", date, string(ScheduleSunday), "Labor Day"); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}

	row, err := GetCalendarOverride("CT", date)
	if err != nil {
		t.Fatalf("GetCalendarOverride failed: %v", err)
	}
	if row == nil {
		t.Fatal("expected an override row, got nil")
	}
	if row.ScheduleType != string(ScheduleSunday) {
		t.Errorf("expected schedule_type 'sunday', got %q", row.ScheduleType)
	}
	if row.Note != "Labor Day" {
		t.Errorf("expected note 'Labor Day', got %q", row.Note)
	}
	if row.AgencyID != "CT" {
		t.Errorf("expected agency_id 'CT', got %q", row.AgencyID)
	}
}

func TestUpsertCalendarOverride_EditsInPlace(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	date := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := UpsertCalendarOverride("CT", date, string(ScheduleSaturday), "first"); err != nil {
		t.Fatalf("initial UpsertCalendarOverride failed: %v", err)
	}
	if err := UpsertCalendarOverride("CT", date, string(ScheduleSunday), "second"); err != nil {
		t.Fatalf("edit-in-place UpsertCalendarOverride failed: %v", err)
	}

	rows, err := ListCalendarOverrides()
	if err != nil {
		t.Fatalf("ListCalendarOverrides failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row after edit-in-place, got %d", len(rows))
	}
	if rows[0].ScheduleType != string(ScheduleSunday) || rows[0].Note != "second" {
		t.Errorf("expected the edited values (sunday/second), got (%s/%s)", rows[0].ScheduleType, rows[0].Note)
	}
}

func TestListCalendarOverrides_OrderedByDateDescending(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	earlier := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := UpsertCalendarOverride("CT", earlier, string(ScheduleHoliday), ""); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}
	if err := UpsertCalendarOverride("CT", later, string(ScheduleSunday), ""); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}

	rows, err := ListCalendarOverrides()
	if err != nil {
		t.Fatalf("ListCalendarOverrides failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if !rows[0].OverrideDate.Equal(later) {
		t.Errorf("expected the later date first, got %v", rows[0].OverrideDate)
	}
}

func TestDeleteCalendarOverride_Removes(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	date := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := UpsertCalendarOverride("CT", date, string(ScheduleSunday), ""); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}
	rows, err := ListCalendarOverrides()
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 row before delete, got %d rows (err=%v)", len(rows), err)
	}

	if err := DeleteCalendarOverride(rows[0].ID); err != nil {
		t.Fatalf("DeleteCalendarOverride failed: %v", err)
	}

	row, err := GetCalendarOverride("CT", date)
	if err != nil {
		t.Fatalf("GetCalendarOverride failed: %v", err)
	}
	if row != nil {
		t.Error("expected the override to be gone after delete")
	}
}

func TestResolveScheduleType_NoOverrideFallsThroughToHolidayCalendar(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleHolidaysJSON))
	}))
	defer mockAPI.Close()
	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()
	Cache.Flush()

	keyPool := NewKeyPool([]string{"test-key"}, 10, 5)

	scheduleType, overridden, err := ResolveScheduleType(keyPool, "CT", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveScheduleType failed: %v", err)
	}
	if overridden {
		t.Error("expected overridden=false with no override present")
	}
	if scheduleType != ScheduleWeekday {
		t.Errorf("expected weekday from the holiday calendar, got %q", scheduleType)
	}
}

func TestResolveScheduleType_OverridePreemptsHolidayCalendar(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	// Labor Day 2026-09-07 is a Monday, which the holiday calendar below would
	// otherwise resolve to "weekday".
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleHolidaysJSON))
	}))
	defer mockAPI.Close()
	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()
	Cache.Flush()

	laborDay := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := UpsertCalendarOverride("CT", laborDay, string(ScheduleSunday), "Labor Day"); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}

	keyPool := NewKeyPool([]string{"test-key"}, 10, 5)
	scheduleType, overridden, err := ResolveScheduleType(keyPool, "CT", laborDay)
	if err != nil {
		t.Fatalf("ResolveScheduleType failed: %v", err)
	}
	if !overridden {
		t.Error("expected overridden=true when an override exists")
	}
	if scheduleType != ScheduleSunday {
		t.Errorf("expected the overridden 'sunday' schedule, got %q", scheduleType)
	}
}

func TestScheduleTypeHandler_WithOverride(t *testing.T) {
	requireCalendarOverridesTestDatabase(t)

	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(exampleHolidaysJSON))
	}))
	defer mockAPI.Close()
	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()
	Cache.Flush()

	laborDay := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := UpsertCalendarOverride("CT", laborDay, string(ScheduleSunday), "Labor Day"); err != nil {
		t.Fatalf("UpsertCalendarOverride failed: %v", err)
	}

	keyPool := NewKeyPool([]string{"test-key"}, 10, 5)
	req := httptest.NewRequest("GET", "/caltrain/scheduletype?operator_id=CT&date=2026-09-07", nil)
	rec := httptest.NewRecorder()
	scheduleTypeHandler(keyPool)(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var result ScheduleTypeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ScheduleType != ScheduleSunday {
		t.Errorf("expected overridden schedule_type 'sunday', got %q", result.ScheduleType)
	}
	if !result.Overridden {
		t.Error("expected overridden=true in the response")
	}
}
