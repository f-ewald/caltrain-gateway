package caltraingateway

import (
	"database/sql"
	"log"

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
