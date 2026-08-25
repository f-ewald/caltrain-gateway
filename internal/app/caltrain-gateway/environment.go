package caltraingateway

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultPort is the TCP port the server listens on when PORT is unset.
	defaultPort = "8080"

	// defaultDeparturePollInterval keeps the poller at 30 requests/hour, leaving
	// headroom within 511's 60 requests/hour per-key quota for proxy traffic.
	defaultDeparturePollInterval = 2 * time.Minute

	// minDeparturePollInterval guards against a configured value that would
	// exhaust the hourly quota on its own.
	minDeparturePollInterval = time.Minute
)

// LoadPortFromEnv loads the TCP listen port from the PORT environment variable.
// Returns defaultPort when PORT is unset, or when it is not a valid port number,
// so a typo degrades to a working server rather than a startup failure.
func LoadPortFromEnv() string {
	raw := strings.TrimSpace(os.Getenv("PORT"))
	if raw == "" {
		return defaultPort
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		log.Printf("Invalid PORT value %q, using %s", raw, defaultPort)
		return defaultPort
	}
	return raw
}

// LoadAPIKeysFromEnv loads API keys from environment variables named FIVEONEONE_API_KEY_1, FIVEONEONE_API_KEY_2, etc.
func LoadAPIKeysFromEnv() []string {
	var keys []string
	for i := 1; ; i++ {
		key := os.Getenv("FIVEONEONE_API_KEY_" + strconv.Itoa(i))
		if key == "" {
			break
		}
		keys = append(keys, key)
	}
	log.Printf("Loaded %d API keys from environment variables.", len(keys))
	return keys
}

// LoadDatabaseURLFromEnv loads the PostgreSQL connection string from the DATABASE_URL environment variable.
func LoadDatabaseURLFromEnv() string {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Println("DATABASE_URL environment variable is not set. Running without database.")
	}
	return dbURL
}

// ParseDatabaseCredentials extracts the username and password from a PostgreSQL connection URL.
// Returns empty strings if the URL cannot be parsed or has no credentials.
func ParseDatabaseCredentials(dbURL string) (username, password string) {
	if dbURL == "" {
		return "", ""
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		return "", ""
	}
	if u.User == nil {
		return "", ""
	}
	password, _ = u.User.Password()
	return u.User.Username(), password
}

// LoadSecretFromEnv loads the Caltrain Gateway secret from the CALTRAIN_GATEWAY_SECRET environment variable.
func LoadSecretFromEnv() string {
	secret := os.Getenv("CALTRAIN_GATEWAY_SECRET")
	if secret == "" {
		log.Println("CALTRAIN_GATEWAY_SECRET environment variable is not set. This is not recommended for production environments.")
	}
	return secret
}

// LoadDepartureTrackingEnabledFromEnv reports whether the departure tracker
// should run, from DEPARTURE_TRACKING_ENABLED. Defaults to true; tracking is
// additionally skipped at startup when no database is configured.
func LoadDepartureTrackingEnabledFromEnv() bool {
	raw := os.Getenv("DEPARTURE_TRACKING_ENABLED")
	if raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("Invalid DEPARTURE_TRACKING_ENABLED value %q, defaulting to enabled", raw)
		return true
	}
	return enabled
}

// LoadDeparturePollIntervalFromEnv loads the departure poll interval from
// DEPARTURE_POLL_INTERVAL as a Go duration (for example "2m"). Values below
// minDeparturePollInterval are raised to that floor so a single misconfiguration
// cannot burn the whole hourly API quota.
func LoadDeparturePollIntervalFromEnv() time.Duration {
	raw := os.Getenv("DEPARTURE_POLL_INTERVAL")
	if raw == "" {
		return defaultDeparturePollInterval
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("Invalid DEPARTURE_POLL_INTERVAL value %q, using %s", raw, defaultDeparturePollInterval)
		return defaultDeparturePollInterval
	}
	if interval < minDeparturePollInterval {
		log.Printf("DEPARTURE_POLL_INTERVAL %s is below the %s floor, using the floor", interval, minDeparturePollInterval)
		return minDeparturePollInterval
	}
	return interval
}
