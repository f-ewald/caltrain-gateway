package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	// Embeds the IANA timezone database in the binary. The runtime image is a
	// bare alpine, which ships no tzdata, so without this every
	// America/Los_Angeles lookup would silently fall back to UTC in production
	// and corrupt service dates and day-of-week values.
	_ "time/tzdata"

	caltraingateway "caltrain-gateway/internal/app/caltrain-gateway"
)

const (
	baseAPIURL = "http://api.511.org/"
	operatorID = "CT"

	// bartOperatorID is BART's 511 agency code. BART gets real timetable,
	// stops and service-alerts data (unlike other agencies, which are only
	// reachable via the generic /agency/{operator}/... routing and 404 for
	// anything requiring locally-loaded data). Real-time departure tracking
	// stays Caltrain-only.
	bartOperatorID = "BA"

	// serviceAlertsRefreshInterval is shared by every agency's alerts. It was
	// 5 minutes when only Caltrain's alerts were fetched (12 requests/hour);
	// doubled to cover BART too would have used 24/hour, so it is widened to
	// keep the combined footprint at its original 12/hour regardless of how
	// many agencies are polled.
	serviceAlertsRefreshInterval = 10 * time.Minute
)

func main() {
	apiKeyPool := caltraingateway.NewKeyPool(
		caltraingateway.LoadAPIKeysFromEnv(),
		1, // 1 request per second
		5, // burst size of 5
	)

	if len(apiKeyPool.Keys) == 0 {
		log.Fatal("No API keys found in environment variables FIVEONEONE_API_KEY_1, FIVEONEONE_API_KEY_2, etc.")
	}

	// Get an API key for loading data
	apiKey, ok := apiKeyPool.GetAvailableKey()
	if !ok {
		log.Fatal("No available API key to load timetables")
	}

	// Load the secret from environment variable
	secret := caltraingateway.LoadSecretFromEnv()

	// Initialize database (optional). Must run before any code path that
	// persists data, so the initial service alerts fetch can write to DB.
	dbURL := caltraingateway.LoadDatabaseURLFromEnv()
	if err := caltraingateway.InitDB(dbURL); err != nil {
		// DATABASE_URL was supplied but unusable. Say so explicitly, otherwise
		// the only clue is a later "no database" message that reads as though
		// none had been configured at all.
		log.Printf("Warning: Failed to initialize database: %v", err)
		log.Println("Warning: DATABASE_URL is set but the connection failed. Continuing WITHOUT a database: " +
			"nothing will be persisted and departure tracking stays disabled.")
	}
	defer caltraingateway.CloseDB()

	// Load Caltrain's lines and timetables (all lines, matching existing behavior).
	tc, err := loadAllTimetables(apiKey.Value, operatorID, false)
	if err != nil {
		log.Printf("Warning: Failed to load timetables: %v", err)
	} else {
		caltraingateway.SetTimetableCollection(operatorID, tc)
		caltraingateway.SetAPIConnected(true)
		log.Println("Timetables loaded successfully")
	}

	// Load BART's lines and timetables, monitored lines only (excludes bus
	// bridges and other non-real-time-tracked variants). Independent of, and
	// non-fatal relative to, Caltrain's load above.
	if bartTC, err := loadAllTimetables(apiKey.Value, bartOperatorID, true); err != nil {
		log.Printf("Warning: Failed to load BART timetables: %v", err)
	} else {
		caltraingateway.SetTimetableCollection(bartOperatorID, bartTC)
		log.Println("BART timetables loaded successfully")
	}

	// Load the known-agency directory. This is only used to make the
	// /agency/{operator}/... routes' error messages accurate and to populate
	// the admin agency picker; a failure here is non-fatal and never affects
	// Caltrain (CT), which those routes check by direct comparison.
	if agencyList, err := loadAgencies(apiKey.Value); err != nil {
		log.Printf("Warning: Failed to load transit agency directory: %v", err)
	} else {
		caltraingateway.SetAgencies(agencyList)
		log.Printf("Loaded %d transit agencies", len(agencyList))
	}

	// Load service alerts. BART's fetch is best-effort: if it fails, Caltrain's
	// alerts still get served rather than blocking on the newer, secondary agency.
	sa, err := loadAllServiceAlerts(apiKey.Value)
	if err != nil {
		log.Printf("Warning: Failed to load service alerts: %v", err)
	} else {
		caltraingateway.SetServiceAlerts(sa)
		log.Println("Service alerts loaded successfully")
	}

	// Periodically refresh service alerts for every loaded agency.
	go func() {
		ticker := time.NewTicker(serviceAlertsRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			key, ok := apiKeyPool.GetAvailableKey()
			if !ok {
				log.Println("Warning: No available API key to refresh service alerts")
				continue
			}
			sa, err := loadAllServiceAlerts(key.Value)
			if err != nil {
				log.Printf("Warning: Failed to refresh service alerts: %v", err)
				caltraingateway.SetAPIConnected(false)
				continue
			}
			caltraingateway.SetServiceAlerts(sa)
			caltraingateway.SetAPIConnected(true)
			log.Println("Service alerts refreshed successfully")
		}
	}()

	dbUsername, dbPassword := caltraingateway.ParseDatabaseCredentials(dbURL)
	mux := caltraingateway.SetupRoutes(apiKeyPool, secret, dbUsername, dbPassword)

	startDepartureTracking(apiKeyPool)
	startTimetableRefresh(apiKeyPool)
	startBARTTimetableRefresh(apiKeyPool)

	listener, err := net.Listen("tcp", ":"+caltraingateway.LoadPortFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Caltrain Proxy running on %s", listener.Addr().String())
	log.Fatal(http.Serve(listener, mux))
}

// startDepartureTracking starts the SIRI StopMonitoring poller that records
// actual departure times, unless disabled by configuration.
func startDepartureTracking(apiKeyPool *caltraingateway.KeyPool) {
	if !caltraingateway.LoadDepartureTrackingEnabledFromEnv() {
		log.Println("Departure tracking disabled by DEPARTURE_TRACKING_ENABLED")
		return
	}
	interval := caltraingateway.LoadDeparturePollIntervalFromEnv()
	caltraingateway.NewDepartureTracker(apiKeyPool, interval).Start()
}

// loadAllTimetables loads lines for agencyOperatorID from the API and then
// loads timetables for each line. When monitoredOnly is true, only lines 511
// flags as Monitored are loaded (e.g. BART's bus-bridge/shuttle lines are
// excluded); Caltrain keeps loading every line, unfiltered, matching its
// original behavior.
func loadAllTimetables(apiKey, agencyOperatorID string, monitoredOnly bool) (*caltraingateway.TimetableCollection, error) {
	// Build URL for lines
	u, err := url.Parse(baseAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base API URL: %w", err)
	}

	u.Path = "transit/lines"
	q := u.Query()
	q.Set("operator_id", agencyOperatorID)
	q.Set("format", "json")
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()

	log.Printf("Loading lines for %s from API ...", agencyOperatorID)
	lines, err := caltraingateway.LoadLinesFromURL(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load lines: %w", err)
	}
	if monitoredOnly {
		lines = caltraingateway.GetMonitoredLines(lines)
	}
	log.Printf("Loaded %d lines for %s", len(lines), agencyOperatorID)

	// Create timetable collection
	tc := caltraingateway.NewTimetableCollection()

	// Load timetable for each line
	for _, line := range lines {
		// Sleep for two seconds to respect rate limiting
		time.Sleep(2 * time.Second)

		timetableURL, err := url.Parse(baseAPIURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse base API URL: %w", err)
		}
		timetableURL.Path = "transit/timetable"
		q := timetableURL.Query()
		q.Set("operator_id", agencyOperatorID)
		q.Set("format", "json")
		q.Set("line_id", line.ID)
		q.Set("api_key", apiKey)
		timetableURL.RawQuery = q.Encode()

		log.Printf("Loading timetable for line: %s", line.ID)
		tt, err := caltraingateway.LoadTimetableFromURL(timetableURL.String())
		if err != nil {
			// Fail the whole load rather than returning a partial collection.
			// A collection missing lines would drop real departures and change
			// the schedule version, pushing every client to re-download a
			// truncated timetable.
			return nil, fmt.Errorf("failed to load timetable for line %s: %w", line.ID, err)
		}
		tc.AddTimetable(tt)
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("no lines returned by the API")
	}

	return tc, nil
}

// startTimetableRefresh starts the nightly Caltrain timetable refresh so the
// served schedule, and the version clients validate against, stay current.
func startTimetableRefresh(apiKeyPool *caltraingateway.KeyPool) {
	hour := caltraingateway.LoadTimetableRefreshHourFromEnv()
	loader := func(apiKey string) (*caltraingateway.TimetableCollection, error) {
		return loadAllTimetables(apiKey, operatorID, false)
	}
	caltraingateway.NewScheduleRefresher(apiKeyPool, operatorID, loader, hour, 0).Start()
}

// bartTimetableRefreshMinInterval bounds how often BART's timetable is
// refetched. BART has roughly 3x Caltrain's lines and a correspondingly larger
// payload, and its schedule changes less often, so it refreshes weekly rather
// than nightly.
const bartTimetableRefreshMinInterval = 7 * 24 * time.Hour

// startBARTTimetableRefresh starts BART's timetable refresh on its own,
// less-frequent schedule (see bartTimetableRefreshMinInterval), independent of
// Caltrain's nightly refresh.
func startBARTTimetableRefresh(apiKeyPool *caltraingateway.KeyPool) {
	hour := caltraingateway.LoadTimetableRefreshHourFromEnv()
	loader := func(apiKey string) (*caltraingateway.TimetableCollection, error) {
		return loadAllTimetables(apiKey, bartOperatorID, true)
	}
	caltraingateway.NewScheduleRefresher(apiKeyPool, bartOperatorID, loader, hour, bartTimetableRefreshMinInterval).Start()
}

// loadServiceAlerts fetches service alerts for one agency from the 511 API.
func loadServiceAlerts(apiKey, agencyID string) (*caltraingateway.ServiceAlertsResponse, error) {
	u, err := url.Parse(baseAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base API URL: %w", err)
	}

	u.Path = "transit/servicealerts"
	q := u.Query()
	q.Set("agency", agencyID)
	q.Set("format", "json")
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()

	log.Printf("Loading service alerts for %s from API ...", agencyID)
	sa, err := caltraingateway.LoadServiceAlertsFromURL(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load service alerts: %w", err)
	}
	return sa, nil
}

// loadAllServiceAlerts fetches and merges service alerts for every agency this
// service tracks (Caltrain and BART). Caltrain's fetch is load-bearing: an
// error there fails the whole call, matching prior behavior. BART's is
// best-effort: a failure there is logged and Caltrain's alerts are still
// returned, since BART is the newer, secondary agency here.
func loadAllServiceAlerts(apiKey string) (*caltraingateway.ServiceAlertsResponse, error) {
	sa, err := loadServiceAlerts(apiKey, operatorID)
	if err != nil {
		return nil, err
	}

	bartSA, err := loadServiceAlerts(apiKey, bartOperatorID)
	if err != nil {
		log.Printf("Warning: Failed to load BART service alerts: %v", err)
		return sa, nil
	}
	return mergeServiceAlerts(sa, bartSA), nil
}

// mergeServiceAlerts concatenates two service-alerts responses' entities under
// one header, so the existing ?agency= filter (serviceAlertsHandler) can find
// either agency's alerts in the combined result.
func mergeServiceAlerts(a, b *caltraingateway.ServiceAlertsResponse) *caltraingateway.ServiceAlertsResponse {
	return &caltraingateway.ServiceAlertsResponse{
		Header: a.Header,
		Entity: append(append([]caltraingateway.ServiceAlertEntity{}, a.Entity...), b.Entity...),
	}
}

// loadAgencies fetches the list of transit operators 511 knows about, used to
// make the /agency/{operator}/... routes' error messages and the admin
// agency picker accurate. It is independent of, and does not gate, Caltrain
// (CT) support.
func loadAgencies(apiKey string) ([]caltraingateway.Agency, error) {
	u, err := url.Parse(baseAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base API URL: %w", err)
	}

	u.Path = "transit/gtfsoperators"
	q := u.Query()
	q.Set("format", "json")
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()

	log.Println("Loading transit agencies from API ...")
	return caltraingateway.LoadAgenciesFromURL(u.String())
}
