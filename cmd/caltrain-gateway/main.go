package main

import (
	"fmt"
	"log"
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
		log.Println("Timetables loaded successfully")
	}

	// Load the secret from environment variable
	secret := caltraingateway.LoadSecretFromEnv()

	caltraingateway.SetupRoutes(apiKeyPool, secret)

	log.Println("Caltrain Proxy running on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
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
