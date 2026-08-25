package caltraingateway

import (
	"os"
	"testing"
	"time"
)

// requireTestDatabase connects to the database named by CALTRAIN_TEST_DATABASE_URL,
// skipping the test when it is not set. It restores the previous DB handle on
// cleanup so these tests do not leak state into the nil-DB unit tests.
func requireTestDatabase(t *testing.T) {
	t.Helper()

	connStr := os.Getenv("CALTRAIN_TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("CALTRAIN_TEST_DATABASE_URL not set, skipping database integration test")
	}

	previous := DB
	DB = nil
	if err := InitDB(connStr); err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	t.Cleanup(func() {
		if DB != nil {
			DB.Exec(`DELETE FROM train_departures`)
			DB.Close()
		}
		DB = previous
	})

	if _, err := DB.Exec(`DELETE FROM train_departures`); err != nil {
		t.Fatalf("failed to clear train_departures: %v", err)
	}
}

// sampleDepartureRow builds a row for the given train, stop and departure delay.
func sampleDepartureRow(train, stopID string, expectedDeparture time.Time, delay int) *TrainDepartureRow {
	serviceDate := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	scheduled := time.Date(2026, 8, 24, 15, 33, 0, 0, time.UTC)
	return &TrainDepartureRow{
		ServiceDate:           serviceDate,
		TrainNumber:           train,
		StopID:                stopID,
		Station:               GTFSIDToParentName[stopID],
		Direction:             "NB",
		Line:                  "Local",
		DayOfWeek:             int(serviceDate.Weekday()),
		ScheduleType:          string(ScheduleWeekday),
		ScheduledDeparture:    &scheduled,
		ExpectedDeparture:     &expectedDeparture,
		DepartureDelaySeconds: &delay,
		DepartureSource:       "expected",
		Monitored:             true,
	}
}

// TestDepartureSchemaAndConvergence exercises the real SQL: the migration, the
// converging upsert and the finalize sweep, none of which unit tests can cover.
func TestDepartureSchemaAndConvergence(t *testing.T) {
	requireTestDatabase(t)

	first := time.Date(2026, 8, 24, 15, 35, 0, 0, time.UTC)
	if err := UpsertTrainDeparture(sampleDepartureRow("401", "70021", first, 120)); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	stored, err := ListTrainDepartures(DepartureFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 row, got %d", len(stored))
	}
	firstSeen := stored[0].FirstSeenAt
	if stored[0].ObservationCount != 1 {
		t.Errorf("expected observation_count 1, got %d", stored[0].ObservationCount)
	}

	// A later observation must overwrite the prediction, not create a new row.
	second := time.Date(2026, 8, 24, 15, 38, 0, 0, time.UTC)
	updated := sampleDepartureRow("401", "70021", second, 300)
	updated.VehicleAtStop = true
	if err := UpsertTrainDeparture(updated); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	stored, err = ListTrainDepartures(DepartureFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected the row to converge, got %d rows", len(stored))
	}
	row := stored[0]
	if row.ObservationCount != 2 {
		t.Errorf("expected observation_count 2, got %d", row.ObservationCount)
	}
	if !row.FirstSeenAt.Equal(firstSeen) {
		t.Error("expected first_seen_at to be preserved across observations")
	}
	if row.ExpectedDeparture == nil || !row.ExpectedDeparture.UTC().Equal(second) {
		t.Errorf("expected the latest prediction to win, got %v", row.ExpectedDeparture)
	}
	if row.DepartureDelaySeconds == nil || *row.DepartureDelaySeconds != 300 {
		t.Errorf("expected delay 300, got %v", row.DepartureDelaySeconds)
	}
	if !row.VehicleAtStop {
		t.Error("expected vehicle_at_stop to be set")
	}
	if row.IsFinal() {
		t.Error("expected a live row not to be finalized")
	}

	// vehicle_at_stop is sticky: a later observation without it must not clear it.
	if err := UpsertTrainDeparture(sampleDepartureRow("401", "70021", second, 300)); err != nil {
		t.Fatalf("third upsert failed: %v", err)
	}
	after, err := GetTrainDepartureByID(row.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !after.VehicleAtStop {
		t.Error("expected vehicle_at_stop to remain true once observed")
	}
}

// TestFinalizeStaleDepartures verifies the sweep only finalizes rows whose visit
// has been absent longer than the grace window.
func TestFinalizeStaleDepartures(t *testing.T) {
	requireTestDatabase(t)

	fresh := time.Date(2026, 8, 24, 15, 35, 0, 0, time.UTC)
	if err := UpsertTrainDeparture(sampleDepartureRow("401", "70021", fresh, 60)); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := UpsertTrainDeparture(sampleDepartureRow("402", "70011", fresh, 60)); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	// Age one row past the grace window.
	if _, err := DB.Exec(
		`UPDATE train_departures SET last_seen_at = NOW() - INTERVAL '30 minutes' WHERE train_number = '402'`,
	); err != nil {
		t.Fatalf("failed to age row: %v", err)
	}

	finalized, err := FinalizeStaleDepartures(6 * time.Minute)
	if err != nil {
		t.Fatalf("finalize failed: %v", err)
	}
	if finalized != 1 {
		t.Fatalf("expected 1 row finalized, got %d", finalized)
	}

	rows, err := ListTrainDepartures(DepartureFilter{TrainNumber: "402"}, 10, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(rows) != 1 || !rows[0].IsFinal() {
		t.Error("expected the stale row to be finalized")
	}

	rows, err = ListTrainDepartures(DepartureFilter{TrainNumber: "401"}, 10, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(rows) != 1 || rows[0].IsFinal() {
		t.Error("expected the recent row to stay live")
	}

	// A reappearing visit means the train had not departed, so the row reopens.
	if err := UpsertTrainDeparture(sampleDepartureRow("402", "70011", fresh, 60)); err != nil {
		t.Fatalf("reopen upsert failed: %v", err)
	}
	rows, err = ListTrainDepartures(DepartureFilter{TrainNumber: "402"}, 10, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(rows) != 1 || rows[0].IsFinal() {
		t.Error("expected a reappearing visit to clear finalized_at")
	}
}

// TestDepartureFilteringAndPaging checks filters and paging against real SQL.
func TestDepartureFilteringAndPaging(t *testing.T) {
	requireTestDatabase(t)

	base := time.Date(2026, 8, 24, 15, 35, 0, 0, time.UTC)
	stops := []string{"70011", "70021", "70031", "70041", "70051"}
	for i, stop := range stops {
		if err := UpsertTrainDeparture(sampleDepartureRow("401", stop, base, i*60)); err != nil {
			t.Fatalf("upsert failed: %v", err)
		}
	}

	total, err := CountTrainDepartures(DepartureFilter{})
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if total != len(stops) {
		t.Fatalf("expected %d rows, got %d", len(stops), total)
	}

	byStation, err := CountTrainDepartures(DepartureFilter{Station: "bayshore"})
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if byStation != 1 {
		t.Errorf("expected 1 row for bayshore, got %d", byStation)
	}

	page, err := ListTrainDepartures(DepartureFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("expected a page of 2, got %d", len(page))
	}

	outOfRange, err := CountTrainDepartures(DepartureFilter{From: "2026-09-01", To: "2026-09-30"})
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if outOfRange != 0 {
		t.Errorf("expected 0 rows outside the date range, got %d", outOfRange)
	}

	exported := 0
	if err := StreamTrainDepartures(DepartureFilter{}, func(TrainDepartureRow) error {
		exported++
		return nil
	}); err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	if exported != len(stops) {
		t.Errorf("expected %d exported rows, got %d", len(stops), exported)
	}
}
