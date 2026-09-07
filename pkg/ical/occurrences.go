package ical

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
	rrule "github.com/teambition/rrule-go"
	"github.com/worldline-go/types"
)

// Occurrences expands an event into instances overlapping [from, to).
// IDs continue to identify the original event, so editing an instance edits its series.
func Occurrences(ctx context.Context, event models.Event, from, to time.Time) ([]models.Event, error) {
	loc := time.UTC
	if event.Tz != "" {
		var err error
		loc, err = time.LoadLocation(event.Tz)
		if err != nil {
			return nil, fmt.Errorf("event %s timezone: %w", event.ID, err)
		}
	}
	start, end := event.DateFrom.In(loc), event.DateTo.In(loc)
	if !end.After(start) {
		return nil, fmt.Errorf("event %s ends before or at its start", event.ID)
	}
	result := []models.Event{}
	seen := map[int64]bool{}
	appendOccurrence := func(a, b time.Time) {
		if a.Before(to) && b.After(from) && !seen[a.UnixNano()] {
			seen[a.UnixNano()] = true
			copy := event
			copy.DateFrom, copy.DateTo = types.Time{Time: a}, types.Time{Time: b}
			result = append(result, copy)
		}
	}
	if strings.TrimSpace(event.RRule) == "" {
		appendOccurrence(start, end)
		return result, nil
	}
	repeat, err := ParseRepeat(event.RRule)
	if err != nil {
		return nil, fmt.Errorf("event %s recurrence: %w", event.ID, err)
	}
	duration := end.Sub(start)
	// All-day recurrences retain their calendar-day length across DST changes.
	dayCount := int(time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).Sub(time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
	for _, rule := range repeat.RRule {
		if !slices.Contains([]string{"DAILY", "WEEKLY", "MONTHLY", "YEARLY"}, rule.Freq) {
			return nil, fmt.Errorf("event %s: calendar supports daily, weekly, monthly and yearly recurrence", event.ID)
		}
		if rule.Interval < 1 || rule.Count != nil && *rule.Count < 1 {
			return nil, fmt.Errorf("event %s: recurrence interval and count must be positive", event.ID)
		}
		options, err := rrule.StrToROption(rule.Org())
		if err != nil {
			return nil, fmt.Errorf("event %s recurrence: %w", event.ID, err)
		}
		options.Dtstart = start
		expander, err := rrule.NewRRule(*options)
		if err != nil {
			return nil, fmt.Errorf("event %s recurrence: %w", event.ID, err)
		}
		next := expander.Iterator()
		for steps := 0; ; steps++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if steps >= 100000 {
				return nil, fmt.Errorf("event %s: recurrence exceeds expansion limit", event.ID)
			}
			candidate, ok := next()
			if !ok || !candidate.Before(to) {
				break
			}
			stop := candidate.Add(duration)
			if event.AllDay && dayCount > 0 {
				stop = candidate.AddDate(0, 0, dayCount)
			}
			appendOccurrence(candidate, stop)
		}
	}
	for _, fn := range repeat.Func {
		for year := from.In(loc).Year() - 1; year <= to.In(loc).Year(); year++ {
			date := fn(year)
			a := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
			appendOccurrence(a, a.AddDate(0, 0, 1))
		}
	}
	slices.SortFunc(result, func(a, b models.Event) int { return a.DateFrom.Compare(b.DateFrom.Time) })
	return result, nil
}
