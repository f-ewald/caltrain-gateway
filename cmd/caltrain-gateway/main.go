package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	caltraingateway "caltrain-gateway/internal/app/caltrain-gateway"
)

const (
	baseAPIURL = "http://api.511.org/"
	operatorID = "CT"
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

	// Load all lines and timetables
	tc, err := loadAllTimetables(apiKey.Value)
	if err != nil {
		log.Printf("Warning: Failed to load timetables: %v", err)
	} else {
		caltraingateway.SetTimetableCollection(tc)
		caltraingateway.SetAPIConnected(true)
		log.Println("Timetables loaded successfully")
	}

	// Load service alerts
	sa, err := loadServiceAlerts(apiKey.Value)
	if err != nil {
		log.Printf("Warning: Failed to load service alerts: %v", err)
	} else {
		caltraingateway.SetServiceAlerts(sa)
		log.Println("Service alerts loaded successfully")
	}

	// Periodically refresh service alerts
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			key, ok := apiKeyPool.GetAvailableKey()
			if !ok {
				log.Println("Warning: No available API key to refresh service alerts")
				continue
			}
			sa, err := loadServiceAlerts(key.Value)
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

	// Load the secret from environment variable
	secret := caltraingateway.LoadSecretFromEnv()

	// Initialize database (optional)
	dbURL := caltraingateway.LoadDatabaseURLFromEnv()
	if err := caltraingateway.InitDB(dbURL); err != nil {
		log.Printf("Warning: Failed to initialize database: %v", err)
	}
	defer caltraingateway.CloseDB()

	caltraingateway.SetupRoutes(apiKeyPool, secret)

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Caltrain Proxy running on %s", listener.Addr().String())
	log.Fatal(http.Serve(listener, nil))
}

// loadAllTimetables loads all lines from the API and then loads timetables for each line
func loadAllTimetables(apiKey string) (*caltraingateway.TimetableCollection, error) {
	// Build URL for lines
	u, err := url.Parse(baseAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base API URL: %w", err)
	}

	u.Path = "transit/lines"
	q := u.Query()
	q.Set("operator_id", operatorID)
	q.Set("format", "json")
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()

	log.Println("Loading lines from API ...")
	lines, err := caltraingateway.LoadLinesFromURL(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load lines: %w", err)
	}
	log.Printf("Loaded %d lines", len(lines))

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
		q.Set("operator_id", operatorID)
		q.Set("format", "json")
		q.Set("line_id", line.ID)
		q.Set("api_key", apiKey)
		timetableURL.RawQuery = q.Encode()

		log.Printf("Loading timetable for line: %s", line.ID)
		tt, err := caltraingateway.LoadTimetableFromURL(timetableURL.String())
		if err != nil {
			log.Printf("Warning: Failed to load timetable for line %s: %v", line.ID, err)
			continue
		}
		tc.AddTimetable(tt)
	}

	return tc, nil
}

// loadServiceAlerts fetches service alerts from the 511 API
func loadServiceAlerts(apiKey string) (*caltraingateway.ServiceAlertsResponse, error) {
	u, err := url.Parse(baseAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base API URL: %w", err)
	}

	u.Path = "transit/servicealerts"
	q := u.Query()
	q.Set("agency", operatorID)
	q.Set("format", "json")
	q.Set("api_key", apiKey)
	u.RawQuery = q.Encode()

	log.Println("Loading service alerts from API ...")
	sa, err := caltraingateway.LoadServiceAlertsFromURL(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load service alerts: %w", err)
	}
	return sa, nil
}
