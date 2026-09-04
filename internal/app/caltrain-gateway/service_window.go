package caltraingateway

import (
	"time"
)

// serviceWindowMargin extends the polling window either side of scheduled
// service. The trailing margin matters most: stopping at the last scheduled
// arrival would leave a delayed final train never observed departing, and its
// row never closed.
const serviceWindowMargin = 30 * time.Minute

// serviceWindow is the span of a service day during which trains run, expressed
// as offsets from midnight of the operating day. The end may exceed 24h because
// trains that cross midnight carry a DaysOffset.
type serviceWindow struct {
	start time.Duration
	end   time.Duration
}

// valid reports whether a usable window was derived.
func (w serviceWindow) valid() bool {
	return w.end > w.start
}

// parseClock converts an "HH:MM:SS" timetable time plus its DaysOffset into a
// duration from midnight of the operating day.
func parseClock(clock, daysOffset string) (time.Duration, bool) {
	parsed, err := time.Parse("15:04:05", clock)
	if err != nil {
		return 0, false
	}
	offsetDays := 0
	if daysOffset != "" {
		if days, err := time.ParseDuration(daysOffset + "h"); err == nil {
			offsetDays = int(days.Hours())
		}
	}
	sinceMidnight := time.Duration(parsed.Hour())*time.Hour +
		time.Duration(parsed.Minute())*time.Minute +
		time.Duration(parsed.Second())*time.Second
	return sinceMidnight + time.Duration(offsetDays)*24*time.Hour, true
}

// computeServiceWindow derives the earliest departure and latest arrival across
// every line for the given weekday, widened by serviceWindowMargin.
//
// DaysOffset is honoured, so a train that departs before midnight and arrives
// after it extends the window past 24h rather than wrapping to the start of the
// day.
func computeServiceWindow(tc *TimetableCollection, weekday Weekday) (serviceWindow, bool) {
	if tc == nil {
		return serviceWindow{}, false
	}

	first, last := time.Duration(-1), time.Duration(-1)
	for _, departures := range tc.GetDeparturesByStopAndWeekday(weekday) {
		for _, departure := range departures {
			for _, moment := range []string{departure.DepartureTime, departure.ArrivalTime} {
				value, ok := parseClock(moment, departure.DaysOffset)
				if !ok {
					continue
				}
				if first < 0 || value < first {
					first = value
				}
				if value > last {
					last = value
				}
			}
		}
	}
	if first < 0 || last < 0 || last <= first {
		return serviceWindow{}, false
	}
	return serviceWindow{start: first - serviceWindowMargin, end: last + serviceWindowMargin}, true
}

// withinServiceWindow reports whether trains are expected to be running at the
// given instant, and whether a window could be determined at all.
//
// The instant is measured against its operating day, so an early-morning moment
// is compared with the previous day's late service rather than the coming day's.
func withinServiceWindow(tc *TimetableCollection, now time.Time) (inService bool, known bool) {
	if tc == nil {
		return false, false
	}

	local := now.In(pacificLocation())
	serviceDate := operatingDate(now)
	weekday := ParseWeekday(serviceDate.Weekday().String())

	window, ok := computeServiceWindow(tc, weekday)
	if !ok || !window.valid() {
		return false, false
	}

	midnight := time.Date(serviceDate.Year(), serviceDate.Month(), serviceDate.Day(), 0, 0, 0, 0, pacificLocation())
	elapsed := local.Sub(midnight)
	return elapsed >= window.start && elapsed <= window.end, true
}

// shouldPollDepartures reports whether the poller should run now.
//
// It fails open: when no timetable is loaded the window cannot be computed, and
// polling continues. Spending quota on empty responses is recoverable, whereas
// silently collecting nothing for a day is not. Departure tracking is
// Caltrain-only, so this always checks CT's timetable regardless of what other
// agencies' data is loaded.
func shouldPollDepartures(now time.Time) bool {
	inService, known := withinServiceWindow(GetTimetableCollection(departureOperatorID), now)
	if !known {
		return true
	}
	return inService
}
