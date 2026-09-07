package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/rakunlabs/query"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

type icsStore struct {
	port.CalendarPort
	events   []models.Event
	query    *query.Query
	added    []models.Event
	addCalls int
}

func (db *icsStore) GetEventsWithFunc(_ context.Context, q *query.Query, fn func(models.Event) error) error {
	db.query = q
	for _, event := range db.events {
		if err := fn(event); err != nil {
			return err
		}
	}
	return nil
}

func (db *icsStore) AddEvents(_ context.Context, events []models.Event) error {
	db.addCalls++
	db.added = events
	return nil
}

func exportService(t *testing.T, events ...models.Event) (*CalendarService, *icsStore) {
	t.Helper()
	db := &icsStore{events: events}
	s, err := NewCalendarService(t.Context(), db)
	require.NoError(t, err)
	return s, db
}

func exportQuery(t *testing.T, raw string) *query.Query {
	t.Helper()
	q, err := query.Parse(raw, query.WithSkipExpressionCmp("year"))
	require.NoError(t, err)
	return q
}

func TestGetEventsICSFuncYearsAndMixedRules(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	event := models.Event{
		ID: "holiday", Tz: loc.String(), AllDay: true,
		DateFrom: types.Time{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, loc)},
		DateTo:   types.Time{Time: time.Date(2020, 1, 2, 0, 0, 0, 0, loc)},
		RRule:    "RRULE:FREQ=YEARLY;COUNT=1 FUNC:GoodFriday FUNC:EasterMonday FUNC:GoodFriday",
	}
	s, db := exportService(t, event)
	q := exportQuery(t, "year=2030,2032,2030&entity=team&event_group=holidays")
	got, err := s.GetEventsICS(t.Context(), q)
	require.NoError(t, err)
	require.Same(t, q, db.query)
	require.Equal(t, []string{"team"}, db.query.GetValues("entity"))
	require.Equal(t, []string{"holidays"}, db.query.GetValues("event_group"))
	require.Len(t, db.query.Where, 2)
	require.Len(t, got, 4)
	dates := []string{}
	ids := map[string]bool{}
	for _, instance := range got {
		require.Empty(t, instance.RRule)
		require.True(t, instance.AllDay)
		require.Equal(t, loc, instance.DateFrom.Location())
		require.Equal(t, 0, instance.DateFrom.Hour())
		require.Equal(t, instance.DateFrom.AddDate(0, 0, 1), instance.DateTo.Time)
		require.False(t, ids[instance.ID])
		ids[instance.ID] = true
		dates = append(dates, instance.DateFrom.Format("2006-01-02"))
	}
	require.ElementsMatch(t, []string{"2030-04-19", "2030-04-22", "2032-03-26", "2032-03-29"}, dates)
	again, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2032,2030"))
	require.NoError(t, err)
	require.ElementsMatch(t, got, again)
	ics, err := ical.GenerateICS(got, "team")
	require.NoError(t, err)
	require.NotContains(t, ics, "RRULE:")
	require.NotContains(t, ics, "FUNC:")

	// A matching RRULE must not overwrite the source used to materialize FUNCs.
	db.events[0].RRule = "RRULE:FREQ=YEARLY FUNC:GoodFriday"
	got, err = s.GetEventsICS(t.Context(), q)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, event.DateFrom, got[0].DateFrom)
	require.Equal(t, "FREQ=YEARLY", got[0].RRule)
	require.Empty(t, got[1].RRule)
}

func TestGetEventsICSRecurrenceWindow(t *testing.T) {
	anchor := time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, rule, years string
		start             time.Time
		want              int
	}{
		{"count retains anchor", "RRULE:FREQ=YEARLY;COUNT=3", "2021,2022", anchor, 1},
		{"count exhausted", "RRULE:FREQ=YEARLY;COUNT=3", "2023", anchor, 0},
		{"exclusive upper bound", "RRULE:FREQ=YEARLY;COUNT=1", "2019", anchor.Add(-9 * time.Hour), 0},
		{"exclusive lower end", "RRULE:FREQ=YEARLY;COUNT=1", "2020", anchor.Add(-10 * time.Hour), 0},
		{"gap is not requested", "RRULE:FREQ=YEARLY;COUNT=1", "2019,2021", anchor, 0},
		{"until exhausted", "RRULE:FREQ=YEARLY;UNTIL=20220101T090000Z", "2023", anchor, 0},
		{"later rule matches", "RRULE:FREQ=YEARLY;COUNT=1 RRULE:FREQ=YEARLY;COUNT=3", "2022", anchor, 1},
		{"multiple rules", "RRULE:FREQ=YEARLY;COUNT=2 RRULE:FREQ=YEARLY;COUNT=3", "2021", anchor, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event := models.Event{ID: "series", Tz: "UTC", RRule: tt.rule,
				DateFrom: types.Time{Time: tt.start}, DateTo: types.Time{Time: tt.start.Add(time.Hour)}}
			s, db := exportService(t, event)
			got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year="+tt.years))
			require.NoError(t, err)
			require.Len(t, got, tt.want)
			ids := map[string]bool{}
			for _, series := range got {
				require.Equal(t, event.DateFrom, series.DateFrom)
				require.Equal(t, event.DateTo, series.DateTo)
				require.False(t, ids[series.ID])
				ids[series.ID] = true
			}
			if tt.name == "count retains anchor" {
				require.Equal(t, "series", got[0].ID)
				require.Equal(t, "FREQ=YEARLY;COUNT=3", got[0].RRule)
				ics, err := ical.GenerateICS(got, "")
				require.NoError(t, err)
				require.NoError(t, s.AddIcal(t.Context(), strings.NewReader(ics), nil, types.Null[string]{}, "importer"))
				rule, err := ical.ParseRepeat(db.added[0].RRule)
				require.NoError(t, err)
				_, _, match := ical.MatchRRuleBetween(rule.RRule[0], got[0].DateFrom.Time, got[0].DateTo.Time, anchor.AddDate(3, 0, 0), anchor.AddDate(4, 0, 0))
				require.False(t, match, "round trip must not invent future occurrences")
			}
		})
	}
}

func TestAddIcalExportRoundTrip(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	for _, allDay := range []bool{false, true} {
		start := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
		event := models.Event{ID: "imported", Name: "Round trip", Tz: loc.String(), AllDay: allDay,
			DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(0, 0, 2)},
			RRule: "RRULE:FREQ=YEARLY;COUNT=2"}
		s, db := exportService(t, event)
		got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2027"))
		require.NoError(t, err)
		data, err := ical.GenerateICS(got, "")
		require.NoError(t, err)
		group := types.Null[string]{V: "import-group", Valid: true}
		require.NoError(t, s.AddIcal(t.Context(), strings.NewReader(data), loc, group, "user"))
		event.EventGroup, event.UpdatedBy = group, "user"
		require.Equal(t, []models.Event{event}, db.added)
	}
	s, db := exportService(t)
	err = s.AddIcal(t.Context(), strings.NewReader("BEGIN:VEVENT\nDTSTART:bad\nEND:VEVENT"), nil, types.Null[string]{}, "user")
	require.Error(t, err)
	require.Nil(t, db.added)
}

func TestGetEventsICSNonRecurringAndErrors(t *testing.T) {
	start := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	event := models.Event{ID: "spanning", Tz: "UTC", AllDay: true,
		DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(0, 0, 2)}}
	disabled := event
	disabled.Disabled = true
	s, db := exportService(t, event, disabled)
	got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2026,2026"))
	require.NoError(t, err)
	require.Equal(t, []models.Event{event}, got)
	for _, year := range []string{"bad", "0", "10000"} {
		_, err := s.GetEventsICS(t.Context(), exportQuery(t, "year="+year))
		require.Error(t, err)
	}
	db.events[0].Tz = "Invalid/Zone"
	_, err = s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
	require.ErrorContains(t, err, "timezone")
}

func TestAddIcalRejectsInvalidDurationBeforePersistence(t *testing.T) {
	valid := "BEGIN:VEVENT\r\nUID:valid\r\nDTSTART;VALUE=DATE:20260329\r\nEND:VEVENT\r\n"
	for _, dates := range []string{
		"DTSTART:20260101T090000Z",
		"DTSTART:20260101T090000Z\r\nDTEND:20260101T090000Z",
		"DTSTART:20260101T090000Z\r\nDTEND:20260101T080000Z",
		"DTSTART;VALUE=DATE:20260101\r\nDTEND;VALUE=DATE:20260101",
		"DTSTART;VALUE=DATE:20260102\r\nDTEND;VALUE=DATE:20260101",
		"DTSTART:20260101T090000Z\r\nDURATION:PT1H",
		"DTSTART;VALUE=DATE:20260101\r\nDURATION:P2D",
		"DTSTART:20260101T090000Z\r\nDTEND:20260101T100000Z\r\nDURATION:PT1H",
	} {
		t.Run(dates, func(t *testing.T) {
			s, db := exportService(t)
			data := "BEGIN:VCALENDAR\r\n" + valid + "BEGIN:VEVENT\r\nUID:invalid\r\n" + dates + "\r\nEND:VEVENT\r\nEND:VCALENDAR"
			err := s.AddIcal(t.Context(), strings.NewReader(data), nil, types.Null[string]{}, "user")
			require.ErrorContains(t, err, "failed to parse ics: event invalid:")
			require.Zero(t, db.addCalls)
			require.Nil(t, db.added)
		})
	}

	// An implicit all-day end remains the next local midnight, including across DST.
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	s, db := exportService(t)
	require.NoError(t, s.AddIcal(t.Context(), strings.NewReader(valid), loc, types.Null[string]{}, "user"))
	require.Equal(t, 1, db.addCalls)
	require.Len(t, db.added, 1)
	event := db.added[0]
	require.True(t, event.AllDay)
	require.Equal(t, event.DateFrom.AddDate(0, 0, 1), event.DateTo.Time)
	require.Equal(t, 23*time.Hour, event.DateTo.Sub(event.DateFrom.Time))
}
