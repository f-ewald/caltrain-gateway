package caltraingateway

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed web/departures_list.html
var departuresListHTML string

//go:embed web/departures_detail.html
var departuresDetailHTML string

const (
	// departuresPageSize bounds each admin page; the table grows by roughly
	// 2,500 rows per day, so the list must never be rendered unpaginated.
	departuresPageSize = 50

	// onTimeThresholdSeconds matches Caltrain's own on-time definition, used
	// only to decide when the UI highlights a delay.
	onTimeThresholdSeconds = 300

	dateLayout = "2006-01-02"
)

// departureView decorates a row with display helpers for the admin templates.
// Times are rendered in Pacific local time, which is what an operator reasons
// about, while the stored values remain UTC.
type departureView struct {
	*TrainDepartureRow
}

// ServiceDateText renders the operating day.
func (v departureView) ServiceDateText() string {
	return v.ServiceDate.Format(dateLayout)
}

// ScheduledDepartureText renders the timetabled departure.
func (v departureView) ScheduledDepartureText() string {
	return formatPacificClock(v.ScheduledDeparture)
}

// ExpectedDepartureText renders the observed (inferred actual) departure.
func (v departureView) ExpectedDepartureText() string {
	return formatPacificClock(v.ExpectedDeparture)
}

// ScheduledArrivalText renders the timetabled arrival.
func (v departureView) ScheduledArrivalText() string {
	return formatPacificClock(v.ScheduledArrival)
}

// ExpectedArrivalText renders the observed arrival.
func (v departureView) ExpectedArrivalText() string {
	return formatPacificClock(v.ExpectedArrival)
}

// FirstSeenText renders when the stop visit was first observed.
func (v departureView) FirstSeenText() string {
	return formatPacificStamp(&v.FirstSeenAt)
}

// LastSeenText renders the final observation time, which bounds the accuracy of
// an inferred departure.
func (v departureView) LastSeenText() string {
	return formatPacificStamp(&v.LastSeenAt)
}

// FinalizedText renders when the row was concluded.
func (v departureView) FinalizedText() string {
	return formatPacificStamp(v.FinalizedAt)
}

// DelayText renders the departure delay.
func (v departureView) DelayText() string { return formatDelay(v.DepartureDelaySeconds) }

// DelayClass returns the CSS class highlighting the departure delay.
func (v departureView) DelayClass() string { return delayClass(v.DepartureDelaySeconds) }

// ArrivalDelayText renders the arrival delay.
func (v departureView) ArrivalDelayText() string { return formatDelay(v.ArrivalDelaySeconds) }

// ArrivalDelayClass returns the CSS class highlighting the arrival delay.
func (v departureView) ArrivalDelayClass() string { return delayClass(v.ArrivalDelaySeconds) }

// DwellText renders the observed dwell duration at the stop.
func (v departureView) DwellText() string {
	if v.DwellSeconds == nil {
		return "—"
	}
	return fmt.Sprintf("%ds", *v.DwellSeconds)
}

// formatPacificClock renders a timestamp as a Pacific wall clock time, or an
// em dash when unknown.
func formatPacificClock(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.In(pacificLocation()).Format("15:04:05")
}

// formatPacificStamp renders a full Pacific timestamp, or an em dash when unknown.
func formatPacificStamp(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.In(pacificLocation()).Format("2006-01-02 15:04:05 MST")
}

// formatDelay renders a signed delay in seconds as a readable duration, where
// a negative value means the train was early.
func formatDelay(seconds *int) string {
	if seconds == nil {
		return "—"
	}
	value := *seconds
	if value == 0 {
		return "on time"
	}
	sign := "+"
	if value < 0 {
		sign = "-"
		value = -value
	}
	if value < 60 {
		return fmt.Sprintf("%s%ds", sign, value)
	}
	return fmt.Sprintf("%s%dm %02ds", sign, value/60, value%60)
}

// delayClass maps a delay to a highlight class, treating Caltrain's five-minute
// on-time threshold as the boundary for "late".
func delayClass(seconds *int) string {
	if seconds == nil {
		return ""
	}
	if *seconds >= onTimeThresholdSeconds {
		return "late"
	}
	if *seconds < 0 {
		return "early"
	}
	return ""
}

// departuresPage is the view model for the paginated list template.
type departuresPage struct {
	Departures []departureView
	Filter     DepartureFilter
	Page       int
	TotalPages int
	Total      int
}

// HasPrev reports whether an earlier page exists.
func (p departuresPage) HasPrev() bool { return p.Page > 1 }

// HasNext reports whether a later page exists.
func (p departuresPage) HasNext() bool { return p.Page < p.TotalPages }

// PrevQuery returns the link to the previous page.
func (p departuresPage) PrevQuery() string { return departuresLink(p.Filter, p.Page-1) }

// NextQuery returns the link to the next page.
func (p departuresPage) NextQuery() string { return departuresLink(p.Filter, p.Page+1) }

// ExportQuery returns the query string carrying the current filter to the export
// endpoint, so a download matches what is on screen.
func (p departuresPage) ExportQuery() string {
	values := departureFilterValues(p.Filter)
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

// departureFilterValues renders the non-empty filter fields as query parameters.
func departureFilterValues(filter DepartureFilter) url.Values {
	values := url.Values{}
	for key, value := range map[string]string{
		"service_date": filter.ServiceDate,
		"station":      filter.Station,
		"train_number": filter.TrainNumber,
		"from":         filter.From,
		"to":           filter.To,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

// departuresLink builds a list URL preserving the filter and page.
func departuresLink(filter DepartureFilter, page int) string {
	values := departureFilterValues(filter)
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if len(values) == 0 {
		return "/admin/departures"
	}
	return "/admin/departures?" + values.Encode()
}

// parseDepartureFilter reads filter parameters from the request. It reports an
// error message when a date parameter is malformed, so an invalid value is
// rejected up front rather than reaching PostgreSQL as a bad cast.
func parseDepartureFilter(r *http.Request) (DepartureFilter, string) {
	query := r.URL.Query()
	filter := DepartureFilter{
		ServiceDate: strings.TrimSpace(query.Get("service_date")),
		Station:     strings.TrimSpace(query.Get("station")),
		TrainNumber: strings.TrimSpace(query.Get("train_number")),
		From:        strings.TrimSpace(query.Get("from")),
		To:          strings.TrimSpace(query.Get("to")),
	}
	for name, value := range map[string]string{
		"service_date": filter.ServiceDate,
		"from":         filter.From,
		"to":           filter.To,
	} {
		if value == "" {
			continue
		}
		if _, err := time.Parse(dateLayout, value); err != nil {
			return filter, "Invalid " + name + " parameter, expected YYYY-MM-DD"
		}
	}
	return filter, ""
}

// departuresListHandler renders a filtered, paginated page of recorded departures.
func departuresListHandler(w http.ResponseWriter, r *http.Request) {
	filter, problem := parseDepartureFilter(r)
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}

	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			http.Error(w, "Invalid page parameter", http.StatusBadRequest)
			return
		}
		page = parsed
	}

	total, err := CountTrainDepartures(filter)
	if err != nil {
		http.Error(w, "Failed to count departures", http.StatusInternalServerError)
		return
	}
	rows, err := ListTrainDepartures(filter, departuresPageSize, (page-1)*departuresPageSize)
	if err != nil {
		http.Error(w, "Failed to load departures", http.StatusInternalServerError)
		return
	}

	renderDeparturesPage(w, buildDeparturesPage(rows, filter, page, total))
}

// buildDeparturesPage assembles the list view model.
func buildDeparturesPage(rows []TrainDepartureRow, filter DepartureFilter, page, total int) departuresPage {
	totalPages := (total + departuresPageSize - 1) / departuresPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	views := make([]departureView, 0, len(rows))
	for i := range rows {
		views = append(views, departureView{TrainDepartureRow: &rows[i]})
	}
	return departuresPage{
		Departures: views,
		Filter:     filter,
		Page:       page,
		TotalPages: totalPages,
		Total:      total,
	}
}

// renderDeparturesPage writes the list template.
func renderDeparturesPage(w http.ResponseWriter, page departuresPage) {
	tmpl, err := template.New("departures_list").Parse(departuresListHTML)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, page)
}

// departuresDetailHandler renders a single recorded departure.
func departuresDetailHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDepartureID(w, r)
	if !ok {
		return
	}
	row, err := GetTrainDepartureByID(id)
	if err != nil {
		http.Error(w, "Failed to load departure", http.StatusInternalServerError)
		return
	}
	if row == nil {
		http.Error(w, "Departure not found", http.StatusNotFound)
		return
	}
	tmpl, err := template.New("departures_detail").Parse(departuresDetailHTML)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, departureView{TrainDepartureRow: row})
}

// departuresDeleteHandler removes a departure and redirects to the list.
func departuresDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, ok := parseDepartureID(w, r)
	if !ok {
		return
	}
	if err := DeleteTrainDeparture(id); err != nil {
		http.Error(w, "Failed to delete departure", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/departures", http.StatusFound)
}

// parseDepartureID reads and validates the id query parameter, writing an error
// response and reporting false when it is missing or malformed.
func parseDepartureID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

// departuresExportHandler streams matching departures as JSONL. The same filter
// parameters as the list apply, plus from/to service-date bounds, so an export
// of a multi-year table can be limited to a tractable range.
func departuresExportHandler(w http.ResponseWriter, r *http.Request) {
	filter, problem := parseDepartureFilter(r)
	if problem != "" {
		http.Error(w, problem, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="train-departures.jsonl"`)

	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)

	count := 0
	err := StreamTrainDepartures(filter, func(row TrainDepartureRow) error {
		if err := encoder.Encode(row); err != nil {
			return err
		}
		count++
		if flusher != nil && count%200 == 0 {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		// Headers are already sent by this point if any rows were written;
		// the best we can do is log and let the client see a truncated stream.
		log.Printf("departures export failed after %d rows: %v", count, err)
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}
