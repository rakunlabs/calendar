package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/core/domain"
)

const dateLayout = "2006-01-02"

// Wall-clock layouts are resolved in the event zone; an explicit offset wins.
var momentLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

func zone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown IANA time zone %q", name)
	}
	return loc, nil
}

// parseDate accepts only a civil date, which is what all-day spans are defined by.
func parseDate(value string, loc *time.Location) (time.Time, error) {
	parsed, err := time.ParseInLocation(dateLayout, strings.TrimSpace(value), loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected a %s date, got %q", dateLayout, value)
	}
	return parsed, nil
}

// parseMoment accepts a date, a wall-clock timestamp in loc, or an explicit offset.
func parseMoment(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}
	if len(value) == len(dateLayout) {
		return parseDate(value, loc)
	}
	for _, layout := range momentLayouts {
		parsed, err := time.ParseInLocation(layout, value, loc)
		if err != nil {
			continue
		}
		// A daylight-saving gap would otherwise be silently normalised to another instant.
		if layout != time.RFC3339Nano && parsed.Format(layout) != value {
			return time.Time{}, fmt.Errorf("%q does not exist in %s (daylight saving transition)", value, loc)
		}
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("expected an RFC3339 or %q timestamp, got %q", "2006-01-02T15:04:05", value)
}

// eventTimes converts tool input into the stored instants: all-day spans use an
// exclusive end so a single day ends on the next midnight, as ICS DTEND requires.
func eventTimes(allDay bool, start, end string, loc *time.Location) (types.Time, types.Time, error) {
	if allDay {
		from, err := parseDate(start, loc)
		if err != nil {
			return types.Time{}, types.Time{}, fmt.Errorf("start_date: %w", err)
		}
		last := from
		if strings.TrimSpace(end) != "" {
			if last, err = parseDate(end, loc); err != nil {
				return types.Time{}, types.Time{}, fmt.Errorf("end_date: %w", err)
			}
		}
		if last.Before(from) {
			return types.Time{}, types.Time{}, fmt.Errorf("end_date must not be before start_date")
		}
		return types.Time{Time: from}, types.Time{Time: last.AddDate(0, 0, 1)}, nil
	}
	from, err := parseMoment(start, loc)
	if err != nil {
		return types.Time{}, types.Time{}, fmt.Errorf("start: %w", err)
	}
	if strings.TrimSpace(end) == "" {
		return types.Time{}, types.Time{}, fmt.Errorf("end is required for a timed event")
	}
	to, err := parseMoment(end, loc)
	if err != nil {
		return types.Time{}, types.Time{}, fmt.Errorf("end: %w", err)
	}
	if !to.After(from) {
		return types.Time{}, types.Time{}, fmt.Errorf("end must be after start")
	}
	return types.Time{Time: from}, types.Time{Time: to}, nil
}

// parseClock reads a HH:MM working-hour boundary as minutes after midnight.
func parseClock(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, fmt.Errorf("expected a HH:MM time of day, got %q", value)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

// describe renders an occurrence for the model: all-day spans report the
// inclusive last day, because an exclusive midnight reads as an extra day.
func describe(event domain.Event) Occurrence {
	loc := event.DateFrom.Location()
	out := Occurrence{
		ID: event.ID, Name: event.Name, Description: event.Description,
		AllDay: event.AllDay, TimeZone: event.Tz, RRule: event.RRule, Disabled: event.Disabled,
	}
	if event.EventGroup.Valid {
		out.EventGroup = event.EventGroup.V
	}
	if event.RecurrenceID != nil {
		out.RecurrenceID = event.RecurrenceID.Value
	}
	if event.AllDay {
		out.StartDate = event.DateFrom.Format(dateLayout)
		out.EndDate = event.DateTo.AddDate(0, 0, -1).Format(dateLayout)
		return out
	}
	out.Start = event.DateFrom.In(loc).Format(time.RFC3339)
	out.End = event.DateTo.In(loc).Format(time.RFC3339)
	return out
}
