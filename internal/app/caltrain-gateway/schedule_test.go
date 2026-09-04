package caltraingateway

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// makeTimetable builds a minimal timetable for one line with the given calls.
// Each call is a stop ID paired with a clock time used for both arrival and
// departure unless a days offset is supplied.
type testCall struct {
	stopID     string
	clock      string
	daysOffset string
}

func makeTimetable(lineRef, trainID string, calls []testCall) *Timetable {
	routeID := "route:" + lineRef
	timetableCalls := make([]Call, 0, len(calls))
	for i, c := range calls {
		timetableCalls = append(timetableCalls, Call{
			Order:                  string(rune('1' + i)),
			ScheduledStopPointRef:  Ref{Ref: c.stopID},
			Arrival:                ArrivalDeparture{Time: c.clock, DaysOffset: c.daysOffset},
			Departure:              ArrivalDeparture{Time: c.clock, DaysOffset: c.daysOffset},
			DestinationDisplayView: DestinationDisplayView{Name: "San Francisco"},
		})
	}

	return &Timetable{
		Content: Content{
			ServiceFrame: ServiceFrame{
				Routes: Routes{Route: []Route{{ID: routeID, LineRef: Ref{Ref: lineRef}}}},
			},
			ServiceCalendarFrame: ServiceCalendarFrame{
				DayTypes: DayTypes{DayType: []DayType{{
					ID:         "daytype:all",
					Properties: DayProperties{PropertyOfDay: PropertyOfDay{DaysOfWeek: "Monday Tuesday Wednesday Thursday Friday Saturday Sunday"}},
				}}},
			},
			TimetableFrame: []TimetableFrame{{
				ID: "Timetable:" + lineRef,
				FrameValidityConditions: FrameValidityConditions{
					AvailabilityCondition: AvailabilityCondition{
						FromDate: "2026-01-31T00:00:00-08:00",
						ToDate:   "2026-08-31T23:59:00-08:00",
						DayTypes: AvailabilityDayType{DayTypeRef: Ref{Ref: "daytype:all"}},
					},
				},
				VehicleJourneys: VehicleJourneys{ServiceJourney: []ServiceJourney{{
					ID:                 trainID,
					JourneyPatternView: JourneyPatternView{RouteRef: Ref{Ref: routeID}, DirectionRef: Ref{Ref: "N"}},
					Calls:              Calls{Call: timetableCalls},
				}}},
			}},
		},
	}
}

// collectionOf assembles a collection from timetables in the given order.
func collectionOf(timetables ...*Timetable) *TimetableCollection {
	tc := NewTimetableCollection()
	for _, tt := range timetables {
		tc.AddTimetable(tt)
	}
	return tc
}

// TestScheduleVersionIsOrderIndependent is the central guard for this feature.
// Departures are appended in whatever order the upstream lines endpoint
// returned, so without canonical ordering an upstream reshuffle would change
// the version despite identical content and stampede every client into
// re-downloading.
func TestScheduleVersionIsOrderIndependent(t *testing.T) {
	local := makeTimetable("Local", "101", []testCall{
		{stopID: "70011", clock: "05:43:00"},
		{stopID: "70021", clock: "05:51:00"},
	})
	limited := makeTimetable("Limited", "401", []testCall{
		{stopID: "70011", clock: "06:10:00"},
		{stopID: "70021", clock: "06:18:00"},
	})

	forward := ScheduleVersion(collectionOf(local, limited))
	reversed := ScheduleVersion(collectionOf(limited, local))

	if forward == "" {
		t.Fatal("expected a non-empty version")
	}
	if forward != reversed {
		t.Errorf("version must not depend on line order: %s vs %s", forward, reversed)
	}
}

func TestScheduleVersionChangesWithContent(t *testing.T) {
	base := collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}}))

	tests := []struct {
		name       string
		collection *TimetableCollection
		wantSame   bool
	}{
		{
			name:       "identical content",
			collection: collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})),
			wantSame:   true,
		},
		{
			name:       "changed departure time",
			collection: collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:44:00"}})),
		},
		{
			name:       "changed train number",
			collection: collectionOf(makeTimetable("Local", "102", []testCall{{stopID: "70011", clock: "05:43:00"}})),
		},
		{
			name:       "changed stop",
			collection: collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70012", clock: "05:43:00"}})),
		},
		{
			name:       "added call",
			collection: collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}, {stopID: "70021", clock: "05:51:00"}})),
		},
	}

	baseVersion := ScheduleVersion(base)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScheduleVersion(tt.collection)
			if tt.wantSame && got != baseVersion {
				t.Errorf("expected an unchanged version, got %s want %s", got, baseVersion)
			}
			if !tt.wantSame && got == baseVersion {
				t.Errorf("expected the version to change, got %s", got)
			}
		})
	}
}

func TestScheduleVersionNilCollection(t *testing.T) {
	if got := ScheduleVersion(nil); got != "" {
		t.Errorf("expected an empty version for a nil collection, got %q", got)
	}
}

func TestExtractScheduleMetadata(t *testing.T) {
	tc := collectionOf(
		makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}}),
		makeTimetable("Limited", "401", []testCall{{stopID: "70011", clock: "06:10:00"}}),
	)

	metadata := ExtractScheduleMetadata(tc)
	if metadata.LineCount != 2 {
		t.Errorf("expected 2 lines, got %d", metadata.LineCount)
	}
	if len(metadata.FrameIDs) != 2 {
		t.Fatalf("expected 2 frame ids, got %v", metadata.FrameIDs)
	}
	if metadata.FrameIDs[0] != "Timetable:Limited" || metadata.FrameIDs[1] != "Timetable:Local" {
		t.Errorf("expected sorted frame ids, got %v", metadata.FrameIDs)
	}
	if !metadata.HasValidity() {
		t.Fatal("expected a validity window")
	}
	if got := formatValidityDate(metadata.ValidTo); got != "2026-08-31" {
		t.Errorf("expected valid_to 2026-08-31, got %s", got)
	}
}

func TestScheduleMetadataExpiry(t *testing.T) {
	validTo, _ := parseValidityDate("2026-08-31T23:59:00-08:00")
	validFrom, _ := parseValidityDate("2026-01-31T00:00:00-08:00")
	metadata := ScheduleMetadata{ValidFrom: validFrom, ValidTo: validTo}

	tests := []struct {
		name     string
		now      string
		wantDays int
		expired  bool
	}{
		{"well before expiry", "2026-08-25T12:00:00-07:00", 6, false},
		{"day before", "2026-08-30T12:00:00-07:00", 1, false},
		{"on the final day", "2026-08-31T12:00:00-07:00", 0, false},
		{"after expiry", "2026-09-02T12:00:00-07:00", -2, true},
		{"late evening stays on its operating day", "2026-08-25T23:30:00-07:00", 6, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tt.now)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			days, ok := metadata.ExpiresInDays(now)
			if !ok {
				t.Fatal("expected a validity window")
			}
			if days != tt.wantDays {
				t.Errorf("expected %d days, got %d", tt.wantDays, days)
			}
			if metadata.Expired(now) != tt.expired {
				t.Errorf("expected expired=%v", tt.expired)
			}
		})
	}
}

// TestParseValidityDateIgnoresFeedOffset pins the handling of 511's fixed
// -08:00 offset. Honouring it would push an end-of-day bound in summer onto the
// following calendar day and overstate how long the schedule remains valid.
func TestParseValidityDateIgnoresFeedOffset(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
		ok    bool
	}{
		{"summer end of day keeps its date", "2026-08-31T23:59:00-08:00", "2026-08-31", true},
		{"winter start of day", "2026-01-31T00:00:00-08:00", "2026-01-31", true},
		{"plain date", "2026-08-31", "2026-08-31", true},
		{"empty", "", "", false},
		{"malformed", "not-a-date", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, ok := parseValidityDate(tt.value)
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if !ok {
				return
			}
			if got := formatValidityDate(parsed); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestScheduleMetadataWithoutValidity(t *testing.T) {
	metadata := ScheduleMetadata{}
	if _, ok := metadata.ExpiresInDays(time.Now()); ok {
		t.Error("expected no expiry without a validity window")
	}
	if metadata.Expired(time.Now()) {
		t.Error("a schedule with no declared window must not be reported as expired")
	}
}

// TestNextRunAt covers the wall-clock scheduling, including both daylight
// saving transitions. A fixed 24h interval would drift across these and
// eventually run during service hours.
func TestNextRunAt(t *testing.T) {
	pacific := pacificLocation()

	tests := []struct {
		name string
		now  string
		want string
	}{
		{"before the hour today", "2026-08-25T01:00:00-07:00", "2026-08-25 03:00:00"},
		{"after the hour rolls to tomorrow", "2026-08-25T04:00:00-07:00", "2026-08-26 03:00:00"},
		{"exactly at the hour rolls forward", "2026-08-25T03:00:00-07:00", "2026-08-26 03:00:00"},
		{"across spring forward", "2026-03-07T20:00:00-08:00", "2026-03-08 03:00:00"},
		{"across fall back", "2026-10-31T20:00:00-07:00", "2026-11-01 03:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tt.now)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			next := nextRunAt(now, 3)

			if got := next.In(pacific).Format("2006-01-02 15:04:05"); got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
			if !next.After(now) {
				t.Errorf("next run %s must be after %s", next, now)
			}
			if next.In(pacific).Hour() != 3 {
				t.Errorf("expected local hour 3, got %d", next.In(pacific).Hour())
			}
		})
	}
}

// TestComputeServiceWindow checks the window derived from the timetable,
// including trains that cross midnight via DaysOffset.
func TestComputeServiceWindow(t *testing.T) {
	tests := []struct {
		name      string
		calls     []testCall
		wantStart time.Duration
		wantEnd   time.Duration
	}{
		{
			name:      "same day service",
			calls:     []testCall{{stopID: "70011", clock: "05:43:00"}, {stopID: "70021", clock: "19:58:00"}},
			wantStart: 5*time.Hour + 43*time.Minute - serviceWindowMargin,
			wantEnd:   19*time.Hour + 58*time.Minute + serviceWindowMargin,
		},
		{
			name: "train crossing midnight extends past 24h",
			calls: []testCall{
				{stopID: "70011", clock: "23:45:00"},
				{stopID: "70021", clock: "01:20:00", daysOffset: "1"},
			},
			wantStart: 23*time.Hour + 45*time.Minute - serviceWindowMargin,
			wantEnd:   25*time.Hour + 20*time.Minute + serviceWindowMargin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := collectionOf(makeTimetable("Local", "101", tt.calls))
			window, ok := computeServiceWindow(tc, Monday)
			if !ok {
				t.Fatal("expected a window")
			}
			if window.start != tt.wantStart {
				t.Errorf("expected start %s, got %s", tt.wantStart, window.start)
			}
			if window.end != tt.wantEnd {
				t.Errorf("expected end %s, got %s", tt.wantEnd, window.end)
			}
		})
	}
}

func TestShouldPollDepartures(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{
		{stopID: "70011", clock: "05:43:00"},
		{stopID: "70021", clock: "19:58:00"},
	})))

	tests := []struct {
		name string
		now  string
		want bool
	}{
		{"mid morning", "2026-08-25T09:00:00-07:00", true},
		{"just inside the leading margin", "2026-08-25T05:20:00-07:00", true},
		{"before the leading margin", "2026-08-25T04:00:00-07:00", false},
		{"just inside the trailing margin", "2026-08-25T20:20:00-07:00", true},
		{"after the trailing margin", "2026-08-25T21:30:00-07:00", false},
		{"deep overnight", "2026-08-26T02:00:00-07:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tt.now)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			if got := shouldPollDepartures(now); got != tt.want {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// TestShouldPollDeparturesFailsOpen confirms that an unknown service window
// keeps polling rather than silently collecting nothing all day.
func TestShouldPollDeparturesFailsOpen(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	if !shouldPollDepartures(time.Now()) {
		t.Error("expected polling to continue when no timetable is loaded")
	}
}

// swapSchedule isolates a test from the process-wide schedule state.
func swapSchedule(t *testing.T) func() {
	t.Helper()
	previous := schedule
	schedule = &scheduleState{}
	return func() { schedule = previous }
}

func TestScheduleVersionHandler(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	recorder := httptest.NewRecorder()
	scheduleVersionHandler(recorder, httptest.NewRequest(http.MethodGet, "/caltrain/timetable/version", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before a timetable loads, got %d", recorder.Code)
	}

	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})))

	recorder = httptest.NewRecorder()
	scheduleVersionHandler(recorder, httptest.NewRequest(http.MethodGet, "/caltrain/timetable/version", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	var response ScheduleVersionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Version == "" {
		t.Error("expected a version")
	}
	if response.ValidTo != "2026-08-31" {
		t.Errorf("expected valid_to 2026-08-31, got %s", response.ValidTo)
	}
	if response.LineCount != 1 {
		t.Errorf("expected 1 line, got %d", response.LineCount)
	}
	if response.Stale {
		t.Error("a freshly published schedule must not be reported stale")
	}
	if response.ExpiresInDays == nil {
		t.Error("expected expires_in_days to be present")
	}
}

// TestTimetableHandlerETag covers revalidation: a matching If-None-Match must
// return 304, and the tag must differ per representation because the body
// varies by weekday and station.
func TestTimetableHandlerETag(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()
	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{
		{stopID: "70011", clock: "05:43:00"},
		{stopID: "70021", clock: "05:51:00"},
	})))

	recorder := httptest.NewRecorder()
	timetableHandler(recorder, httptest.NewRequest(http.MethodGet, "/caltrain/timetable", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected an ETag")
	}
	if etag[:2] != "W/" {
		t.Errorf("expected a weak ETag, got %s", etag)
	}

	// Same tag returns 304 with no body.
	request := httptest.NewRequest(http.MethodGet, "/caltrain/timetable", nil)
	request.Header.Set("If-None-Match", etag)
	recorder = httptest.NewRecorder()
	timetableHandler(recorder, request)
	if recorder.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("a 304 must not carry a body, got %d bytes", recorder.Body.Len())
	}

	// A stale tag returns the payload again.
	request = httptest.NewRequest(http.MethodGet, "/caltrain/timetable", nil)
	request.Header.Set("If-None-Match", `W/"outdated-all-all"`)
	recorder = httptest.NewRecorder()
	timetableHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Errorf("expected 200 for a stale tag, got %d", recorder.Code)
	}

	// The tag for a different representation must not match, since that body
	// contains only one station.
	request = httptest.NewRequest(http.MethodGet, "/caltrain/timetable?station=70011", nil)
	request.Header.Set("If-None-Match", etag)
	recorder = httptest.NewRecorder()
	timetableHandler(recorder, request)
	if recorder.Code == http.StatusNotModified {
		t.Error("a tag from the unfiltered representation must not satisfy a station-filtered request")
	}
}

func TestScheduleETagVariants(t *testing.T) {
	tests := []struct {
		name    string
		weekday string
		station string
	}{
		{"unfiltered", "", ""},
		{"weekday only", "Monday", ""},
		{"station only", "", "70011"},
		{"both", "Monday", "70011"},
	}

	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			etag := scheduleETag("abc123", tt.weekday, tt.station)
			if etag == "" {
				t.Fatal("expected an ETag")
			}
			if previous, clash := seen[etag]; clash {
				t.Errorf("representation %q shares an ETag with %q", tt.name, previous)
			}
			seen[etag] = tt.name
		})
	}

	if scheduleETag("", "", "") != "" {
		t.Error("expected no ETag without a version")
	}
}

// TestRefreshKeepsPreviousScheduleOnFailure is the atomicity guarantee: a failed
// refresh must never replace a working schedule.
func TestRefreshKeepsPreviousScheduleOnFailure(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	good := collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}}))
	schedule.Publish(good)
	original := schedule.Version()

	refresher := NewScheduleRefresher(NewKeyPool([]string{"key"}, 10, 10), "CT", func(string) (*TimetableCollection, error) {
		return nil, errRateLimited
	}, 3, 0)

	if refresher.Refresh() {
		t.Error("expected the refresh to report failure")
	}
	if got := schedule.Version(); got != original {
		t.Errorf("expected the previous schedule to survive, version changed %s -> %s", original, got)
	}
	if schedule.Collection() == nil {
		t.Error("a failed refresh must not clear the schedule")
	}
}

func TestRefreshPublishesNewSchedule(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})))
	original := schedule.Version()

	refresher := NewScheduleRefresher(NewKeyPool([]string{"key"}, 10, 10), "CT", func(string) (*TimetableCollection, error) {
		return collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "06:00:00"}})), nil
	}, 3, 0)

	if !refresher.Refresh() {
		t.Fatal("expected the refresh to succeed")
	}
	if schedule.Version() == original {
		t.Error("expected the version to change after a real schedule change")
	}
}

// TestScheduleRefresherDueForRefresh covers the minInterval gate that lets an
// agency with a bigger payload (BART) refresh less often than nightly: it must
// always be due when nothing has published yet or minInterval is zero, and
// only due again once minInterval has actually elapsed since the last publish.
func TestScheduleRefresherDueForRefresh(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	loader := func(string) (*TimetableCollection, error) {
		return collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})), nil
	}

	t.Run("zero minInterval is always due", func(t *testing.T) {
		refresher := NewScheduleRefresher(NewKeyPool([]string{"key"}, 10, 10), "CT", loader, 3, 0)
		if !refresher.dueForRefresh() {
			t.Error("expected a zero minInterval to always be due")
		}
	})

	t.Run("not yet due right after a publish", func(t *testing.T) {
		schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})))
		refresher := NewScheduleRefresher(NewKeyPool([]string{"key"}, 10, 10), "CT", loader, 3, 24*time.Hour)
		if refresher.dueForRefresh() {
			t.Error("expected a refresh moments after publishing to not be due yet")
		}
	})

	t.Run("due when nothing has published yet", func(t *testing.T) {
		schedule = &scheduleState{}
		refresher := NewScheduleRefresher(NewKeyPool([]string{"key"}, 10, 10), "CT", loader, 3, 24*time.Hour)
		if !refresher.dueForRefresh() {
			t.Error("expected a refresh to be due when nothing has ever published")
		}
	})
}

// TestPublishIgnoresNilCollection guards the invariant that nothing can erase a
// working schedule.
func TestPublishIgnoresNilCollection(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}})))
	version := schedule.Version()

	schedule.Publish(nil)

	if schedule.Version() != version || schedule.Collection() == nil {
		t.Error("publishing nil must leave the existing schedule untouched")
	}
}

// TestScheduleStateConcurrentAccess exercises the lock that makes the nightly
// refresh safe: publishing happens on a background goroutine while handlers
// read. Run with -race, this fails if the state is ever read mid-swap.
func TestScheduleStateConcurrentAccess(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()

	first := collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "05:43:00"}}))
	second := collectionOf(makeTimetable("Local", "101", []testCall{{stopID: "70011", clock: "06:00:00"}}))
	schedule.Publish(first)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				if i%2 == 0 {
					schedule.Publish(first)
				} else {
					schedule.Publish(second)
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 200; n++ {
				collection, version, metadata, _, _ := schedule.Snapshot()
				if collection == nil || version == "" {
					t.Error("snapshot must never observe an unpublished schedule")
					return
				}
				// The version must always match the collection it was taken
				// with; a torn read would break this.
				if version != ScheduleVersion(collection) {
					t.Error("version disagrees with the collection it was read with")
					return
				}
				_ = metadata
				_ = shouldPollDepartures(time.Now())
			}
		}()
	}
	wg.Wait()
}

// TestTimetableETagThroughGzipMiddleware exercises the real middleware chain.
// A 304 carries no body, so the gzip wrapper must neither advertise gzip nor
// close the stream: doing so would append gzip framing to a bodyless response.
func TestTimetableETagThroughGzipMiddleware(t *testing.T) {
	restore := swapSchedule(t)
	defer restore()
	schedule.Publish(collectionOf(makeTimetable("Local", "101", []testCall{
		{stopID: "70011", clock: "05:43:00"},
	})))

	handler := gzipMiddleware(timetableHandler)

	request := httptest.NewRequest(http.MethodGet, "/caltrain/timetable", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Error("expected a gzip-encoded body")
	}
	etag := recorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected the ETag to survive the gzip wrapper")
	}

	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read the gzip body: %v", err)
	}
	var departures map[string][]TrainDeparture
	if err := json.Unmarshal(decoded, &departures); err != nil {
		t.Fatalf("decompressed body is not valid JSON: %v", err)
	}
	if len(departures) == 0 {
		t.Error("expected departures in the body")
	}

	// Revalidate: the 304 must be genuinely empty and not claim gzip.
	request = httptest.NewRequest(http.MethodGet, "/caltrain/timetable", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("If-None-Match", etag)
	recorder = httptest.NewRecorder()
	handler(recorder, request)

	if recorder.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("a 304 must have an empty body, got %d bytes", recorder.Body.Len())
	}
	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "" {
		t.Errorf("a bodyless 304 must not advertise Content-Encoding, got %q", encoding)
	}
	if recorder.Header().Get("ETag") != etag {
		t.Error("expected the ETag to be echoed on the 304")
	}
}
