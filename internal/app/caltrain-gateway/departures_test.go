package caltraingateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const sampleStopMonitoringJSON = `{
  "ServiceDelivery": {
    "ResponseTimestamp": "2026-08-24T15:30:00Z",
    "ProducerRef": "CT",
    "StopMonitoringDelivery": {
      "ResponseTimestamp": "2026-08-24T15:30:00Z",
      "MonitoredStopVisit": [
        {
          "RecordedAtTime": "2026-08-24T15:29:40Z",
          "MonitoringRef": "70021",
          "MonitoredVehicleJourney": {
            "LineRef": "Local",
            "DirectionRef": "NB",
            "FramedVehicleJourneyRef": {
              "DataFrameRef": "2026-08-24",
              "DatedVehicleJourneyRef": "401"
            },
            "PublishedLineName": "Local",
            "OperatorRef": "CT",
            "VehicleJourneyName": "401",
            "Monitored": true,
            "VehicleRef": "9007",
            "MonitoredCall": {
              "StopPointRef": "70021",
              "StopPointName": "22nd Street",
              "VehicleAtStop": false,
              "AimedArrivalTime": "2026-08-24T15:32:00Z",
              "ExpectedArrivalTime": "2026-08-24T15:35:00Z",
              "ActualArrivalTime": null,
              "AimedDepartureTime": "2026-08-24T15:33:00Z",
              "ExpectedDepartureTime": "2026-08-24T15:36:30Z",
              "ActualDepartureTime": null
            }
          }
        }
      ]
    }
  }
}`

func TestParseStopMonitoringJSON(t *testing.T) {
	response, err := parseStopMonitoringJSON([]byte(sampleStopMonitoringJSON))
	if err != nil {
		t.Fatalf("failed to parse stop monitoring JSON: %v", err)
	}

	visits := response.Visits()
	if len(visits) != 1 {
		t.Fatalf("expected 1 visit, got %d", len(visits))
	}

	journey := visits[0].MonitoredVehicleJourney
	if got := journey.TrainNumber(); got != "401" {
		t.Errorf("expected train number 401, got %q", got)
	}
	if !bool(journey.Monitored) {
		t.Error("expected Monitored to be true")
	}
	if bool(journey.MonitoredCall.VehicleAtStop) {
		t.Error("expected VehicleAtStop to be false")
	}
	if journey.MonitoredCall.ActualDepartureTime != "" {
		t.Errorf("expected null ActualDepartureTime to decode as empty, got %q",
			journey.MonitoredCall.ActualDepartureTime)
	}
}

// TestParseStopMonitoringVariants covers producer-specific encodings that would
// otherwise abort parsing of the whole feed.
func TestParseStopMonitoringVariants(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantVisits  int
		wantTrain   string
		wantAtStop  bool
		wantMonitor bool
	}{
		{
			name: "delivery as array",
			json: `{"ServiceDelivery":{"StopMonitoringDelivery":[{"MonitoredStopVisit":[
				{"MonitoringRef":"70011","MonitoredVehicleJourney":{
					"FramedVehicleJourneyRef":{"DatedVehicleJourneyRef":"506"},
					"MonitoredCall":{"StopPointRef":"70011"}}}]}]}}`,
			wantVisits: 1,
			wantTrain:  "506",
		},
		{
			name: "booleans as strings",
			json: `{"ServiceDelivery":{"StopMonitoringDelivery":{"MonitoredStopVisit":[
				{"MonitoringRef":"70011","MonitoredVehicleJourney":{
					"Monitored":"true","VehicleJourneyName":"110",
					"MonitoredCall":{"StopPointRef":"70011","VehicleAtStop":"true"}}}]}}}`,
			wantVisits:  1,
			wantTrain:   "110",
			wantAtStop:  true,
			wantMonitor: true,
		},
		{
			name: "line refs as arrays",
			json: `{"ServiceDelivery":{"StopMonitoringDelivery":{"MonitoredStopVisit":[
				{"MonitoringRef":["70011"],"MonitoredVehicleJourney":{
					"LineRef":["Limited"],"VehicleJourneyName":"220",
					"MonitoredCall":{"StopPointRef":["70011"]}}}]}}}`,
			wantVisits: 1,
			wantTrain:  "220",
		},
		{
			name:       "empty delivery",
			json:       `{"ServiceDelivery":{"StopMonitoringDelivery":null}}`,
			wantVisits: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := parseStopMonitoringJSON([]byte(tt.json))
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			visits := response.Visits()
			if len(visits) != tt.wantVisits {
				t.Fatalf("expected %d visits, got %d", tt.wantVisits, len(visits))
			}
			if tt.wantVisits == 0 {
				return
			}
			journey := visits[0].MonitoredVehicleJourney
			if got := journey.TrainNumber(); got != tt.wantTrain {
				t.Errorf("expected train %q, got %q", tt.wantTrain, got)
			}
			if bool(journey.MonitoredCall.VehicleAtStop) != tt.wantAtStop {
				t.Errorf("expected VehicleAtStop %v", tt.wantAtStop)
			}
			if bool(journey.Monitored) != tt.wantMonitor {
				t.Errorf("expected Monitored %v", tt.wantMonitor)
			}
		})
	}
}

func TestParseStopMonitoringJSONWithBOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(sampleStopMonitoringJSON)...)
	response, err := parseStopMonitoringJSON(data)
	if err != nil {
		t.Fatalf("failed to parse stop monitoring JSON with BOM: %v", err)
	}
	if len(response.Visits()) != 1 {
		t.Errorf("expected 1 visit, got %d", len(response.Visits()))
	}
}

func TestLoadStopMonitoringFromURLRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	if _, err := LoadStopMonitoringFromURL(server.URL); err != errRateLimited {
		t.Errorf("expected errRateLimited, got %v", err)
	}
}

// TestOperatingDate verifies the 3am Pacific operating-day boundary, which keeps
// trains that cross midnight grouped with the day their run started, and checks
// that a DST transition does not shift the date.
func TestOperatingDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"midday pacific", "2026-08-24T20:30:00Z", "2026-08-24"},            // 13:30 PDT Mon
		{"late evening pacific", "2026-08-25T06:30:00Z", "2026-08-24"},      // 23:30 PDT Mon
		{"after midnight before 3am", "2026-08-25T08:30:00Z", "2026-08-24"}, // 01:30 PDT Tue
		{"after 3am boundary", "2026-08-25T11:30:00Z", "2026-08-25"},        // 04:30 PDT Tue
		{"dst spring forward", "2026-03-08T18:00:00Z", "2026-03-08"},        // 11:00 PDT Sun
		{"dst fall back", "2026-11-01T17:00:00Z", "2026-11-01"},             // 09:00 PST Sun
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := time.Parse(time.RFC3339, tt.input)
			if err != nil {
				t.Fatalf("bad test input: %v", err)
			}
			if got := operatingDate(parsed).Format(dateLayout); got != tt.want {
				t.Errorf("operatingDate(%s) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeriveServiceDate(t *testing.T) {
	fallback, _ := time.Parse(time.RFC3339, "2026-08-25T06:30:00Z") // 23:30 PDT Aug 24

	tests := []struct {
		name         string
		dataFrameRef string
		fallback     time.Time
		want         string
		wantOK       bool
	}{
		{"uses data frame ref", "2026-08-24", time.Time{}, "2026-08-24", true},
		{"ignores malformed ref and falls back", "not-a-date", fallback, "2026-08-24", true},
		{"falls back to operating day", "", fallback, "2026-08-24", true},
		{"reports failure without any source", "", time.Time{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveServiceDate(tt.dataFrameRef, tt.fallback)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v", tt.wantOK, ok)
			}
			if !ok {
				return
			}
			if formatted := got.Format(dateLayout); formatted != tt.want {
				t.Errorf("expected %s, got %s", tt.want, formatted)
			}
		})
	}
}

// TestDayOfWeekFromServiceDate guards the timezone trap: deriving the weekday
// from a raw UTC timestamp mislabels every evening train, so it must come from
// the operating day instead.
func TestDayOfWeekFromServiceDate(t *testing.T) {
	// 23:30 PDT on Monday 2026-08-24, which is already Tuesday in UTC.
	instant, _ := time.Parse(time.RFC3339, "2026-08-25T06:30:00Z")

	if instant.UTC().Weekday() != time.Tuesday {
		t.Fatalf("test premise wrong: expected UTC weekday Tuesday, got %s", instant.UTC().Weekday())
	}

	serviceDate := operatingDate(instant)
	if serviceDate.Weekday() != time.Monday {
		t.Errorf("expected service weekday Monday, got %s", serviceDate.Weekday())
	}
}

func TestDelaySeconds(t *testing.T) {
	base, _ := time.Parse(time.RFC3339, "2026-08-24T15:00:00Z")

	tests := []struct {
		name      string
		observed  time.Time
		observeOK bool
		schedOK   bool
		want      *int
	}{
		{"late", base.Add(3 * time.Minute), true, true, intPtr(180)},
		{"early", base.Add(-90 * time.Second), true, true, intPtr(-90)},
		{"on time", base, true, true, intPtr(0)},
		{"unknown observation", time.Time{}, false, true, nil},
		{"unknown schedule", base, true, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := delaySeconds(base, tt.observed, tt.schedOK, tt.observeOK)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %d", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %d, got nil", *tt.want)
			}
			if *got != *tt.want {
				t.Errorf("expected %d, got %d", *tt.want, *got)
			}
		})
	}
}

// TestDepartureObservation confirms an authoritative actual time wins over a
// prediction, and that the source is recorded so inferred rows stay identifiable.
func TestDepartureObservation(t *testing.T) {
	tests := []struct {
		name       string
		call       MonitoredCall
		wantOK     bool
		wantSource string
		wantTime   string
	}{
		{
			name: "prefers actual when present",
			call: MonitoredCall{
				ActualDepartureTime:   "2026-08-24T15:37:00Z",
				ExpectedDepartureTime: "2026-08-24T15:36:30Z",
			},
			wantOK:     true,
			wantSource: "actual",
			wantTime:   "2026-08-24T15:37:00Z",
		},
		{
			name:       "falls back to expected",
			call:       MonitoredCall{ExpectedDepartureTime: "2026-08-24T15:36:30Z"},
			wantOK:     true,
			wantSource: "expected",
			wantTime:   "2026-08-24T15:36:30Z",
		},
		{
			name:   "reports failure when neither present",
			call:   MonitoredCall{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, source := departureObservation(&tt.call)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v", tt.wantOK, ok)
			}
			if !ok {
				return
			}
			if source != tt.wantSource {
				t.Errorf("expected source %q, got %q", tt.wantSource, source)
			}
			if formatted := got.Format(time.RFC3339); formatted != tt.wantTime {
				t.Errorf("expected %s, got %s", tt.wantTime, formatted)
			}
		})
	}
}

func TestBuildDepartureRow(t *testing.T) {
	response, err := parseStopMonitoringJSON([]byte(sampleStopMonitoringJSON))
	if err != nil {
		t.Fatalf("failed to parse sample: %v", err)
	}

	row, ok := buildDepartureRow(response.Visits()[0], ScheduleWeekday)
	if !ok {
		t.Fatal("expected the sample visit to produce a row")
	}

	if row.TrainNumber != "401" {
		t.Errorf("expected train 401, got %s", row.TrainNumber)
	}
	if row.StopID != "70021" {
		t.Errorf("expected stop 70021, got %s", row.StopID)
	}
	if row.Station != "22nd_street" {
		t.Errorf("expected station 22nd_street, got %s", row.Station)
	}
	if row.ServiceDate.Format(dateLayout) != "2026-08-24" {
		t.Errorf("expected service date 2026-08-24, got %s", row.ServiceDate.Format(dateLayout))
	}
	if row.DayOfWeek != int(time.Monday) {
		t.Errorf("expected Monday, got %s", time.Weekday(row.DayOfWeek))
	}
	if row.ScheduleType != string(ScheduleWeekday) {
		t.Errorf("expected weekday schedule type, got %s", row.ScheduleType)
	}
	if row.DepartureSource != "expected" {
		t.Errorf("expected inferred source, got %s", row.DepartureSource)
	}
	if row.DepartureDelaySeconds == nil || *row.DepartureDelaySeconds != 210 {
		t.Errorf("expected 210s departure delay, got %v", row.DepartureDelaySeconds)
	}
	if row.ArrivalDelaySeconds == nil || *row.ArrivalDelaySeconds != 180 {
		t.Errorf("expected 180s arrival delay, got %v", row.ArrivalDelaySeconds)
	}
	if row.DwellSeconds == nil || *row.DwellSeconds != 90 {
		t.Errorf("expected 90s dwell, got %v", row.DwellSeconds)
	}
}

// TestBuildDepartureRowRejectsIncomplete ensures visits that cannot satisfy the
// unique key are skipped rather than written as unusable rows.
func TestBuildDepartureRowRejectsIncomplete(t *testing.T) {
	tests := []struct {
		name  string
		visit MonitoredStopVisit
	}{
		{
			name: "missing train number",
			visit: MonitoredStopVisit{
				MonitoringRef: "70011",
				MonitoredVehicleJourney: MonitoredVehicleJourney{
					MonitoredCall: MonitoredCall{
						StopPointRef:       "70011",
						AimedDepartureTime: "2026-08-24T15:33:00Z",
					},
				},
			},
		},
		{
			name: "missing stop",
			visit: MonitoredStopVisit{
				MonitoredVehicleJourney: MonitoredVehicleJourney{
					VehicleJourneyName: "401",
					MonitoredCall: MonitoredCall{
						AimedDepartureTime: "2026-08-24T15:33:00Z",
					},
				},
			},
		},
		{
			name: "no usable times",
			visit: MonitoredStopVisit{
				MonitoringRef: "70011",
				MonitoredVehicleJourney: MonitoredVehicleJourney{
					VehicleJourneyName: "401",
					MonitoredCall:      MonitoredCall{StopPointRef: "70011"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := buildDepartureRow(tt.visit, ScheduleWeekday); ok {
				t.Error("expected the visit to be rejected")
			}
		})
	}
}

// TestBuildDepartureRowFallsBackToMonitoringRef covers feeds that omit
// StopPointRef inside MonitoredCall but identify the stop on the visit.
func TestBuildDepartureRowFallsBackToMonitoringRef(t *testing.T) {
	visit := MonitoredStopVisit{
		MonitoringRef: "70011",
		MonitoredVehicleJourney: MonitoredVehicleJourney{
			VehicleJourneyName: "401",
			MonitoredCall: MonitoredCall{
				AimedDepartureTime:    "2026-08-24T15:33:00Z",
				ExpectedDepartureTime: "2026-08-24T15:33:00Z",
			},
		},
	}

	row, ok := buildDepartureRow(visit, ScheduleWeekday)
	if !ok {
		t.Fatal("expected a row")
	}
	if row.StopID != "70011" {
		t.Errorf("expected stop 70011, got %s", row.StopID)
	}
	if row.Station != "san_francisco" {
		t.Errorf("expected station san_francisco, got %s", row.Station)
	}
}

// TestFinalizeSkipsWithoutRecentPoll guards the outage case: if the poller has
// not succeeded recently, every row looks stale and finalizing would freeze live
// trains at a stale prediction.
func TestFinalizeSkipsWithoutRecentPoll(t *testing.T) {
	tracker := NewDepartureTracker(NewKeyPool([]string{"key"}, 1, 5), time.Minute)

	// Zero lastSuccessfulPoll represents a tracker that has never polled.
	tracker.Finalize()

	tracker.mu.Lock()
	tracker.lastSuccessfulPoll = time.Now().Add(-time.Hour)
	tracker.mu.Unlock()

	// With DB nil these calls must not panic; the guard short-circuits first.
	tracker.Finalize()
}

// TestDepartureTrackerPoll exercises the full poll path against a mock 511
// server: URL construction, fetch, parse and row building. The holidays request
// deliberately fails so the graceful degradation to an unknown schedule type is
// covered too.
func TestDepartureTrackerPoll(t *testing.T) {
	var monitoringRequests int
	var receivedQuery url.Values

	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/transit/StopMonitoring") {
			monitoringRequests++
			receivedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(sampleStopMonitoringJSON))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockAPI.Close()

	originalBaseURL := apiBaseURL
	apiBaseURL = mockAPI.URL + "/"
	defer func() { apiBaseURL = originalBaseURL }()

	tracker := NewDepartureTracker(NewKeyPool([]string{"test-key"}, 10, 10), 2*time.Minute)
	tracker.Poll()

	if monitoringRequests != 1 {
		t.Fatalf("expected 1 StopMonitoring request, got %d", monitoringRequests)
	}
	if got := receivedQuery.Get("agency"); got != departureOperatorID {
		t.Errorf("expected agency %q, got %q", departureOperatorID, got)
	}
	if got := receivedQuery.Get("api_key"); got != "test-key" {
		t.Errorf("expected the api key to be forwarded, got %q", got)
	}
	if got := receivedQuery.Get("format"); got != "json" {
		t.Errorf("expected format json, got %q", got)
	}
	if receivedQuery.Get("stopCode") != "" {
		t.Error("expected an agency-wide query with no stopCode")
	}

	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	if tracker.lastSuccessfulPoll.IsZero() {
		t.Error("expected a successful poll to be recorded")
	}
}

func TestDepartureFilterWhere(t *testing.T) {
	tests := []struct {
		name       string
		filter     DepartureFilter
		wantClause string
		wantArgs   int
	}{
		{"empty", DepartureFilter{}, "", 0},
		{
			name:       "single field",
			filter:     DepartureFilter{Station: "san_francisco"},
			wantClause: " WHERE station = $1",
			wantArgs:   1,
		},
		{
			name:       "date range",
			filter:     DepartureFilter{From: "2026-01-01", To: "2026-02-01"},
			wantClause: " WHERE service_date >= $1 AND service_date <= $2",
			wantArgs:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := tt.filter.where()
			if clause != tt.wantClause {
				t.Errorf("expected clause %q, got %q", tt.wantClause, clause)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("expected %d args, got %d", tt.wantArgs, len(args))
			}
		})
	}
}

func TestFormatDelay(t *testing.T) {
	tests := []struct {
		name    string
		seconds *int
		want    string
		class   string
	}{
		{"unknown", nil, "—", ""},
		{"on time", intPtr(0), "on time", ""},
		{"seconds late", intPtr(45), "+45s", ""},
		{"beyond threshold", intPtr(372), "+6m 12s", "late"},
		{"early", intPtr(-90), "-1m 30s", "early"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDelay(tt.seconds); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
			if got := delayClass(tt.seconds); got != tt.class {
				t.Errorf("expected class %q, got %q", tt.class, got)
			}
		})
	}
}

func TestBuildDeparturesPage(t *testing.T) {
	tests := []struct {
		name           string
		total          int
		page           int
		wantTotalPages int
		wantPrev       bool
		wantNext       bool
	}{
		{"empty", 0, 1, 1, false, false},
		{"single page", 20, 1, 1, false, false},
		{"first of many", 130, 1, 3, false, true},
		{"middle", 130, 2, 3, true, true},
		{"last", 130, 3, 3, true, false},
		{"exact multiple", 100, 2, 2, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := buildDeparturesPage(nil, DepartureFilter{}, tt.page, tt.total)
			if page.TotalPages != tt.wantTotalPages {
				t.Errorf("expected %d pages, got %d", tt.wantTotalPages, page.TotalPages)
			}
			if page.HasPrev() != tt.wantPrev {
				t.Errorf("expected HasPrev=%v", tt.wantPrev)
			}
			if page.HasNext() != tt.wantNext {
				t.Errorf("expected HasNext=%v", tt.wantNext)
			}
		})
	}
}

func TestDeparturesLink(t *testing.T) {
	filter := DepartureFilter{Station: "san_francisco", TrainNumber: "401"}

	if got := departuresLink(DepartureFilter{}, 1); got != "/admin/departures" {
		t.Errorf("expected bare path, got %q", got)
	}
	got := departuresLink(filter, 3)
	want := "/admin/departures?page=3&station=san_francisco&train_number=401"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestDeparturesHandlersRejectBadInput checks parameters are validated before
// reaching PostgreSQL, where a malformed date would fail as a bad cast.
func TestDeparturesHandlersRejectBadInput(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		target  string
		want    int
	}{
		{"bad service date", departuresListHandler, http.MethodGet, "/admin/departures?service_date=24-08-2026", http.StatusBadRequest},
		{"bad from date", departuresExportHandler, http.MethodGet, "/admin/departures/export?from=yesterday", http.StatusBadRequest},
		{"bad page", departuresListHandler, http.MethodGet, "/admin/departures?page=0", http.StatusBadRequest},
		{"non numeric page", departuresListHandler, http.MethodGet, "/admin/departures?page=abc", http.StatusBadRequest},
		{"missing id", departuresDetailHandler, http.MethodGet, "/admin/departures/detail", http.StatusBadRequest},
		{"delete rejects GET", departuresDeleteHandler, http.MethodGet, "/admin/departures/delete?id=1", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tt.handler(recorder, httptest.NewRequest(tt.method, tt.target, nil))
			if recorder.Code != tt.want {
				t.Errorf("expected status %d, got %d", tt.want, recorder.Code)
			}
		})
	}
}

// TestDeparturesListHandlerRendersWithoutDatabase confirms the admin page
// degrades to an empty state when no database is configured.
func TestDeparturesListHandlerRendersWithoutDatabase(t *testing.T) {
	recorder := httptest.NewRecorder()
	departuresListHandler(recorder, httptest.NewRequest(http.MethodGet, "/admin/departures", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "No departures recorded yet") {
		t.Error("expected the empty state to be rendered")
	}
}

// TestStoreFunctionsAreNoOpWithoutDatabase mirrors the existing service alert
// behaviour: every store call must be safe when DB is nil.
func TestStoreFunctionsAreNoOpWithoutDatabase(t *testing.T) {
	if err := UpsertTrainDeparture(&TrainDepartureRow{}); err != nil {
		t.Errorf("UpsertTrainDeparture returned %v", err)
	}
	if err := UpsertTrainDeparture(nil); err != nil {
		t.Errorf("UpsertTrainDeparture(nil) returned %v", err)
	}
	if err := DeleteTrainDeparture(1); err != nil {
		t.Errorf("DeleteTrainDeparture returned %v", err)
	}
	if _, err := CountTrainDepartures(DepartureFilter{}); err != nil {
		t.Errorf("CountTrainDepartures returned %v", err)
	}
	if _, err := ListTrainDepartures(DepartureFilter{}, 10, 0); err != nil {
		t.Errorf("ListTrainDepartures returned %v", err)
	}
	if _, err := GetTrainDepartureByID(1); err != nil {
		t.Errorf("GetTrainDepartureByID returned %v", err)
	}
	if _, err := FinalizeStaleDepartures(time.Minute); err != nil {
		t.Errorf("FinalizeStaleDepartures returned %v", err)
	}
	if err := StreamTrainDepartures(DepartureFilter{}, func(TrainDepartureRow) error { return nil }); err != nil {
		t.Errorf("StreamTrainDepartures returned %v", err)
	}
}

func intPtr(value int) *int { return &value }

// TestParseStopMonitoringTolerantScalars covers the scalar encodings observed
// from the live 511 feed. A decode failure here would be severe: the feed is
// fetched as one agency-wide document, so a single odd field would discard
// every train in the response.
//
// The empty-string boolean is a real regression: 511 sends "Monitored": "",
// which aborted the entire poll with
// `invalid boolean string "": strconv.ParseBool: parsing "": invalid syntax`.
func TestParseStopMonitoringTolerantScalars(t *testing.T) {
	tests := []struct {
		name        string
		call        string
		journey     string
		wantTrain   string
		wantStop    string
		wantAtStop  bool
		wantMonitor bool
	}{
		{
			name:      "empty string booleans",
			journey:   `"Monitored":"","VehicleJourneyName":"401"`,
			call:      `"StopPointRef":"70021","VehicleAtStop":""`,
			wantTrain: "401",
			wantStop:  "70021",
		},
		{
			name:      "whitespace only boolean",
			journey:   `"Monitored":"  ","VehicleJourneyName":"401"`,
			call:      `"StopPointRef":"70021","VehicleAtStop":" "`,
			wantTrain: "401",
			wantStop:  "70021",
		},
		{
			name:      "unrecognised boolean word",
			journey:   `"Monitored":"maybe","VehicleJourneyName":"401"`,
			call:      `"StopPointRef":"70021","VehicleAtStop":"Y"`,
			wantTrain: "401",
			wantStop:  "70021",
		},
		{
			name:        "numeric boolean strings",
			journey:     `"Monitored":"1","VehicleJourneyName":"401"`,
			call:        `"StopPointRef":"70021","VehicleAtStop":"0"`,
			wantTrain:   "401",
			wantMonitor: true,
			wantStop:    "70021",
		},
		{
			name:      "numeric stop and train identifiers",
			journey:   `"VehicleJourneyName":401`,
			call:      `"StopPointRef":70021`,
			wantTrain: "401",
			wantStop:  "70021",
		},
		{
			name:      "object where a string is expected",
			journey:   `"VehicleJourneyName":"401","LineRef":{"ref":"Local"}`,
			call:      `"StopPointRef":"70021"`,
			wantTrain: "401",
			wantStop:  "70021",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"ServiceDelivery":{"StopMonitoringDelivery":{"MonitoredStopVisit":[
				{"MonitoringRef":"70021","MonitoredVehicleJourney":{` + tt.journey +
				`,"MonitoredCall":{` + tt.call + `}}}]}}}`

			response, err := parseStopMonitoringJSON([]byte(payload))
			if err != nil {
				t.Fatalf("parsing must not fail, got: %v", err)
			}
			visits := response.Visits()
			if len(visits) != 1 {
				t.Fatalf("expected 1 visit, got %d", len(visits))
			}
			journey := visits[0].MonitoredVehicleJourney
			if got := journey.TrainNumber(); got != tt.wantTrain {
				t.Errorf("expected train %q, got %q", tt.wantTrain, got)
			}
			if got := string(journey.MonitoredCall.StopPointRef); got != tt.wantStop {
				t.Errorf("expected stop %q, got %q", tt.wantStop, got)
			}
			if bool(journey.Monitored) != tt.wantMonitor {
				t.Errorf("expected Monitored %v, got %v", tt.wantMonitor, bool(journey.Monitored))
			}
			if bool(journey.MonitoredCall.VehicleAtStop) != tt.wantAtStop {
				t.Errorf("expected VehicleAtStop %v", tt.wantAtStop)
			}
		})
	}
}
