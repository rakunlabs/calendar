package ical

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
)

// ResolveCalendarDate resolves a preserved ICS value. Floating values and DATEs
// use defaultTZ; leap seconds are interpreted as second 59 without changing metadata.
func ResolveCalendarDate(date models.CalendarDate, defaultTZ *time.Location, definitions []string) (time.Time, error) {
	if defaultTZ == nil {
		defaultTZ = time.UTC
	}
	loc, layout, value := defaultTZ, "20060102T150405", date.Value
	if strings.ContainsAny(value+date.TZID+date.Type, "\r\n\"") {
		return time.Time{}, fmt.Errorf("invalid calendar date")
	}
	switch date.Type {
	case "DATE":
		if date.TZID != "" {
			return time.Time{}, fmt.Errorf("DATE cannot have TZID")
		}
		layout = "20060102"
	case "", "DATE-TIME":
		if date.TZID != "" {
			if strings.HasSuffix(value, "Z") {
				return time.Time{}, fmt.Errorf("UTC date cannot have TZID")
			}
			var err error
			loc, err = ResolveLocation(date.TZID, definitions)
			if err != nil {
				return time.Time{}, err
			}
		}
		if strings.HasSuffix(value, "Z") {
			layout, loc = "20060102T150405Z", time.UTC
		}
		if len(value) >= 15 && value[13:15] == "60" {
			value = value[:13] + "59" + value[15:]
		}
	default:
		return time.Time{}, fmt.Errorf("unsupported calendar date type %q", date.Type)
	}
	if len(value) != len(layout) {
		return time.Time{}, fmt.Errorf("invalid calendar date %q", date.Value)
	}
	t, err := time.ParseInLocation(layout, value, loc)
	if err != nil {
		return time.Time{}, err
	}
	return firstRRuleLocalTime(t), nil
}

func CalendarDateFromTime(t time.Time, allDay bool) models.CalendarDate {
	if allDay {
		return models.CalendarDate{Value: t.Format("20060102"), Type: "DATE"}
	}
	if t.Location() == time.UTC {
		return models.CalendarDate{Value: t.Format("20060102T150405Z")}
	}
	return models.CalendarDate{Value: t.Format("20060102T150405"), TZID: t.Location().String()}
}

func eventTimezoneDefinitions(e models.Event, inherited []string) []string {
	definitions := slices.Clone(inherited)
	if e.Recurrence != nil {
		for _, definition := range e.Recurrence.Timezones {
			if !slices.Contains(definitions, definition) {
				definitions = append(definitions, definition)
			}
		}
	}
	return definitions
}

// EventTimes resolves the effective start and end without mutating the event.
// Lexical Start/End and nominal Duration take precedence over DateFrom/DateTo.
// Services can use these values to normalize or check their stored projections;
// ValidateEvent separately validates the complete recurrence set.
func EventTimes(event models.Event) (time.Time, time.Time, error) {
	start, end, _, err := eventTiming(event, nil)
	return start, end, err
}

// definitions contains inherited timezone definitions, not just the event's own.
func eventTiming(e models.Event, definitions []string) (time.Time, time.Time, calendarDuration, error) {
	definitions = eventTimezoneDefinitions(e, definitions)
	loc := e.DateFrom.Location()
	var err error
	if e.Tz != "" {
		loc, err = ResolveLocation(e.Tz, definitions)
		if err != nil {
			return time.Time{}, time.Time{}, calendarDuration{}, err
		}
	}
	a, b := e.DateFrom.In(loc), e.DateTo.In(loc)
	r := e.Recurrence
	if r != nil {
		if r.Start != nil {
			a, err = ResolveCalendarDate(*r.Start, loc, definitions)
			if err != nil {
				return a, b, calendarDuration{}, err
			}
		}
		if r.End != nil {
			b, err = ResolveCalendarDate(*r.End, loc, definitions)
			if err != nil {
				return a, b, calendarDuration{}, err
			}
		}
		if r.Duration != "" {
			if a.IsZero() {
				return a, b, calendarDuration{}, fmt.Errorf("missing or invalid DTSTART")
			}
			if r.End != nil {
				return a, b, calendarDuration{}, fmt.Errorf("DTEND and DURATION are mutually exclusive")
			}
			d, err := parseDuration(r.Duration)
			if err == nil && e.AllDay && d.exact != 0 {
				err = fmt.Errorf("DATE DURATION must use days or weeks")
			}
			return a, d.end(a), d, err
		}
	}
	if a.IsZero() {
		return a, b, calendarDuration{}, fmt.Errorf("missing or invalid DTSTART")
	}
	if b.IsZero() && e.AllDay {
		b = a.AddDate(0, 0, 1)
	}
	if b.IsZero() {
		return a, b, calendarDuration{}, fmt.Errorf("timed event requires DTEND or DURATION")
	}
	if !b.After(a) {
		return a, b, calendarDuration{}, fmt.Errorf("DTEND must be after DTSTART")
	}
	d := calendarDuration{exact: b.Sub(a)}
	if e.AllDay {
		d = calendarDuration{days: int((time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC).Unix() - time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC).Unix()) / 86400)}
	}
	return a, b, d, nil
}

// ValidateEvent validates persisted masters and their atomic recurrence metadata.
// Start/End lexical metadata is authoritative when present; response identity fields
// are not detached-event input. Overrides must carry their original RecurrenceID.
func ValidateEvent(event models.Event) error {
	return validateEvent(context.Background(), event, nil, false)
}

func validateEvent(ctx context.Context, e models.Event, defs []string, detached bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := e.Recurrence
	a, _, _, err := eventTiming(e, defs)
	if err != nil {
		return fmt.Errorf("event %s: %w", e.ID, err)
	}
	defs = eventTimezoneDefinitions(e, defs)
	// Validate even unused definitions and UTC/floating events so invalid raw
	// metadata cannot be persisted and only fail later during calendar export.
	if err := ValidateTimezoneDefinitions(defs); err != nil {
		return fmt.Errorf("event %s timezones: %w", e.ID, err)
	}
	if e.RRule != "" {
		if detached {
			return fmt.Errorf("detached override cannot define a recurrence set")
		}
		repeat, err := ParseRepeat(e.RRule)
		if err != nil {
			return err
		}
		for _, rule := range repeat.RRule {
			if e.AllDay && (rule.Freq == "HOURLY" || rule.Freq == "MINUTELY" || rule.Freq == "SECONDLY") {
				return fmt.Errorf("DATE DTSTART cannot use sub-daily recurrence")
			}
		}
	}
	if r == nil {
		return nil
	}
	if detached && (len(r.Overrides) > 0 || len(r.RDates) > 0 || len(r.ExDates) > 0 || e.RRule != "") {
		return fmt.Errorf("detached override cannot define a recurrence set")
	}
	check := func(d models.CalendarDate) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if (d.Type == "DATE") != e.AllDay {
			return fmt.Errorf("recurrence date type must match DTSTART")
		}
		_, err := ResolveCalendarDate(d, a.Location(), defs)
		return err
	}
	if r.Start != nil {
		if err := check(*r.Start); err != nil {
			return err
		}
	}
	if r.End != nil {
		if err := check(*r.End); err != nil {
			return err
		}
	}
	if len(r.ExDates)+len(r.RDates)+len(r.Overrides) > 100000 {
		return fmt.Errorf("recurrence set exceeds work limit (100000)")
	}
	for _, d := range r.ExDates {
		if err := check(d); err != nil {
			return err
		}
	}
	for _, p := range r.RDates {
		if err := check(p.Start); err != nil {
			return err
		}
		s, _ := ResolveCalendarDate(p.Start, a.Location(), defs)
		if p.End != nil && p.Duration != "" {
			return fmt.Errorf("RDATE PERIOD end and duration are mutually exclusive")
		}
		if (p.End != nil || p.Duration != "") && e.AllDay {
			return fmt.Errorf("RDATE PERIOD requires DATE-TIME")
		}
		if p.End != nil {
			if p.Start.TZID != p.End.TZID || strings.HasSuffix(p.Start.Value, "Z") != strings.HasSuffix(p.End.Value, "Z") {
				return fmt.Errorf("RDATE PERIOD endpoints must use the same timezone representation")
			}
			if err := check(*p.End); err != nil {
				return err
			}
			end, _ := ResolveCalendarDate(*p.End, a.Location(), defs)
			if !end.After(s) {
				return fmt.Errorf("RDATE PERIOD end must follow start")
			}
		}
		if p.Duration != "" {
			if _, err := parseDuration(p.Duration); err != nil {
				return err
			}
		}
	}
	seen := map[time.Time]bool{}
	for _, o := range r.Overrides {
		if err := check(o.RecurrenceID); err != nil {
			return err
		}
		id, _ := ResolveCalendarDate(o.RecurrenceID, a.Location(), defs)
		id = id.UTC()
		if seen[id] {
			return fmt.Errorf("duplicate RECURRENCE-ID")
		}
		seen[id] = true
		if !o.Cancelled && o.Event == nil {
			return fmt.Errorf("non-cancelled override requires Event")
		}
		if o.Event != nil {
			if o.Event.AllDay != e.AllDay {
				return fmt.Errorf("override DTSTART type must match master")
			}
			if err := validateEvent(ctx, *o.Event, defs, true); err != nil {
				return err
			}
		}
	}
	return nil
}
