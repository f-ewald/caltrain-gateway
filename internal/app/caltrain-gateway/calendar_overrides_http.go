package caltraingateway

import (
	_ "embed"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/calendar_overrides_list.html
var calendarOverridesListHTML string

// defaultCalendarOverrideAgencyID preselects the admin form's agency dropdown.
// Caltrain is the only agency this service has locally-loaded data for today,
// but an override can be recorded for any agency_id the operator enters —
// overrides are just labelled data, independent of what data is loaded.
const defaultCalendarOverrideAgencyID = departureOperatorID

// validScheduleTypes are the schedule types selectable in the admin dropdown,
// matching the ScheduleType values ResolveScheduleType/DetermineScheduleType produce.
var validScheduleTypes = []ScheduleType{ScheduleWeekday, ScheduleSaturday, ScheduleSunday, ScheduleHoliday}

// isValidScheduleType reports whether s is one of the known schedule types.
func isValidScheduleType(s string) bool {
	for _, t := range validScheduleTypes {
		if string(t) == s {
			return true
		}
	}
	return false
}

// calendarOverrideView decorates a row with display helpers for the admin template.
type calendarOverrideView struct {
	CalendarOverrideRow
}

// DateText renders the overridden date.
func (v calendarOverrideView) DateText() string {
	return v.OverrideDate.Format(dateLayout)
}

// CreatedAtText renders when the override was created.
func (v calendarOverrideView) CreatedAtText() string {
	return v.CreatedAt.In(pacificLocation()).Format("2006-01-02 15:04:05 MST")
}

// UpdatedAtText renders when the override was last edited.
func (v calendarOverrideView) UpdatedAtText() string {
	return v.UpdatedAt.In(pacificLocation()).Format("2006-01-02 15:04:05 MST")
}

// calendarOverridesPage is the view model for the list/create template.
type calendarOverridesPage struct {
	Overrides       []calendarOverrideView
	ScheduleTypes   []ScheduleType
	Agencies        []Agency
	DefaultAgencyID string
	Error           string
}

// calendarOverridesListHandler renders the create form and the existing overrides.
func calendarOverridesListHandler(w http.ResponseWriter, r *http.Request) {
	renderCalendarOverridesPage(w, "")
}

// renderCalendarOverridesPage loads the current overrides and renders the page,
// optionally surfacing a validation error from a just-submitted form.
func renderCalendarOverridesPage(w http.ResponseWriter, formError string) {
	rows, err := ListCalendarOverrides()
	if err != nil {
		http.Error(w, "Failed to load calendar overrides", http.StatusInternalServerError)
		return
	}
	views := make([]calendarOverrideView, 0, len(rows))
	for _, row := range rows {
		views = append(views, calendarOverrideView{CalendarOverrideRow: row})
	}
	renderAdminPage(w, "calendar_overrides_list", calendarOverridesListHTML,
		newAdminPage(tabCalendar, "Calendar overrides", calendarOverridesPage{
			Overrides:       views,
			ScheduleTypes:   validScheduleTypes,
			Agencies:        calendarOverrideAgencyOptions(),
			DefaultAgencyID: defaultCalendarOverrideAgencyID,
			Error:           formError,
		}))
}

// calendarOverrideAgencyOptions returns the agencies offered in the admin
// dropdown. It falls back to just the default agency when the directory
// (agencies.go) has not loaded, so the form always has at least one option.
func calendarOverrideAgencyOptions() []Agency {
	if all := AllAgencies(); len(all) > 0 {
		return all
	}
	name, _ := AgencyName(defaultCalendarOverrideAgencyID)
	if name == "" {
		name = defaultCalendarOverrideAgencyID
	}
	return []Agency{{ID: defaultCalendarOverrideAgencyID, Name: name}}
}

// calendarOverridesCreateHandler creates an override for a date, or edits one
// in place if the date already has an override.
func calendarOverridesCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	dateParam := strings.TrimSpace(r.PostFormValue("override_date"))
	scheduleType := strings.TrimSpace(r.PostFormValue("schedule_type"))
	note := strings.TrimSpace(r.PostFormValue("note"))
	agencyID := strings.ToUpper(strings.TrimSpace(r.PostFormValue("agency_id")))

	date, err := time.Parse(dateLayout, dateParam)
	if err != nil {
		renderCalendarOverridesPage(w, "Invalid date, expected YYYY-MM-DD")
		return
	}
	if agencyID == "" {
		renderCalendarOverridesPage(w, "Agency is required")
		return
	}
	if !isValidScheduleType(scheduleType) {
		renderCalendarOverridesPage(w, "Invalid schedule type")
		return
	}

	if err := UpsertCalendarOverride(agencyID, date, scheduleType, note); err != nil {
		http.Error(w, "Failed to save calendar override", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/calendar", http.StatusFound)
}

// calendarOverridesDeleteHandler removes an override and redirects to the list.
func calendarOverridesDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid or missing id parameter", http.StatusBadRequest)
		return
	}
	if err := DeleteCalendarOverride(id); err != nil {
		http.Error(w, "Failed to delete calendar override", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/calendar", http.StatusFound)
}
