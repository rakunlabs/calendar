package mcp

import (
	"slices"
	"time"

	"github.com/rakunlabs/calendar/internal/core/domain"
)

type interval struct{ from, to time.Time }

// busy collapses occurrences into merged intervals in loc. All-day events block
// their whole span, so a holiday removes the day rather than leaving it bookable.
func busy(occurrences []domain.Event, loc *time.Location) []interval {
	intervals := make([]interval, 0, len(occurrences))
	for _, occurrence := range occurrences {
		from, to := occurrence.DateFrom.In(loc), occurrence.DateTo.In(loc)
		if occurrence.AllDay {
			// All-day instants are civil dates; read them as days in the target zone.
			from = time.Date(occurrence.DateFrom.Year(), occurrence.DateFrom.Month(), occurrence.DateFrom.Day(), 0, 0, 0, 0, loc)
			to = time.Date(occurrence.DateTo.Year(), occurrence.DateTo.Month(), occurrence.DateTo.Day(), 0, 0, 0, 0, loc)
		}
		if to.After(from) {
			intervals = append(intervals, interval{from: from, to: to})
		}
	}
	slices.SortFunc(intervals, func(a, b interval) int { return a.from.Compare(b.from) })
	merged := []interval{}
	for _, current := range intervals {
		if last := len(merged) - 1; last >= 0 && !current.from.After(merged[last].to) {
			if current.to.After(merged[last].to) {
				merged[last].to = current.to
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

// freeSlots walks each day's working window and reports the gaps left between
// merged busy intervals that are long enough to hold duration.
func freeSlots(occurrences []domain.Event, w window, duration time.Duration, dayStart, dayEnd int, weekends bool, limit int) []Slot {
	taken := busy(occurrences, w.loc)
	slots := []Slot{}
	day := time.Date(w.from.In(w.loc).Year(), w.from.In(w.loc).Month(), w.from.In(w.loc).Day(), 0, 0, 0, 0, w.loc)
	for ; day.Before(w.to) && len(slots) < limit; day = day.AddDate(0, 0, 1) {
		weekday := day.Weekday()
		if !weekends && (weekday == time.Saturday || weekday == time.Sunday) {
			continue
		}
		// Adding minutes to midnight keeps working hours on the wall clock across DST.
		cursor := day.Add(time.Duration(dayStart) * time.Minute)
		end := day.Add(time.Duration(dayEnd) * time.Minute)
		if cursor.Before(w.from) {
			cursor = w.from
		}
		if end.After(w.to) {
			end = w.to
		}
		for _, block := range taken {
			if !block.to.After(cursor) || !block.from.Before(end) {
				continue
			}
			if gap := block.from.Sub(cursor); gap >= duration {
				slots = append(slots, slot(cursor, block.from))
				if len(slots) >= limit {
					return slots
				}
			}
			if block.to.After(cursor) {
				cursor = block.to
			}
		}
		if end.Sub(cursor) >= duration {
			slots = append(slots, slot(cursor, end))
		}
	}
	return slots
}

func slot(from, to time.Time) Slot {
	return Slot{
		Start:   from.Format(time.RFC3339),
		End:     to.Format(time.RFC3339),
		Minutes: int(to.Sub(from) / time.Minute),
	}
}
