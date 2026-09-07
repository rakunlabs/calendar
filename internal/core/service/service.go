package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
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

			s.tzTime(&h)

			if strings.TrimSpace(h.RRule) == "" {
				if !qDateCheck.Time.Before(h.DateFrom.Time) && qDateCheck.Time.Before(h.DateTo.Time) {
					events = append(events, h)
				}

				return nil
			}

			icsRepeat, err := s.getRRule(ctx, h.RRule)
			if err != nil {
				return fmt.Errorf("failed to get rrule: %w", err)
			}

			for _, rrule := range icsRepeat.RRule {
				start, stop, ok := ical.MatchRRuleAt(rrule, h.DateFrom.Time, h.DateTo.Time, qDateCheck.Time)
				if !ok {
					return nil
				}

				h.DateFrom.Time = start
				h.DateTo.Time = stop

				events = append(events, h)
			}

			for _, yearFn := range icsRepeat.Func {
				newDate := yearFn(qDateCheck.Year())
				h.DateFrom.Time = newDate
				h.DateTo.Time = h.DateFrom.Time.AddDate(0, 0, 1)

				if !qDateCheck.Time.Before(h.DateFrom.Time) && qDateCheck.Time.Before(h.DateTo.Time) {
					events = append(events, h)
				}
			}

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
				// MatchRRuleBetween is inclusive; ICS year windows are [from, to).
				_, _, ok := ical.MatchRRuleBetween(rrule, h.DateFrom.Time, h.DateTo.Time, from.Add(time.Nanosecond), from.AddDate(1, 0, 0).Add(-time.Nanosecond))
				if !ok {
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

		for _, yearFn := range icsRepeat.Func {
			for _, year := range qYearCheck {
				newDate := yearFn(year)
				instance := h
				instance.DateFrom.Time = time.Date(newDate.Year(), newDate.Month(), newDate.Day(), 0, 0, 0, 0, h.DateFrom.Location())
				instance.DateTo.Time = instance.DateFrom.AddDate(0, 0, 1)
				instance.RRule = ""
				instance.AllDay = true
				instance.ID = h.ID + "-func-" + instance.DateFrom.Format("20060102")

				if instance.DateFrom.Year() == year && !seen[instance.ID] {
					seen[instance.ID] = true
					events = append(events, instance)
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
	tzLoc, err := s.TZLocation(h.Tz)
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

	s.tzTime(h)

	return h, nil
}

func (s *CalendarService) UpdateEvent(ctx context.Context, id string, event *models.Event) error {
	err := s.db.UpdateEvent(ctx, id, event)
	if err != nil {
		return err
	}

	return nil
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

	if err := s.db.AddEvents(ctx, events); err != nil {
		return fmt.Errorf("failed to add events: %w", err)
	}

	return nil
}
