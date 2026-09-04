package caltraingateway

import (
	"log"
	"strings"
	"sync"
	"time"
)

// scheduleState holds the loaded timetable together with everything derived
// from it. Version and metadata are stored alongside the collection and swapped
// under one lock, so a client can never read a version that disagrees with the
// schedule being served.
type scheduleState struct {
	mu          sync.RWMutex
	collection  *TimetableCollection
	version     string
	metadata    ScheduleMetadata
	refreshedAt time.Time
	// lastAttempt records when a refresh was last tried, successfully or not,
	// so staleness can distinguish "verified current" from "not rechecked".
	lastAttempt time.Time
}

// schedule is the process-wide timetable state for Caltrain (CT), the
// original and still most fully-featured agency.
var schedule = &scheduleState{}

// bartSchedule is the process-wide timetable state for BART (BA).
var bartSchedule = &scheduleState{}

// scheduleFor returns the schedule state for a known operator (case-insensitive),
// or nil for any agency this service does not locally load timetable data for.
// Only CT and BA are backed by real state today; extending this to a third
// agency means adding one more case here.
func scheduleFor(operatorID string) *scheduleState {
	switch strings.ToUpper(operatorID) {
	case departureOperatorID: // "CT"
		return schedule
	case bartOperatorID: // "BA"
		return bartSchedule
	default:
		return nil
	}
}

// Publish atomically replaces the timetable and recomputes everything derived
// from it. A nil collection is ignored so a failed refresh cannot erase a
// working schedule.
func (s *scheduleState) Publish(tc *TimetableCollection) {
	if tc == nil {
		return
	}
	version := ScheduleVersion(tc)
	metadata := ExtractScheduleMetadata(tc)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.collection = tc
	s.version = version
	s.metadata = metadata
	s.refreshedAt = now
	s.lastAttempt = now
}

// RecordFailedAttempt notes that a refresh ran but did not produce a usable
// schedule, leaving the previously published one in place.
func (s *scheduleState) RecordFailedAttempt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAttempt = time.Now()
}

// Collection returns the current timetable, or nil when none has loaded.
func (s *scheduleState) Collection() *TimetableCollection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collection
}

// Version returns the current schedule version, or "" when none has loaded.
func (s *scheduleState) Version() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// Snapshot returns the collection and everything derived from it as a single
// consistent view.
func (s *scheduleState) Snapshot() (*TimetableCollection, string, ScheduleMetadata, time.Time, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collection, s.version, s.metadata, s.refreshedAt, s.lastAttempt
}

// SetTimetableCollection publishes a timetable collection for operatorID,
// recomputing its version and validity metadata. A call for an operator this
// service does not track state for (see scheduleFor) is a no-op.
func SetTimetableCollection(operatorID string, tc *TimetableCollection) {
	s := scheduleFor(operatorID)
	if s == nil {
		return
	}
	s.Publish(tc)
	if version := s.Version(); version != "" {
		log.Printf("Timetable published for %s, schedule version %s", strings.ToUpper(operatorID), version)
	}
}

// GetTimetableCollection returns the currently served timetable for operatorID,
// or nil when none has loaded or the operator is untracked.
func GetTimetableCollection(operatorID string) *TimetableCollection {
	s := scheduleFor(operatorID)
	if s == nil {
		return nil
	}
	return s.Collection()
}
