package caltraingateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// LoadTimetable reads and parses a timetable JSON file from the given filename.
func LoadTimetable(filename string) (*Timetable, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read timetable file: %w", err)
	}

	return parseTimetableJSON(data)
}

// LoadTimetableFromURL fetches and parses a timetable JSON from the given URL.
func LoadTimetableFromURL(url string) (*Timetable, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch timetable from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return parseTimetableJSON(data)
}

// parseTimetableJSON parses the JSON data into a Timetable
func parseTimetableJSON(data []byte) (*Timetable, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var timetable Timetable
	if err := json.Unmarshal(data, &timetable); err != nil {
		return nil, fmt.Errorf("failed to parse timetable JSON: %w", err)
	}
	return &timetable, nil
}

// Timetable represents the root structure of the timetable JSON
type Timetable struct {
	Content Content `json:"Content"`
}

// Content holds all frame data for the timetable
type Content struct {
	ServiceFrame         ServiceFrame         `json:"ServiceFrame"`
	ServiceCalendarFrame ServiceCalendarFrame `json:"ServiceCalendarFrame"`
	TimetableFrame       []TimetableFrame     `json:"TimetableFrame"`
}

// ServiceFrame contains route information
type ServiceFrame struct {
	ID     string `json:"id"`
	Routes Routes `json:"routes"`
}

// Routes is a wrapper for the Route array
type Routes struct {
	Route []Route `json:"Route"`
}

// Route represents a transit route
type Route struct {
	ID               string           `json:"id"`
	Name             string           `json:"Name"`
	LineRef          Ref              `json:"LineRef"`
	DirectionRef     Ref              `json:"DirectionRef"`
	PointsInSequence PointsInSequence `json:"pointsInSequence"`
}

// Ref is a generic reference structure
type Ref struct {
	Ref  string `json:"ref"`
	Type string `json:"type,omitempty"`
}

// PointsInSequence contains the ordered stops on a route
type PointsInSequence struct {
	PointOnRoute []PointOnRoute `json:"PointOnRoute"`
}

// PointOnRoute represents a stop point on a route
type PointOnRoute struct {
	ID       string `json:"id"`
	PointRef Ref    `json:"PointRef"`
}

// ServiceCalendarFrame contains calendar/schedule validity information
type ServiceCalendarFrame struct {
	ID                 string             `json:"id"`
	DayTypes           DayTypes           `json:"dayTypes"`
	DayTypeAssignments DayTypeAssignments `json:"dayTypeAssignments"`
}

// DayTypes is a wrapper for the DayType array
type DayTypes struct {
	DayType []DayType `json:"DayType"`
}

// DayType represents a type of day (e.g., weekday, weekend)
type DayType struct {
	ID         string        `json:"id"`
	Name       string        `json:"Name"`
	Properties DayProperties `json:"properties"`
}

// DayProperties contains properties of a day type
type DayProperties struct {
	PropertyOfDay PropertyOfDay `json:"PropertyOfDay"`
}

// PropertyOfDay specifies which days of the week apply
type PropertyOfDay struct {
	DaysOfWeek string `json:"DaysOfWeek"`
}

// DayTypeAssignments contains day type assignment information
type DayTypeAssignments struct {
	DayTypeAssignment DayTypeAssignment `json:"DayTypeAssignment"`
}

// DayTypeAssignment links a day type to specific dates
type DayTypeAssignment struct {
	DayTypeRef *Ref `json:"DayTypeRef"`
}

// TimetableFrame contains the actual timetable data
type TimetableFrame struct {
	ID                      string                  `json:"id"`
	Name                    string                  `json:"Name"`
	FrameValidityConditions FrameValidityConditions `json:"frameValidityConditions"`
	VehicleJourneys         VehicleJourneys         `json:"vehicleJourneys"`
}

// FrameValidityConditions specifies when the timetable is valid
type FrameValidityConditions struct {
	AvailabilityCondition AvailabilityCondition `json:"AvailabilityCondition"`
}

// AvailabilityCondition defines the date range and day types for validity
type AvailabilityCondition struct {
	ID       string              `json:"id"`
	FromDate string              `json:"FromDate"`
	ToDate   string              `json:"ToDate"`
	DayTypes AvailabilityDayType `json:"dayTypes"`
}

// AvailabilityDayType references a day type for availability
type AvailabilityDayType struct {
	DayTypeRef Ref `json:"DayTypeRef"`
}

// VehicleJourneys is a wrapper for the ServiceJourney array
type VehicleJourneys struct {
	ServiceJourney []ServiceJourney `json:"ServiceJourney"`
}

// ServiceJourney represents a single train journey
type ServiceJourney struct {
	ID                    string             `json:"id"`
	SiriVehicleJourneyRef string             `json:"SiriVehicleJourneyRef"`
	JourneyPatternView    JourneyPatternView `json:"JourneyPatternView"`
	Calls                 Calls              `json:"calls"`
}

// JourneyPatternView contains route and direction references
type JourneyPatternView struct {
	RouteRef     Ref `json:"RouteRef"`
	DirectionRef Ref `json:"DirectionRef"`
}

// Calls is a wrapper for the Call array
type Calls struct {
	Call []Call `json:"Call"`
}

// Call represents a stop in a journey
type Call struct {
	Order                  string                 `json:"order"`
	ScheduledStopPointRef  Ref                    `json:"ScheduledStopPointRef"`
	Arrival                ArrivalDeparture       `json:"Arrival"`
	Departure              ArrivalDeparture       `json:"Departure"`
	DestinationDisplayView DestinationDisplayView `json:"DestinationDisplayView"`
}

// ArrivalDeparture contains arrival or departure time information
type ArrivalDeparture struct {
	Time       string `json:"Time"`
	DaysOffset string `json:"DaysOffset"`
}

// DestinationDisplayView contains the destination name to display
type DestinationDisplayView struct {
	Name string `json:"Name"`
}

// TrainDeparture represents a train departure at a specific stop
type TrainDeparture struct {
	TrainID       string `json:"trainId"`       // e.g., "401"
	Line          string `json:"line"`          // e.g., "Limited"
	Direction     string `json:"direction"`     // e.g., "N" or "S"
	ArrivalTime   string `json:"arrivalTime"`   // e.g., "05:43:00"
	DepartureTime string `json:"departureTime"` // e.g., "05:43:00"
	Destination   string `json:"destination"`   // e.g., "San Francisco"
	DaysOffset    string `json:"daysOffset"`    // e.g., "0"
}

// TimetableCollection holds multiple timetables (one per line)
type TimetableCollection struct {
	timetables []*Timetable
}

// NewTimetableCollection creates an empty TimetableCollection
func NewTimetableCollection() *TimetableCollection {
	return &TimetableCollection{
		timetables: make([]*Timetable, 0),
	}
}

// LoadTimetableFiles loads multiple timetable JSON files into the collection
func (tc *TimetableCollection) LoadTimetableFiles(filenames ...string) error {
	for _, filename := range filenames {
		tt, err := LoadTimetable(filename)
		if err != nil {
			return fmt.Errorf("failed to load timetable %s: %w", filename, err)
		}
		tc.timetables = append(tc.timetables, tt)
	}
	return nil
}

// AddTimetable adds a timetable to the collection
func (tc *TimetableCollection) AddTimetable(tt *Timetable) {
	tc.timetables = append(tc.timetables, tt)
}

// Weekday represents a day of the week
type Weekday string

const (
	Monday    Weekday = "Monday"
	Tuesday   Weekday = "Tuesday"
	Wednesday Weekday = "Wednesday"
	Thursday  Weekday = "Thursday"
	Friday    Weekday = "Friday"
	Saturday  Weekday = "Saturday"
	Sunday    Weekday = "Sunday"
)

// ParseWeekday converts a string to a Weekday, returns empty string if invalid
func ParseWeekday(s string) Weekday {
	switch s {
	case "Monday", "monday":
		return Monday
	case "Tuesday", "tuesday":
		return Tuesday
	case "Wednesday", "wednesday":
		return Wednesday
	case "Thursday", "thursday":
		return Thursday
	case "Friday", "friday":
		return Friday
	case "Saturday", "saturday":
		return Saturday
	case "Sunday", "sunday":
		return Sunday
	default:
		return ""
	}
}

// isValidForWeekday checks if a timetable frame is valid for the given weekday
func (t *Timetable) isValidForWeekday(frame TimetableFrame, weekday Weekday) bool {
	dayTypeRef := frame.FrameValidityConditions.AvailabilityCondition.DayTypes.DayTypeRef.Ref

	// Find the day type definition
	for _, dayType := range t.Content.ServiceCalendarFrame.DayTypes.DayType {
		if dayType.ID == dayTypeRef {
			// Check if the weekday is in the DaysOfWeek string
			daysOfWeek := dayType.Properties.PropertyOfDay.DaysOfWeek
			return strings.Contains(daysOfWeek, string(weekday))
		}
	}
	return false
}

// GetDeparturesByStop returns a map of stop IDs to their train departures.
// Each stop ID maps to a slice of TrainDeparture containing all trains
// that stop at that location.
func (t *Timetable) GetDeparturesByStop() map[string][]TrainDeparture {
	return t.GetDeparturesByStopAndWeekday("")
}

// GetDeparturesByStopAndWeekday returns departures filtered by weekday.
// If weekday is empty, returns all departures.
func (t *Timetable) GetDeparturesByStopAndWeekday(weekday Weekday) map[string][]TrainDeparture {
	result := make(map[string][]TrainDeparture)

	for _, frame := range t.Content.TimetableFrame {
		// Filter by weekday if specified
		if weekday != "" && !t.isValidForWeekday(frame, weekday) {
			continue
		}

		for _, journey := range frame.VehicleJourneys.ServiceJourney {
			line := journey.JourneyPatternView.RouteRef.Ref
			direction := journey.JourneyPatternView.DirectionRef.Ref

			// Look up the line name from the route
			for _, route := range t.Content.ServiceFrame.Routes.Route {
				if route.ID == line {
					line = route.LineRef.Ref
					break
				}
			}

			for _, call := range journey.Calls.Call {
				stopID := call.ScheduledStopPointRef.Ref
				departure := TrainDeparture{
					TrainID:       journey.ID,
					Line:          line,
					Direction:     direction,
					ArrivalTime:   call.Arrival.Time,
					DepartureTime: call.Departure.Time,
					Destination:   call.DestinationDisplayView.Name,
					DaysOffset:    call.Departure.DaysOffset,
				}
				result[stopID] = append(result[stopID], departure)
			}
		}
	}

	return result
}

// GetDeparturesByStop returns combined departures from all timetables
func (tc *TimetableCollection) GetDeparturesByStop() map[string][]TrainDeparture {
	return tc.GetDeparturesByStopAndWeekday("")
}

// GetDeparturesByStopAndWeekday returns combined departures filtered by weekday
func (tc *TimetableCollection) GetDeparturesByStopAndWeekday(weekday Weekday) map[string][]TrainDeparture {
	result := make(map[string][]TrainDeparture)

	for _, tt := range tc.timetables {
		departures := tt.GetDeparturesByStopAndWeekday(weekday)
		for stopID, deps := range departures {
			result[stopID] = append(result[stopID], deps...)
		}
	}

	return result
}
