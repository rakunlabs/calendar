package ical

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/worldline-go/types"
)

// Occurrences expands an event into instances overlapping [from, to).
// IDs continue to identify the original event, so editing an instance edits its series.
// More than 20000 results returns an error, never a truncated calendar.
// Response Recurrence contains only effective instance timing. Timezone definitions
// and the recurrence graph remain on the master; RecurrenceID retains identity.
func Occurrences(ctx context.Context, event models.Event, from, to time.Time) ([]models.Event, error) {
	return occurrences(ctx, event, from, to, 20001)
}

// HasOccurrence reports whether an effective instance overlaps [from, to).
// It validates the complete event, then stops expansion at the first surviving
// instance, including moved-in overrides and excluding cancelled instances.
func HasOccurrence(ctx context.Context, event models.Event, from, to time.Time) (bool, error) {
	result, err := occurrences(ctx, event, from, to, 1)
	return len(result) != 0, err
}

func occurrences(ctx context.Context, event models.Event, from, to time.Time, limit int) ([]models.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("query end must be after its start")
	}
	if err := validateEvent(ctx, event, nil, false); err != nil {
		return nil, err
	}
	var definitions []string
	if event.Recurrence != nil {
		definitions = event.Recurrence.Timezones
	}
	start, end, nominal, err := eventTiming(event, nil)
	if err != nil {
		return nil, err
	}
	loc := start.Location()
	result := []models.Event{}
	finish := func() ([]models.Event, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(result) > 20000 {
			return nil, fmt.Errorf("event %s exceeds occurrence result limit (20000)", event.ID)
		}
		slices.SortFunc(result, func(a, b models.Event) int { return a.DateFrom.Compare(b.DateFrom.Time) })
		return result, nil
	}
	seen := map[time.Time]bool{}
	excluded := map[time.Time]bool{}
	identities := map[time.Time]models.CalendarDate{}
	if event.Recurrence != nil {
		for _, d := range event.Recurrence.ExDates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			t, _ := ResolveCalendarDate(d, loc, definitions)
			excluded[t.UTC()] = true
		}
		for _, o := range event.Recurrence.Overrides {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, _ := ResolveCalendarDate(o.RecurrenceID, loc, definitions)
			excluded[id.UTC()] = true
			if o.Cancelled {
				continue
			}
			copy := *o.Event
			a, b, _, err := eventTiming(copy, definitions)
			if err != nil {
				return nil, err
			}
			if a.Before(to) && b.After(from) {
				copy.ID = event.ID
				copy.EventGroup = event.EventGroup
				copy.Disabled = event.Disabled || copy.Disabled
				copy.RecurrenceID = &o.RecurrenceID
				copy.IsOverride = true
				copy.DateFrom, copy.DateTo = types.Time{Time: a}, types.Time{Time: b}
				copy.Recurrence = occurrenceTiming(copy.Recurrence, a, b, copy.AllDay, true)
				result = append(result, copy)
				if len(result) >= limit {
					return finish()
				}
			}
		}
	}
	appendOccurrence := func(a, b time.Time, period *models.RecurrencePeriod) {
		if seen[a.UTC()] || excluded[a.UTC()] {
			return
		}
		seen[a.UTC()] = true
		if a.Before(to) && b.After(from) {
			copy := event
			copy.DateFrom, copy.DateTo = types.Time{Time: a}, types.Time{Time: b}
			id, ok := identities[a.UTC()]
			if !ok {
				id = CalendarDateFromTime(a, event.AllDay)
				if event.Recurrence != nil && event.Recurrence.Start != nil {
					template := event.Recurrence.Start
					id.Type = template.Type
					id.TZID = template.TZID
					if template.TZID == "" && !strings.HasSuffix(template.Value, "Z") && !event.AllDay {
						id.Value = a.Format("20060102T150405")
					}
				}
			}
			copy.RecurrenceID = &id
			copy.IsOverride = false
			copy.Recurrence = occurrenceTiming(event.Recurrence, a, b, event.AllDay, a.Equal(start))
			if period != nil {
				date := period.Start
				copy.Recurrence.Start = &date
				if period.End != nil {
					date := *period.End
					copy.Recurrence.End, copy.Recurrence.Duration = &date, ""
				} else if period.Duration != "" {
					copy.Recurrence.End, copy.Recurrence.Duration = nil, period.Duration
				}
			}
			result = append(result, copy)
		}
	}
	if event.Recurrence != nil {
		if event.Recurrence.Start != nil {
			identities[start.UTC()] = *event.Recurrence.Start
		}
		// Explicit periods take precedence over the inherited series duration.
		for _, periods := range []bool{true, false} {
			for _, p := range event.Recurrence.RDates {
				if (p.End != nil || p.Duration != "") != periods {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				a, _ := ResolveCalendarDate(p.Start, loc, definitions)
				b := nominal.end(a)
				if p.End != nil {
					b, _ = ResolveCalendarDate(*p.End, loc, definitions)
				} else if p.Duration != "" {
					d, _ := parseDuration(p.Duration)
					b = d.end(a)
				}
				if _, exists := identities[a.UTC()]; !exists {
					identities[a.UTC()] = p.Start
				}
				appendOccurrence(a, b, &p)
				if len(result) >= limit {
					return finish()
				}
			}
		}
	}
	if strings.TrimSpace(event.RRule) == "" {
		appendOccurrence(start, end, nil)
		return finish()
	}
	repeat, err := ParseRepeat(event.RRule)
	if err != nil {
		return nil, fmt.Errorf("event %s recurrence: %w", event.ID, err)
	}
	// Legacy FUNC-only events use DTSTART as a lower bound, not an instance.
	if len(repeat.RRule) > 0 {
		appendOccurrence(start, end, nil)
		if len(result) >= limit {
			return finish()
		}
	}
	lookback := from.In(loc).Add(-nominal.exact).AddDate(0, 0, -nominal.days)
	if nominal.days > 0 {
		// Civil subtraction near a timezone discontinuity is not always the
		// inverse of addition. Keep a conservative extra day for overlap checks.
		lookback = lookback.AddDate(0, 0, -1)
	}
	for _, rule := range repeat.RRule {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if rule.Interval < 1 || rule.Count != nil && *rule.Count < 1 {
			return nil, fmt.Errorf("event %s: recurrence interval and count must be positive", event.ID)
		}
		if event.AllDay {
			copy := *rule
			copy.ByHour, copy.ByMinute, copy.BySecond = nil, nil, nil
			// Without date selectors each period has one inherited date. Apply
			// BYSETPOS here so iterator validation does not reject the stripped rule.
			if len(copy.BySetPos) > 0 && len(copy.ByDay)+len(copy.ByMonthDay)+len(copy.ByYearDay)+len(copy.ByWeekNo)+len(copy.ByMonth) == 0 {
				if !slices.Contains(copy.BySetPos, 1) && !slices.Contains(copy.BySetPos, -1) {
					continue
				}
				copy.BySetPos = nil
			}
			rule = &copy
		}
		err := walkRRuleFrom(ctx, rule, start, lookback, to, func(candidate time.Time) bool {
			if !candidate.Before(to) {
				return false
			}
			stop := nominal.end(candidate)
			appendOccurrence(candidate, stop, nil)
			return len(result) < limit
		})
		if err != nil {
			return nil, fmt.Errorf("event %s recurrence: %w", event.ID, err)
		}
		if len(result) >= limit {
			return finish()
		}
	}
	firstYear := max(start.Year(), lookback.Year())
	lastYear := to.In(loc).Year()
	budget := 100000
	for _, fn := range repeat.Func {
		for year := firstYear; year <= lastYear; year++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if budget == 0 {
				return nil, fmt.Errorf("event %s: FUNC exceeds expansion/work limit (100000)", event.ID)
			}
			budget--
			date := fn(year)
			a := time.Date(date.Year(), date.Month(), date.Day(), start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), loc)
			if a.Before(start) {
				continue
			}
			b := nominal.end(a)
			appendOccurrence(a, b, nil)
			if len(result) >= limit {
				return finish()
			}
		}
	}
	return finish()
}

// Preserve lexical timing only for the source instance (including leap seconds).
// Generated instances encode their effective dates rather than the master dates.
func occurrenceTiming(source *models.Recurrence, a, b time.Time, allDay, original bool) *models.Recurrence {
	start, end := CalendarDateFromTime(a, allDay), CalendarDateFromTime(b, allDay)
	// A computed end may land in the second fold, which local lexical time
	// cannot identify. Encode that instant in UTC instead of changing its meaning.
	endInFirstFold := firstRRuleLocalTime(b).Equal(b)
	if !allDay && !endInFirstFold {
		end = CalendarDateFromTime(b.UTC(), false)
	}
	if source != nil {
		if source.Start != nil {
			if original {
				start = *source.Start
			} else if source.Start.TZID == "" && !allDay && !strings.HasSuffix(source.Start.Value, "Z") {
				start = models.CalendarDate{Value: a.Format("20060102T150405"), Type: source.Start.Type}
			}
		}
		if source.Duration != "" {
			return &models.Recurrence{Start: &start, Duration: source.Duration}
		}
		if original && source.End != nil {
			end = *source.End
		} else if !allDay && endInFirstFold && start.TZID == "" && !strings.HasSuffix(start.Value, "Z") && (source.End == nil || source.End.TZID == "" && !strings.HasSuffix(source.End.Value, "Z")) {
			end = models.CalendarDate{Value: b.Format("20060102T150405")}
		}
	}
	return &models.Recurrence{Start: &start, End: &end}
}
