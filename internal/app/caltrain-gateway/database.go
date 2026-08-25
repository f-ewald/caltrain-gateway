package caltraingateway

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// DB holds the database connection pool. It is nil when no database is configured.
var DB *sql.DB

// InitDB opens a PostgreSQL connection, verifies it with a ping, and runs
// schema migrations. If connStr is empty the function returns nil and the
// application continues without a database.
func InitDB(connStr string) error {
	if connStr == "" {
		log.Println("DATABASE_URL not set, running without database")
		return nil
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return err
	}

	if err := migrateSchema(db); err != nil {
		db.Close()
		return err
	}

	DB = db
	log.Println("Database connection established")
	return nil
}

// CloseDB closes the database connection if one is open.
func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}

// migrateSchema creates required tables if they do not already exist.
func migrateSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS support_requests (
			id         SERIAL PRIMARY KEY,
			name       TEXT        NOT NULL,
			app        TEXT        NOT NULL,
			email      TEXT        NOT NULL,
			type       TEXT        NOT NULL,
			message    TEXT        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS service_alerts (
			id               SERIAL      PRIMARY KEY,
			entity_id        TEXT        NOT NULL,
			content_hash     TEXT        NOT NULL,
			agency_id        TEXT,
			cause            INT,
			effect           INT,
			severity_level   INT,
			header_text      TEXT,
			description_text TEXT,
			url              TEXT,
			feed_timestamp   BIGINT,
			first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (entity_id, content_hash)
		)
	`); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_service_alerts_last_seen_at ON service_alerts(last_seen_at)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_service_alerts_agency_id ON service_alerts(agency_id)`); err != nil {
		return err
	}

	return migrateDepartureSchema(db)
}

// migrateDepartureSchema creates the train_departures table and its indexes.
// One row represents one train calling at one stop on one operating day; the
// row converges as the poller observes it repeatedly.
func migrateDepartureSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS train_departures (
			id                      BIGSERIAL   PRIMARY KEY,
			service_date            DATE        NOT NULL,
			train_number            TEXT        NOT NULL,
			stop_id                 TEXT        NOT NULL,
			station                 TEXT,
			direction               TEXT,
			line                    TEXT,
			day_of_week             SMALLINT    NOT NULL,
			schedule_type           TEXT,
			scheduled_arrival       TIMESTAMPTZ,
			expected_arrival        TIMESTAMPTZ,
			scheduled_departure     TIMESTAMPTZ,
			expected_departure      TIMESTAMPTZ,
			arrival_delay_seconds   INT,
			departure_delay_seconds INT,
			dwell_seconds           INT,
			departure_source        TEXT,
			vehicle_at_stop         BOOLEAN     NOT NULL DEFAULT FALSE,
			monitored               BOOLEAN     NOT NULL DEFAULT FALSE,
			vehicle_ref             TEXT,
			observation_count       INT         NOT NULL DEFAULT 1,
			first_seen_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			finalized_at            TIMESTAMPTZ,
			UNIQUE (service_date, train_number, stop_id)
		)
	`); err != nil {
		return err
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_train_departures_last_seen_at ON train_departures(last_seen_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_train_departures_service_date ON train_departures(service_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_train_departures_station_dow ON train_departures(station, day_of_week)`,
		`CREATE INDEX IF NOT EXISTS idx_train_departures_train_number ON train_departures(train_number)`,
		`CREATE INDEX IF NOT EXISTS idx_train_departures_pending ON train_departures(last_seen_at) WHERE finalized_at IS NULL`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// nullable converts an empty string to a nil any so it is stored as SQL NULL
// rather than an empty string.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullStringValue returns the contained string or "" when the sql.NullString is invalid.
func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// nullInt64Value returns the contained int64 or 0 when the sql.NullInt64 is invalid.
func nullInt64Value(ns sql.NullInt64) int64 {
	if ns.Valid {
		return ns.Int64
	}
	return 0
}

// nullInt32Value returns the contained int32 as int or 0 when the sql.NullInt32 is invalid.
func nullInt32Value(ns sql.NullInt32) int {
	if ns.Valid {
		return int(ns.Int32)
	}
	return 0
}

// SupportRequestRow represents a support request row from the database.
type SupportRequestRow struct {
	ID        int
	Name      string
	App       string
	Email     string
	Type      string
	Message   string
	CreatedAt time.Time
}

// GetAllSupportRequests returns all support requests ordered by creation time descending.
// Returns nil, nil if the database is not configured.
func GetAllSupportRequests() ([]SupportRequestRow, error) {
	if DB == nil {
		return nil, nil
	}
	rows, err := DB.Query(`SELECT id, name, app, email, type, message, created_at FROM support_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SupportRequestRow
	for rows.Next() {
		var r SupportRequestRow
		if err := rows.Scan(&r.ID, &r.Name, &r.App, &r.Email, &r.Type, &r.Message, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetSupportRequestByID returns a single support request by ID.
// Returns nil, nil if the database is not configured or the row is not found.
func GetSupportRequestByID(id int) (*SupportRequestRow, error) {
	if DB == nil {
		return nil, nil
	}
	var r SupportRequestRow
	err := DB.QueryRow(`SELECT id, name, app, email, type, message, created_at FROM support_requests WHERE id = $1`, id).
		Scan(&r.ID, &r.Name, &r.App, &r.Email, &r.Type, &r.Message, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteSupportRequest removes a support request by ID.
// If no database is configured (DB is nil) the call is a no-op.
func DeleteSupportRequest(id int) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`DELETE FROM support_requests WHERE id = $1`, id)
	return err
}

// InsertSupportRequest persists a support request to the database.
// If no database is configured (DB is nil) the call is a no-op.
func InsertSupportRequest(req *supportRequest) error {
	if DB == nil {
		return nil
	}

	_, err := DB.Exec(
		`INSERT INTO support_requests (name, app, email, type, message) VALUES ($1, $2, $3, $4, $5)`,
		req.Name, req.App, req.Email, req.Type, req.Message,
	)
	return err
}

// ServiceAlertRow represents a single persisted service alert variant.
type ServiceAlertRow struct {
	ID              int
	EntityID        string
	ContentHash     string
	AgencyID        string
	Cause           int
	Effect          int
	SeverityLevel   int
	HeaderText      string
	DescriptionText string
	URL             string
	FeedTimestamp   int64
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
}

const serviceAlertColumns = `id, entity_id, content_hash, agency_id, cause, effect, severity_level, header_text, description_text, url, feed_timestamp, first_seen_at, last_seen_at`

// scanServiceAlertRow scans a row from a SELECT serviceAlertColumns query.
func scanServiceAlertRow(scanner interface {
	Scan(dest ...any) error
}) (ServiceAlertRow, error) {
	var (
		r        ServiceAlertRow
		agency   sql.NullString
		cause    sql.NullInt32
		effect   sql.NullInt32
		severity sql.NullInt32
		header   sql.NullString
		descr    sql.NullString
		urlStr   sql.NullString
		feedTs   sql.NullInt64
	)
	if err := scanner.Scan(
		&r.ID, &r.EntityID, &r.ContentHash, &agency, &cause, &effect, &severity,
		&header, &descr, &urlStr, &feedTs, &r.FirstSeenAt, &r.LastSeenAt,
	); err != nil {
		return r, err
	}
	r.AgencyID = nullStringValue(agency)
	r.Cause = nullInt32Value(cause)
	r.Effect = nullInt32Value(effect)
	r.SeverityLevel = nullInt32Value(severity)
	r.HeaderText = nullStringValue(header)
	r.DescriptionText = nullStringValue(descr)
	r.URL = nullStringValue(urlStr)
	r.FeedTimestamp = nullInt64Value(feedTs)
	return r, nil
}

// UpsertServiceAlert inserts a service alert variant or refreshes its last_seen_at
// timestamp when an identical (entity_id, content_hash) pair already exists.
// It performs a read-modify-write so the SERIAL id sequence is not consumed on
// every refresh cycle for already-known alerts. If no database is configured
// (DB is nil) the call is a no-op.
func UpsertServiceAlert(entityID, contentHash, agencyID string,
	cause, effect, severity int, headerText, descriptionText, url string,
	feedTimestamp int64) error {
	if DB == nil {
		return nil
	}
	var existingID int
	err := DB.QueryRow(
		`SELECT id FROM service_alerts WHERE entity_id = $1 AND content_hash = $2`,
		entityID, contentHash,
	).Scan(&existingID)
	if err == nil {
		_, err = DB.Exec(`UPDATE service_alerts SET last_seen_at = NOW() WHERE id = $1`, existingID)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = DB.Exec(
		`INSERT INTO service_alerts (entity_id, content_hash, agency_id, cause, effect,
			severity_level, header_text, description_text, url, feed_timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (entity_id, content_hash) DO NOTHING`,
		entityID, contentHash, nullable(agencyID), cause, effect, severity,
		nullable(headerText), nullable(descriptionText), nullable(url), feedTimestamp,
	)
	return err
}

// GetAllServiceAlerts returns every persisted service alert ordered by most-recently
// seen first. Returns nil, nil when the database is not configured.
func GetAllServiceAlerts() ([]ServiceAlertRow, error) {
	if DB == nil {
		return nil, nil
	}
	rows, err := DB.Query(`SELECT ` + serviceAlertColumns + ` FROM service_alerts ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ServiceAlertRow
	for rows.Next() {
		r, err := scanServiceAlertRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetServiceAlertByID returns a single persisted alert by primary key.
// Returns nil, nil when the database is not configured or the row is not found.
func GetServiceAlertByID(id int) (*ServiceAlertRow, error) {
	if DB == nil {
		return nil, nil
	}
	row := DB.QueryRow(`SELECT `+serviceAlertColumns+` FROM service_alerts WHERE id = $1`, id)
	r, err := scanServiceAlertRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteServiceAlert removes a persisted service alert by primary key.
// If no database is configured (DB is nil) the call is a no-op.
func DeleteServiceAlert(id int) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`DELETE FROM service_alerts WHERE id = $1`, id)
	return err
}

// StreamAllServiceAlerts iterates every persisted service alert in last_seen_at
// descending order, invoking fn for each row. Iteration stops on the first
// error from fn or the underlying scan. No-op (returns nil) when DB is nil.
func StreamAllServiceAlerts(fn func(ServiceAlertRow) error) error {
	if DB == nil {
		return nil
	}
	rows, err := DB.Query(`SELECT ` + serviceAlertColumns + ` FROM service_alerts ORDER BY last_seen_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		r, err := scanServiceAlertRow(rows)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}
