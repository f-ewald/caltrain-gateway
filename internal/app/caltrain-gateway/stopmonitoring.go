package caltraingateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// errRateLimited reports that the upstream API rejected a request because the
// key's quota is exhausted, so the caller should back off rather than retry.
var errRateLimited = errors.New("511 API rate limit exceeded")

// flexString unmarshals a JSON value that SIRI producers encode inconsistently:
// as a plain string, a single-element array, or a bare number. Nulls, objects
// and empty input decode to "".
//
// This never fails. The feed is fetched as one agency-wide document, so
// returning an error would discard every train in the response over a single
// odd field.
type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*s = ""
		return nil
	}
	switch trimmed[0] {
	case '"':
		var single string
		if err := json.Unmarshal(trimmed, &single); err != nil {
			*s = ""
			return nil
		}
		*s = flexString(single)
	case '[':
		var list []flexString
		if err := json.Unmarshal(trimmed, &list); err != nil {
			*s = ""
			return nil
		}
		if len(list) > 0 {
			*s = list[0]
		}
	case '{':
		*s = ""
	default:
		// A bare number or literal: keep its exact source text so an ID such as
		// 70011 does not gain a float representation.
		*s = flexString(trimmed)
	}
	return nil
}

// flexBool unmarshals a JSON boolean that SIRI producers may emit as a quoted
// string ("true"/"false"), as an empty string meaning "no value", or omit
// entirely.
//
// Anything unrecognised decodes to false, and never as an error. False is the
// conservative reading for both fields that use this type: Monitored=false
// marks a prediction as untrustworthy and VehicleAtStop=false means arrival was
// never confirmed, so an unknown value degrades data quality rather than
// overstating it. Failing instead would reject the entire agency-wide response.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*b = false
		return nil
	}
	if trimmed[0] == '"' {
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			*b = false
			return nil
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
		*b = flexBool(err == nil && parsed)
		return nil
	}
	var parsed bool
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		*b = false
		return nil
	}
	*b = flexBool(parsed)
	return nil
}

// stopMonitoringDeliveries accepts a StopMonitoringDelivery encoded either as a
// single object or as an array of objects, which differs between SIRI producers.
type stopMonitoringDeliveries []StopMonitoringDelivery

func (d *stopMonitoringDeliveries) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*d = nil
		return nil
	}
	if trimmed[0] == '[' {
		var list []StopMonitoringDelivery
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		*d = list
		return nil
	}
	var single StopMonitoringDelivery
	if err := json.Unmarshal(trimmed, &single); err != nil {
		return err
	}
	*d = []StopMonitoringDelivery{single}
	return nil
}

// StopMonitoringResponse is the root of a SIRI StopMonitoring response.
type StopMonitoringResponse struct {
	ServiceDelivery SiriServiceDelivery `json:"ServiceDelivery"`
}

// SiriServiceDelivery wraps the stop monitoring deliveries and feed metadata.
type SiriServiceDelivery struct {
	ResponseTimestamp      string                   `json:"ResponseTimestamp"`
	ProducerRef            flexString               `json:"ProducerRef"`
	StopMonitoringDelivery stopMonitoringDeliveries `json:"StopMonitoringDelivery"`
}

// StopMonitoringDelivery holds the stop visits reported in one delivery.
type StopMonitoringDelivery struct {
	ResponseTimestamp  string               `json:"ResponseTimestamp"`
	MonitoredStopVisit []MonitoredStopVisit `json:"MonitoredStopVisit"`
}

// MonitoredStopVisit is a single upcoming call of one vehicle at one stop.
type MonitoredStopVisit struct {
	RecordedAtTime          string                  `json:"RecordedAtTime"`
	MonitoringRef           flexString              `json:"MonitoringRef"`
	MonitoredVehicleJourney MonitoredVehicleJourney `json:"MonitoredVehicleJourney"`
}

// MonitoredVehicleJourney describes the vehicle and journey serving a stop visit.
type MonitoredVehicleJourney struct {
	LineRef                 flexString              `json:"LineRef"`
	DirectionRef            flexString              `json:"DirectionRef"`
	FramedVehicleJourneyRef FramedVehicleJourneyRef `json:"FramedVehicleJourneyRef"`
	PublishedLineName       flexString              `json:"PublishedLineName"`
	OperatorRef             flexString              `json:"OperatorRef"`
	VehicleJourneyName      flexString              `json:"VehicleJourneyName"`
	Monitored               flexBool                `json:"Monitored"`
	VehicleRef              flexString              `json:"VehicleRef"`
	MonitoredCall           MonitoredCall           `json:"MonitoredCall"`
}

// FramedVehicleJourneyRef identifies a journey within a specific operating day.
type FramedVehicleJourneyRef struct {
	DataFrameRef           flexString `json:"DataFrameRef"`
	DatedVehicleJourneyRef flexString `json:"DatedVehicleJourneyRef"`
}

// MonitoredCall carries the scheduled and predicted times for one stop.
// 511 is not expected to populate the Actual* fields; see TrainNumber and
// departureObservation for how actual departures are inferred instead.
type MonitoredCall struct {
	StopPointRef          flexString `json:"StopPointRef"`
	StopPointName         flexString `json:"StopPointName"`
	VehicleAtStop         flexBool   `json:"VehicleAtStop"`
	AimedArrivalTime      string     `json:"AimedArrivalTime"`
	ExpectedArrivalTime   string     `json:"ExpectedArrivalTime"`
	ActualArrivalTime     string     `json:"ActualArrivalTime"`
	AimedDepartureTime    string     `json:"AimedDepartureTime"`
	ExpectedDepartureTime string     `json:"ExpectedDepartureTime"`
	ActualDepartureTime   string     `json:"ActualDepartureTime"`
}

// TrainNumber returns the public Caltrain train number for the journey,
// preferring DatedVehicleJourneyRef and falling back to VehicleJourneyName.
// Returns "" when neither is present.
func (j *MonitoredVehicleJourney) TrainNumber() string {
	if ref := string(j.FramedVehicleJourneyRef.DatedVehicleJourneyRef); ref != "" {
		return ref
	}
	return string(j.VehicleJourneyName)
}

// Visits returns every monitored stop visit across all deliveries in the response.
func (r *StopMonitoringResponse) Visits() []MonitoredStopVisit {
	var visits []MonitoredStopVisit
	for _, delivery := range r.ServiceDelivery.StopMonitoringDelivery {
		visits = append(visits, delivery.MonitoredStopVisit...)
	}
	return visits
}

// LoadStopMonitoring reads and parses a SIRI StopMonitoring JSON file.
func LoadStopMonitoring(filename string) (*StopMonitoringResponse, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read stop monitoring file: %w", err)
	}

	return parseStopMonitoringJSON(data)
}

// LoadStopMonitoringFromURL fetches and parses SIRI StopMonitoring JSON from a URL.
// A 429 response is reported as errRateLimited so callers can back off.
func LoadStopMonitoringFromURL(url string) (*StopMonitoringResponse, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stop monitoring from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return parseStopMonitoringJSON(data)
}

// parseStopMonitoringJSON parses the JSON data into a StopMonitoringResponse.
func parseStopMonitoringJSON(data []byte) (*StopMonitoringResponse, error) {
	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var response StopMonitoringResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse stop monitoring JSON: %w", err)
	}
	return &response, nil
}
