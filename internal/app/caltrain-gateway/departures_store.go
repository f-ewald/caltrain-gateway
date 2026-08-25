package caltraingateway

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TrainDepartureRow is one train calling at one stop on one operating day.
//
// ExpectedDeparture holds the most recent departure time observed for the call.
// Once FinalizedAt is set the stop visit has left the feed, so that value is the
// inferred actual departure. DepartureSource records whether it came from the
// feed's authoritative ActualDepartureTime or from a prediction.
//
// JSON tags mirror the database columns so the export is directly loadable by
// analysis tooling.
type TrainDepartureRow struct {
	ID                    int64      `json:"id"`
	ServiceDate           time.Time  `json:"service_date"`
	TrainNumber           string     `json:"train_number"`
	StopID                string     `json:"stop_id"`
	Station               string     `json:"station"`
	Direction             string     `json:"direction"`
	Line                  string     `json:"line"`
	DayOfWeek             int        `json:"day_of_week"`
	ScheduleType          string     `json:"schedule_type"`
	ScheduledArrival      *time.Time `json:"scheduled_arrival"`
	ExpectedArrival       *time.Time `json:"expected_arrival"`
	ScheduledDeparture    *time.Time `json:"scheduled_departure"`
	ExpectedDeparture     *time.Time `json:"expected_departure"`
	ArrivalDelaySeconds   *int       `json:"arrival_delay_seconds"`
	DepartureDelaySeconds *int       `json:"departure_delay_seconds"`
	DwellSeconds          *int       `json:"dwell_seconds"`
	DepartureSource       string     `json:"departure_source"`
	VehicleAtStop         bool       `json:"vehicle_at_stop"`
	Monitored             bool       `json:"monitored"`
	VehicleRef            string     `json:"vehicle_ref"`
	ObservationCount      int        `json:"observation_count"`
	FirstSeenAt           time.Time  `json:"first_seen_at"`
	LastSeenAt            time.Time  `json:"last_seen_at"`
	FinalizedAt           *time.Time `json:"finalized_at"`
}

// WeekdayName returns the English name of the row's day of week.
func (r *TrainDepartureRow) WeekdayName() string {
	return time.Weekday(r.DayOfWeek).String()
}

// IsFinal reports whether the stop visit has left the feed, meaning the stored
// departure time is no longer expected to change.
func (r *TrainDepartureRow) IsFinal() bool {
	return r.FinalizedAt != nil
}

// DepartureFilter narrows a departure query. Empty fields are ignored.
type DepartureFilter struct {
	ServiceDate string // exact operating day, YYYY-MM-DD
	Station     string
	TrainNumber string
	From        string // inclusive lower bound on service_date, YYYY-MM-DD
	To          string // inclusive upper bound on service_date, YYYY-MM-DD
}

const trainDepartureColumns = `id, service_date, train_number, stop_id, station, direction, line,
	day_of_week, schedule_type, scheduled_arrival, expected_arrival, scheduled_departure,
	expected_departure, arrival_delay_seconds, departure_delay_seconds, dwell_seconds,
	departure_source, vehicle_at_stop, monitored, vehicle_ref, observation_count,
	first_seen_at, last_seen_at, finalized_at`

// where renders the filter as a SQL WHERE clause plus its ordered arguments.
// Returns an empty clause when no field is set.
func (f DepartureFilter) where() (string, []any) {
	var clauses []string
	var args []any

	add := func(expr, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expr, len(args)))
	}
	add("service_date = $%d", f.ServiceDate)
	add("station = $%d", f.Station)
	add("train_number = $%d", f.TrainNumber)
	add("service_date >= $%d", f.From)
	add("service_date <= $%d", f.To)

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// scanTrainDepartureRow scans a row selected with trainDepartureColumns.
func scanTrainDepartureRow(scanner interface {
	Scan(dest ...any) error
}) (TrainDepartureRow, error) {
	var (
		r              TrainDepartureRow
		station        sql.NullString
		direction      sql.NullString
		line           sql.NullString
		scheduleType   sql.NullString
		schedArrival   sql.NullTime
		expArrival     sql.NullTime
		schedDeparture sql.NullTime
		expDeparture   sql.NullTime
		arrivalDelay   sql.NullInt32
		departureDelay sql.NullInt32
		dwell          sql.NullInt32
		source         sql.NullString
		vehicleRef     sql.NullString
		finalizedAt    sql.NullTime
	)
	if err := scanner.Scan(
		&r.ID, &r.ServiceDate, &r.TrainNumber, &r.StopID, &station, &direction, &line,
		&r.DayOfWeek, &scheduleType, &schedArrival, &expArrival, &schedDeparture,
		&expDeparture, &arrivalDelay, &departureDelay, &dwell,
		&source, &r.VehicleAtStop, &r.Monitored, &vehicleRef, &r.ObservationCount,
		&r.FirstSeenAt, &r.LastSeenAt, &finalizedAt,
	); err != nil {
		return r, err
	}
	r.Station = nullStringValue(station)
	r.Direction = nullStringValue(direction)
	r.Line = nullStringValue(line)
	r.ScheduleType = nullStringValue(scheduleType)
	r.DepartureSource = nullStringValue(source)
	r.VehicleRef = nullStringValue(vehicleRef)
	r.ScheduledArrival = nullTimePointer(schedArrival)
	r.ExpectedArrival = nullTimePointer(expArrival)
	r.ScheduledDeparture = nullTimePointer(schedDeparture)
	r.ExpectedDeparture = nullTimePointer(expDeparture)
	r.FinalizedAt = nullTimePointer(finalizedAt)
	r.ArrivalDelaySeconds = nullInt32Pointer(arrivalDelay)
	r.DepartureDelaySeconds = nullInt32Pointer(departureDelay)
	r.DwellSeconds = nullInt32Pointer(dwell)
	return r, nil
}

// nullTimePointer converts a sql.NullTime into a pointer, nil when invalid.
func nullTimePointer(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	value := nt.Time
	return &value
}

// nullInt32Pointer converts a sql.NullInt32 into an *int, nil when invalid.
func nullInt32Pointer(ni sql.NullInt32) *int {
	if !ni.Valid {
		return nil
	}
	value := int(ni.Int32)
	return &value
}

// upsertTrainDepartureSQL converges repeated observations of the same stop call.
//
// Non-null incoming values win so the latest prediction is kept, while COALESCE
// prevents a later observation that omits a field from erasing a value already
// recorded. vehicle_at_stop is sticky because arrival is a one-way transition,
// and finalized_at is reset because a reappearing visit means the train has not
// actually departed yet.
const upsertTrainDepartureSQL = `
	INSERT INTO train_departures (
		service_date, train_number, stop_id, station, direction, line, day_of_week,
		schedule_type, scheduled_arrival, expected_arrival, scheduled_departure,
		expected_departure, arrival_delay_seconds, departure_delay_seconds,
		dwell_seconds, departure_source, vehicle_at_stop, monitored, vehicle_ref
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	ON CONFLICT (service_date, train_number, stop_id) DO UPDATE SET
		station                 = COALESCE(EXCLUDED.station, train_departures.station),
		direction               = COALESCE(EXCLUDED.direction, train_departures.direction),
		line                    = COALESCE(EXCLUDED.line, train_departures.line),
		schedule_type           = COALESCE(EXCLUDED.schedule_type, train_departures.schedule_type),
		scheduled_arrival       = COALESCE(EXCLUDED.scheduled_arrival, train_departures.scheduled_arrival),
		expected_arrival        = COALESCE(EXCLUDED.expected_arrival, train_departures.expected_arrival),
		scheduled_departure     = COALESCE(EXCLUDED.scheduled_departure, train_departures.scheduled_departure),
		expected_departure      = COALESCE(EXCLUDED.expected_departure, train_departures.expected_departure),
		arrival_delay_seconds   = COALESCE(EXCLUDED.arrival_delay_seconds, train_departures.arrival_delay_seconds),
		departure_delay_seconds = COALESCE(EXCLUDED.departure_delay_seconds, train_departures.departure_delay_seconds),
		dwell_seconds           = COALESCE(EXCLUDED.dwell_seconds, train_departures.dwell_seconds),
		departure_source        = COALESCE(EXCLUDED.departure_source, train_departures.departure_source),
		vehicle_at_stop         = train_departures.vehicle_at_stop OR EXCLUDED.vehicle_at_stop,
		monitored               = EXCLUDED.monitored,
		vehicle_ref             = COALESCE(EXCLUDED.vehicle_ref, train_departures.vehicle_ref),
		observation_count       = train_departures.observation_count + 1,
		last_seen_at            = NOW(),
		finalized_at            = NULL`

// UpsertTrainDeparture records one observation of a stop call, creating the row
// on first sight and converging it on every later observation.
// If no database is configured (DB is nil) the call is a no-op.
func UpsertTrainDeparture(row *TrainDepartureRow) error {
	if DB == nil || row == nil {
		return nil
	}
	_, err := DB.Exec(upsertTrainDepartureSQL,
		row.ServiceDate, row.TrainNumber, row.StopID, nullable(row.Station),
		nullable(row.Direction), nullable(row.Line), row.DayOfWeek,
		nullable(row.ScheduleType), row.ScheduledArrival, row.ExpectedArrival,
		row.ScheduledDeparture, row.ExpectedDeparture, row.ArrivalDelaySeconds,
		row.DepartureDelaySeconds, row.DwellSeconds, nullable(row.DepartureSource),
		row.VehicleAtStop, row.Monitored, nullable(row.VehicleRef),
	)
	return err
}

// CountTrainDepartures returns how many rows match the filter.
// Returns 0, nil when the database is not configured.
func CountTrainDepartures(filter DepartureFilter) (int, error) {
	if DB == nil {
		return 0, nil
	}
	clause, args := filter.where()
	var total int
	err := DB.QueryRow(`SELECT COUNT(*) FROM train_departures`+clause, args...).Scan(&total)
	return total, err
}

// ListTrainDepartures returns a page of departures matching the filter, most
// recently observed first. Returns nil, nil when the database is not configured.
func ListTrainDepartures(filter DepartureFilter, limit, offset int) ([]TrainDepartureRow, error) {
	if DB == nil {
		return nil, nil
	}
	clause, args := filter.where()
	args = append(args, limit, offset)
	query := fmt.Sprintf(
		`SELECT %s FROM train_departures%s ORDER BY last_seen_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		trainDepartureColumns, clause, len(args)-1, len(args),
	)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TrainDepartureRow
	for rows.Next() {
		r, err := scanTrainDepartureRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetTrainDepartureByID returns a single departure by primary key.
// Returns nil, nil when the database is not configured or the row is not found.
func GetTrainDepartureByID(id int64) (*TrainDepartureRow, error) {
	if DB == nil {
		return nil, nil
	}
	row := DB.QueryRow(`SELECT `+trainDepartureColumns+` FROM train_departures WHERE id = $1`, id)
	r, err := scanTrainDepartureRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteTrainDeparture removes a departure by primary key.
// If no database is configured (DB is nil) the call is a no-op.
func DeleteTrainDeparture(id int64) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`DELETE FROM train_departures WHERE id = $1`, id)
	return err
}

// StreamTrainDepartures iterates every departure matching the filter in service
// date order, invoking fn per row. Iteration stops on the first error.
// No-op (returns nil) when DB is nil.
func StreamTrainDepartures(filter DepartureFilter, fn func(TrainDepartureRow) error) error {
	if DB == nil {
		return nil
	}
	clause, args := filter.where()
	query := `SELECT ` + trainDepartureColumns + ` FROM train_departures` + clause +
		` ORDER BY service_date, train_number, id`

	rows, err := DB.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		r, err := scanTrainDepartureRow(rows)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

// FinalizeStaleDepartures marks rows whose stop visit has been absent from the
// feed for longer than grace, and returns how many were finalized. Callers must
// verify a recent successful poll first; see DepartureTracker.Finalize.
// Returns 0, nil when the database is not configured.
func FinalizeStaleDepartures(grace time.Duration) (int64, error) {
	if DB == nil {
		return 0, nil
	}
	result, err := DB.Exec(`
		UPDATE train_departures
		SET finalized_at = NOW()
		WHERE finalized_at IS NULL
		  AND last_seen_at < NOW() - make_interval(secs => $1)`,
		grace.Seconds(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
