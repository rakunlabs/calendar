package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/rakunlabs/calendar/pkg/ical"
)

func (s *CalendarService) getRRule(ctx context.Context, repeatStr string) (*ical.Repeat, error) {
	s.m.RLock()
	rrule, ok, err := s.cacheRule.Get(ctx, repeatStr)
	s.m.RUnlock()
	if err != nil {
		slog.ErrorContext(ctx, "failed to get rrule from cache", "error", err)
	}

	if ok {
		return rrule, nil
	}

	// Only one goroutine should parse and set for a given rruleStr at a time
	s.m.Lock()
	defer s.m.Unlock()
	// Double-check cache after acquiring write lock (to avoid race)
	rrule, ok, err = s.cacheRule.Get(ctx, repeatStr)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get rrule from cache (after lock)", "error", err)
	}
	if ok {
		return rrule, nil
	}

	rrule, err = ical.ParseRepeat(repeatStr)
	if err != nil {
		return nil, err
	}

	if err := s.cacheRule.Set(ctx, repeatStr, rrule); err != nil {
		slog.ErrorContext(ctx, "failed to set rrule in cache", "error", err)
	}

	return rrule, nil
}

func (s *CalendarService) TZLocation(tz string) (*time.Location, error) {
	s.m.RLock()
	loc, ok, err := s.cacheTZ.Get(context.Background(), tz)
	s.m.RUnlock()
	if err != nil {
		slog.Error("failed to get location from cache", "error", err)
	}

	if ok {
		return loc, nil
	}

	// Only one goroutine should parse and set for a given tz at a time
	s.m.Lock()
	defer s.m.Unlock()
	// Double-check cache after acquiring write lock (to avoid race)
	loc, ok, err = s.cacheTZ.Get(context.Background(), tz)
	if err != nil {
		slog.Error("failed to get location from cache (after lock)", "error", err)
	}
	if ok {
		return loc, nil
	}

	loc, err = time.LoadLocation(tz)
	if err != nil {
		return nil, err
	}
	if err := s.cacheTZ.Set(context.Background(), tz, loc); err != nil {
		slog.Error("failed to set location in cache", "error", err)
	}

	return loc, nil
}
