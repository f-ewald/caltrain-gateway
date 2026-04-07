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
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS support_requests (
			id         SERIAL PRIMARY KEY,
			name       TEXT        NOT NULL,
			app        TEXT        NOT NULL,
			email      TEXT        NOT NULL,
			type       TEXT        NOT NULL,
			message    TEXT        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
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
