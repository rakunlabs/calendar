package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/cache"
	"github.com/rakunlabs/cache/store/memory"
	"github.com/rakunlabs/query"
	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
)

type CalendarService struct {
	db        port.CalendarPort
	cacheRule cache.Cacher[string, *ical.Repeat]
	cacheTZ   cache.Cacher[string, *time.Location]
	m         sync.RWMutex
}

var _ port.CalendarService = (*CalendarService)(nil)

func NewCalendarService(ctx context.Context, db port.CalendarPort) (*CalendarService, error) {
	cacheRule, err := cache.New[string, *ical.Repeat](ctx,
		memory.Store,
		cache.WithStoreConfig(&memory.Config{
			MaxItems: 200,
			TTL:      30 * time.Minute,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cacheRule: %w", err)
	}

	cacheTZ, err := cache.New[string, *time.Location](ctx,
		memory.Store,
		cache.WithStoreConfig(&memory.Config{
			MaxItems: 200,
			TTL:      30 * time.Minute,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cacheTZ: %w", err)
	}

	return &CalendarService{
		cacheRule: cacheRule,
		cacheTZ:   cacheTZ,
		db:        db,
	}, nil
}

// WorkDay returns the next workday after the given date.
func (s *CalendarService) WorkDay(ctx context.Context, date types.Time) (types.Time, error) {
	return types.Time{Time: time.Now()}, nil
}

// //////////////////////////////////////////////////////////////
// Database
// //////////////////////////////////////////////////////////////

func (s *CalendarService) AddEvents(ctx context.Context, events []models.Event) error {
	for i := range events {
		events[i].RecurrenceID = nil
		events[i].IsOverride = false
		if err := validatePersistedEvent(events[i]); err != nil {
			return fmt.Errorf("%w: %w", port.ErrInvalidEvent, err)
		}
	}
	if err := s.db.AddEvents(ctx, events); err != nil {
		return err
	}

	return nil
}

func (s *CalendarService) RemoveEvent(ctx context.Context, id ...string) error {
	err := s.db.RemoveEvent(ctx, id...)
	if err != nil {
		return err
	}

	return nil
}

func (s *CalendarService) GetEventsCount(ctx context.Context, q *query.Query) (uint64, error) {
	count, err := s.db.GetEventsCount(ctx, q)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (s *CalendarService) GetEvents(ctx context.Context, q *query.Query) ([]models.Event, error) {
	if q.HasAny("date") {
		var events []models.Event

		qDateCheck := types.Time{}
		if qDate, _ := q.Values["date"]; len(qDate) > 0 {
			qDateStr, ok := qDate[0].Value.(string)
			if !ok {
				return nil, fmt.Errorf("invalid date format")
			}
			if err := qDateCheck.Parse(qDateStr); err != nil {
				return nil, fmt.Errorf("invalid date format: %w", err)
			}
		}

		err := s.db.GetEventsWithFunc(ctx, q, func(h models.Event) error {
			if h.Disabled {
				return nil
			}

			// A one-nanosecond window gives point-in-time, end-exclusive matching.
			instances, err := ical.Occurrences(ctx, h, qDateCheck.Time, qDateCheck.Add(time.Nanosecond))
			if err != nil {
				return err
			}
			events = append(events, instances...)

			return nil
		})
		if err != nil {
			return nil, err
		}

		return events, nil
	}

	events, err := s.db.GetEvents(ctx, q)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func (s *CalendarService) GetEventsICS(ctx context.Context, q *query.Query) ([]models.Event, error) {
	var events []models.Event

	qYearCheck := []int{}
	if qYear, _ := q.Values["year"]; len(qYear) > 0 {
		for _, v := range qYear {
			if v.Value == nil {
				continue
			}

			var years []string
			switch value := v.Value.(type) {
			case string:
				years = []string{value}
			case []string:
				years = value
			default:
				return nil, fmt.Errorf("invalid year format")
			}

			for _, year := range years {
				qYearInt, err := strconv.Atoi(year)
				if err != nil {
					return nil, fmt.Errorf("invalid year format: %w", err)
				}
				if qYearInt < 1 || qYearInt > 9999 {
					return nil, fmt.Errorf("year must be between 1 and 9999")
				}

				qYearCheck = append(qYearCheck, qYearInt)
			}
		}
	}

	if len(qYearCheck) == 0 {
		year := time.Now().Year()

		qYearCheck = append(qYearCheck, year-1, year, year+1, year+2)
	}

	err := s.db.GetEventsWithFunc(ctx, q, func(h models.Event) error {
		if h.Disabled {
			return nil
		}

		if err := s.tzTime(&h); err != nil {
			return err
		}
		if h.Recurrence != nil {
			materialize := false
			if strings.TrimSpace(h.RRule) != "" {
				repeat, err := s.getRRule(ctx, h.RRule)
				if err != nil {
					return err
				}
				materialize = len(repeat.Func) > 0 || len(repeat.RRule) > 1
			}
			seen := map[string]bool{}
			for _, year := range qYearCheck {
				from := time.Date(year, 1, 1, 0, 0, 0, 0, h.DateFrom.Location())
				if !materialize {
					found, err := ical.HasOccurrence(ctx, h, from, from.AddDate(1, 0, 0))
					if err != nil {
						return err
					}
					if found {
						events = append(events, h)
						break
					}
					continue
				}
				// Extended sets cannot be split without changing exception semantics.
				instances, err := ical.Occurrences(ctx, h, from, from.AddDate(1, 0, 0))
				if err != nil {
					return err
				}
				for _, instance := range instances {
					key := instance.DateFrom.Time
					if instance.RecurrenceID != nil {
						key, err = ical.ResolveCalendarDate(*instance.RecurrenceID, h.DateFrom.Location(), h.Recurrence.Timezones)
						if err != nil {
							return err
						}
					}
					instance.ID = fmt.Sprintf("%s-instance-%x", h.ID, sha256.Sum256([]byte(key.UTC().Format(time.RFC3339Nano))))
					if seen[instance.ID] {
						continue
					}
					seen[instance.ID] = true
					instance.RRule, instance.Recurrence, instance.RecurrenceID, instance.IsOverride = "", nil, nil, false
					// Standalone instances no longer carry calendar-scoped timezone definitions.
					instance.Tz = "UTC"
					for _, date := range []*types.Time{&instance.DateFrom, &instance.DateTo} {
						if instance.AllDay {
							date.Time = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
						} else {
							date.Time = date.UTC()
						}
					}
					events = append(events, instance)
				}
			}
			return nil
		}

		if strings.TrimSpace(h.RRule) == "" {
			for _, year := range qYearCheck {
				from := time.Date(year, 1, 1, 0, 0, 0, 0, h.DateFrom.Location())
				if h.DateFrom.Before(from.AddDate(1, 0, 0)) && h.DateTo.After(from) {
					events = append(events, h)
					break
				}
			}

			return nil
		}

		icsRepeat, err := s.getRRule(ctx, h.RRule)
		if err != nil {
			return fmt.Errorf("failed to get rrule: %w", err)
		}

		seen := map[string]bool{}
		for _, rrule := range icsRepeat.RRule {
			for _, year := range qYearCheck {
				from := time.Date(year, 1, 1, 0, 0, 0, 0, h.DateFrom.Location())
				candidate := h
				candidate.RRule = rrule.Org()
				found, err := ical.HasOccurrence(ctx, candidate, from, from.AddDate(1, 0, 0))
				if err != nil {
					return err
				}
				if !found {
					continue
				}
				// Keep DTSTART and COUNT together: moving the anchor invents occurrences.
				series := h
				series.RRule = rrule.Org()
				if len(icsRepeat.RRule) > 1 {
					series.ID = fmt.Sprintf("%s-rrule-%x", h.ID, sha256.Sum256([]byte(series.RRule)))
				}
				if !seen[series.ID] {
					seen[series.ID] = true
					events = append(events, series)
				}
				break
			}
		}

		if len(icsRepeat.Func) > 0 {
			// Materialize only FUNCs; RRULEs above retain their original series anchors.
			var funcs []string
			for _, part := range strings.Fields(h.RRule) {
				if strings.HasPrefix(strings.ToUpper(part), "FUNC:") {
					funcs = append(funcs, part)
				}
			}
			funcEvent := h
			funcEvent.RRule = strings.Join(funcs, " ")
			for _, year := range qYearCheck {
				from := time.Date(year, 1, 1, 0, 0, 0, 0, h.DateFrom.Location())
				instances, err := ical.Occurrences(ctx, funcEvent, from, from.AddDate(1, 0, 0))
				if err != nil {
					return err
				}
				for _, instance := range instances {
					instance.RRule = ""
					instance.ID = h.ID + "-func-" + instance.DateFrom.Format("20060102")
					if instance.DateFrom.Year() == year && !seen[instance.ID] {
						seen[instance.ID] = true
						events = append(events, instance)
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return events, nil
}

func (s *CalendarService) tzTime(h *models.Event) error {
	if h == nil {
		return nil
	}
	var definitions []string
	if h.Recurrence != nil {
		definitions = h.Recurrence.Timezones
	}
	tzLoc, err := ical.ResolveLocation(h.Tz, definitions)
	if err != nil {
		return fmt.Errorf("failed to get timezone location: %w", err)
	}

	h.DateFrom = types.Time{Time: h.DateFrom.In(tzLoc)}
	h.DateTo = types.Time{Time: h.DateTo.In(tzLoc)}

	return nil
}

func (s *CalendarService) GetEvent(ctx context.Context, id string) (*models.Event, error) {
	h, err := s.db.GetEvent(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.tzTime(h); err != nil {
		return nil, err
	}

	return h, nil
}

func (s *CalendarService) UpdateEvent(ctx context.Context, id string, event *models.Event) error {
	if event == nil {
		return port.ErrInvalidEvent
	}
	current, err := s.db.GetEvent(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("%w: event no longer exists", port.ErrConflict)
	}
	updated := *event
	updated.RecurrenceID = nil
	updated.IsOverride = false
	if current.Recurrence != nil {
		if updated.Recurrence == nil {
			updated.Recurrence = current.Recurrence
			// Old clients omit metadata; preserve the version just read if they also omit it.
			if updated.UpdatedAt.IsZero() {
				updated.UpdatedAt = current.UpdatedAt
			}
		}
		r := current.Recurrence
		if len(r.Overrides)+len(r.ExDates)+len(r.RDates) > 0 &&
			(!current.DateFrom.Equal(updated.DateFrom.Time) || !current.DateTo.Equal(updated.DateTo.Time) ||
				current.Tz != updated.Tz || current.AllDay != updated.AllDay || current.RRule != updated.RRule ||
				!reflect.DeepEqual(r.Start, updated.Recurrence.Start) || !reflect.DeepEqual(r.End, updated.Recurrence.End) ||
				r.Duration != updated.Recurrence.Duration || !reflect.DeepEqual(r.Timezones, updated.Recurrence.Timezones)) {
			return fmt.Errorf("%w: cannot change series dates, timezone or rule while exceptions exist", port.ErrConflict)
		}
	}
	if updated.Recurrence != nil && (updated.UpdatedAt.IsZero() || !updated.UpdatedAt.Equal(current.UpdatedAt.Time)) {
		return fmt.Errorf("%w: reload the event before editing recurrence", port.ErrConflict)
	}
	if err := validatePersistedEvent(updated); err != nil {
		return fmt.Errorf("%w: %w", port.ErrInvalidEvent, err)
	}
	err = s.db.UpdateEvent(ctx, id, &updated)
	if err != nil {
		return err
	}
	*event = updated

	return nil
}

// Persisted projections must agree with the lexical dates used by expansion/export.
func validatePersistedEvent(event models.Event) error {
	if err := ical.ValidateEvent(event); err != nil {
		return err
	}
	var check func(models.Event, []string) error
	check = func(e models.Event, definitions []string) error {
		if e.Recurrence == nil {
			return nil
		}
		r := e.Recurrence
		definitions = append(append([]string{}, definitions...), r.Timezones...)
		timing := *r
		timing.Timezones = definitions
		e.Recurrence = &timing
		start, end, err := ical.EventTimes(e)
		if err != nil {
			return err
		}
		if !start.Equal(e.DateFrom.Time) || !end.Equal(e.DateTo.Time) {
			return fmt.Errorf("date_from/date_to must match effective recurrence start/end")
		}
		for _, override := range r.Overrides {
			if override.Event != nil {
				if err := check(*override.Event, definitions); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return check(event, nil)
}

// ///////////////////////////////////////////////////////////////
// Relations
// ///////////////////////////////////////////////////////////////

func (s *CalendarService) AddRelations(ctx context.Context, relations []models.Relation) error {
	if err := s.db.AddRelations(ctx, relations); err != nil {
		return err
	}

	return nil
}

func (s *CalendarService) RemoveRelation(ctx context.Context, q *query.Query) error {
	err := s.db.RemoveRelation(ctx, q)
	if err != nil {
		return err
	}

	return nil
}

func (s *CalendarService) GetRelations(ctx context.Context, q *query.Query) ([]models.Relation, error) {
	relations, err := s.db.GetRelations(ctx, q)
	if err != nil {
		return nil, err
	}

	return relations, nil
}

func (s *CalendarService) GetRelationsCount(ctx context.Context, q *query.Query) (uint64, error) {
	count, err := s.db.GetRelationsCount(ctx, q)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// ///////////////////////////////////////////////////////////////
// iCal
// ///////////////////////////////////////////////////////////////

func (s *CalendarService) AddIcal(ctx context.Context, data io.Reader, tz *time.Location, group types.Null[string], updatedBy string) error {
	events, err := ical.ParseICS(data, tz)
	if err != nil {
		return fmt.Errorf("failed to parse ics: %w", err)
	}

	for i := range events {
		events[i].EventGroup = group
		events[i].UpdatedBy = updatedBy
	}

	if err := s.AddEvents(ctx, events); err != nil {
		return fmt.Errorf("failed to add events: %w", err)
	}

	return nil
}
