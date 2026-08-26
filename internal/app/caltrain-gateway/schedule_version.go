package caltraingateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// versionPrefixLength is how many hex characters of the digest are published as
// the schedule version. 16 characters keeps the value short enough to pass
// around in an ETag while leaving collisions implausible.
const versionPrefixLength = 16

// ScheduleMetadata describes the provenance and validity of a loaded timetable.
type ScheduleMetadata struct {
	FrameIDs  []string
	ValidFrom time.Time
	ValidTo   time.Time
	LineCount int
}

// HasValidity reports whether a validity window was found in the source data.
func (m ScheduleMetadata) HasValidity() bool {
	return !m.ValidFrom.IsZero() && !m.ValidTo.IsZero()
}

// ExpiresInDays returns whole days from the given moment until the schedule
// lapses, negative once it has expired. The second result is false when the
// source data carried no validity window.
func (m ScheduleMetadata) ExpiresInDays(now time.Time) (int, bool) {
	if !m.HasValidity() {
		return 0, false
	}
	today := operatingDate(now)
	return int(m.ValidTo.Sub(today).Hours() / 24), true
}

// Expired reports whether the schedule's validity window has already closed.
func (m ScheduleMetadata) Expired(now time.Time) bool {
	if !m.HasValidity() {
		return false
	}
	days, _ := m.ExpiresInDays(now)
	return days < 0
}

// ScheduleVersion returns a stable identifier for the timetable's content.
//
// The digest is taken over a canonical projection of every departure, so the
// value changes if and only if the schedule a client consumes changes.
// Ordering is normalised first: GetDeparturesByStop appends in the order the
// upstream lines endpoint happened to return, so without sorting an upstream
// reordering would yield a different digest for identical content and push
// every client into a needless re-download.
func ScheduleVersion(tc *TimetableCollection) string {
	if tc == nil {
		return ""
	}

	byStop := tc.GetDeparturesByStop()
	stopIDs := make([]string, 0, len(byStop))
	for stopID := range byStop {
		stopIDs = append(stopIDs, stopID)
	}
	sort.Strings(stopIDs)

	digest := sha256.New()
	for _, stopID := range stopIDs {
		departures := append([]TrainDeparture(nil), byStop[stopID]...)
		sortDepartures(departures)

		digest.Write([]byte(stopID))
		digest.Write([]byte{0x1F})
		for _, departure := range departures {
			writeDepartureFields(digest, departure)
		}
		digest.Write([]byte{0x1E})
	}
	return hex.EncodeToString(digest.Sum(nil))[:versionPrefixLength]
}

// sortDepartures orders departures deterministically. Every distinguishing
// field participates so that two departures are only treated as equal when a
// client could not tell them apart either.
func sortDepartures(departures []TrainDeparture) {
	sort.Slice(departures, func(i, j int) bool {
		return departureSortKey(departures[i]) < departureSortKey(departures[j])
	})
}

// departureSortKey renders a departure as a single comparable string.
func departureSortKey(d TrainDeparture) string {
	return d.TrainID + "\x1f" + d.Line + "\x1f" + d.Direction + "\x1f" +
		d.DaysOffset + "\x1f" + d.DepartureTime + "\x1f" + d.ArrivalTime + "\x1f" + d.Destination
}

// writeDepartureFields feeds one departure into the digest, separating fields so
// that shifting content across boundaries cannot produce the same bytes.
func writeDepartureFields(digest interface{ Write([]byte) (int, error) }, d TrainDeparture) {
	fields := []string{
		d.TrainID, d.Line, d.Direction, d.ArrivalTime, d.DepartureTime,
		d.Destination, d.DaysOffset,
		strconv.FormatBool(d.OnWeekdays), strconv.FormatBool(d.OnWeekends),
	}
	for _, field := range fields {
		digest.Write([]byte(field))
		digest.Write([]byte{0x1F})
	}
}

// ExtractScheduleMetadata collects frame identifiers and the overall validity
// window spanning every loaded timetable. Frame IDs are sorted so the result is
// stable regardless of the order lines were fetched in.
func ExtractScheduleMetadata(tc *TimetableCollection) ScheduleMetadata {
	metadata := ScheduleMetadata{}
	if tc == nil {
		return metadata
	}

	metadata.LineCount = len(tc.timetables)
	for _, timetable := range tc.timetables {
		for _, frame := range timetable.Content.TimetableFrame {
			if frame.ID != "" {
				metadata.FrameIDs = append(metadata.FrameIDs, frame.ID)
			}
			condition := frame.FrameValidityConditions.AvailabilityCondition
			widenValidity(&metadata, condition.FromDate, condition.ToDate)
		}
	}
	sort.Strings(metadata.FrameIDs)
	return metadata
}

// parseValidityDate reads the calendar date from a validity bound, as a plain
// date at UTC midnight.
//
// Only the date portion is used, because 511 emits a fixed -08:00 offset all
// year rather than tracking daylight saving. Honouring that offset would place
// an end-of-day bound such as 2026-08-31T23:59:00-08:00 at 00:59 on 1 September
// Pacific, reporting the schedule as valid a day longer than it is.
func parseValidityDate(value string) (time.Time, bool) {
	if len(value) < len(dateLayout) {
		return time.Time{}, false
	}
	parsed, err := time.Parse(dateLayout, value[:len(dateLayout)])
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// widenValidity expands the metadata window to cover the given dates. The
// overall window is the widest span across frames, since the collection is only
// fully usable while at least one frame applies.
func widenValidity(metadata *ScheduleMetadata, fromDate, toDate string) {
	if from, ok := parseValidityDate(fromDate); ok {
		if metadata.ValidFrom.IsZero() || from.Before(metadata.ValidFrom) {
			metadata.ValidFrom = from
		}
	}
	if to, ok := parseValidityDate(toDate); ok {
		if metadata.ValidTo.IsZero() || to.After(metadata.ValidTo) {
			metadata.ValidTo = to
		}
	}
}

// formatValidityDate renders a validity bound as a calendar date, or "" when
// unset. The value is already a plain date, so no zone conversion applies.
func formatValidityDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

// scheduleETag builds a weak entity tag for one representation of the timetable.
//
// The variant must be part of the tag: the response body differs by weekday and
// station, so a single schedule-wide tag would let a client reuse a cached body
// for the wrong query. The tag is weak because it is derived from content rather
// than the exact bytes sent, which gzip would otherwise alter.
func scheduleETag(version, weekday, station string) string {
	if version == "" {
		return ""
	}
	return fmt.Sprintf(`W/"%s-%s-%s"`, version, defaultVariant(weekday), defaultVariant(station))
}

// defaultVariant renders an absent query parameter as a stable placeholder.
func defaultVariant(value string) string {
	if value == "" {
		return "all"
	}
	return value
}
