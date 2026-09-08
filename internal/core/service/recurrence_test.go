package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

type editStore struct {
	port.CalendarPort
	event  *models.Event
	writes int
	err    error
}

func (db *editStore) GetEvent(context.Context, string) (*models.Event, error) {
	return db.event, nil
}

func (db *editStore) UpdateEvent(_ context.Context, _ string, event *models.Event) error {
	if db.err != nil {
		return db.err
	}
	db.writes++
	copy := *event
	db.event = &copy
	return nil
}

func recurrenceMaster() models.Event {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	return models.Event{ID: "master", Tz: "UTC", Name: "series", RRule: "FREQ=DAILY;COUNT=2",
		DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)},
		UpdatedAt: types.Time{Time: start}, EventGroup: types.NewNull("group"), Disabled: true,
		Recurrence: &models.Recurrence{
			Start: &models.CalendarDate{Value: "20260101T090000Z"}, End: &models.CalendarDate{Value: "20260101T100000Z"},
			Overrides: []models.OccurrenceOverride{{RecurrenceID: models.CalendarDate{Value: "20260102T090000Z"}, Cancelled: true}},
		}}
}

func TestUpdateRecurrencePreservationAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*models.Event)
		want error
	}{
		{"legacy", func(e *models.Event) { e.Recurrence = nil; e.Name = "renamed" }, nil},
		{"legacy without version", func(e *models.Event) { e.Recurrence = nil; e.UpdatedAt = types.Time{} }, nil},
		{"stale version", func(e *models.Event) { e.UpdatedAt.Time = e.UpdatedAt.Add(-time.Second) }, port.ErrConflict},
		{"missing version", func(e *models.Event) { e.UpdatedAt = types.Time{} }, port.ErrConflict},
		{"date", func(e *models.Event) { e.DateFrom.Time = e.DateFrom.Add(time.Hour) }, port.ErrConflict},
		{"end", func(e *models.Event) { e.DateTo.Time = e.DateTo.Add(time.Hour) }, port.ErrConflict},
		{"timezone", func(e *models.Event) { e.Tz = "Europe/Berlin" }, port.ErrConflict},
		{"rule", func(e *models.Event) { e.RRule = "FREQ=WEEKLY" }, port.ErrConflict},
		{"reset", func(e *models.Event) { r := *e.Recurrence; r.Overrides = nil; e.Recurrence = &r }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master := recurrenceMaster()
			db := &editStore{event: &master}
			s := &CalendarService{db: db}
			updated := master
			tc.edit(&updated)
			updated.RecurrenceID = master.Recurrence.Start
			updated.IsOverride = true
			err := s.UpdateEvent(t.Context(), master.ID, &updated)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, db.writes)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, db.writes)
			require.NotNil(t, updated.Recurrence)
			require.Nil(t, updated.RecurrenceID)
			require.False(t, updated.IsOverride)
			require.True(t, updated.Disabled)
			require.Equal(t, master.EventGroup, updated.EventGroup)
			if tc.name != "reset" {
				require.Equal(t, master.Recurrence, updated.Recurrence)
			}
		})
	}
}

func TestPersistenceValidationAndGetEvent(t *testing.T) {
	s := &CalendarService{db: &editStore{}}
	got, err := s.GetEvent(t.Context(), "missing")
	require.NoError(t, err)
	require.Nil(t, got)
	master := recurrenceMaster()
	master.Tz = "Invalid/Zone"
	s.db = &editStore{event: &master}
	_, err = s.GetEvent(t.Context(), master.ID)
	require.Error(t, err)
	master.Tz = "Custom/Office"
	master.Recurrence.Timezones = []string{"BEGIN:VTIMEZONE\r\nTZID:Custom/Office\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0530\r\nTZOFFSETTO:+0530\r\nEND:STANDARD\r\nEND:VTIMEZONE"}
	got, err = s.GetEvent(t.Context(), master.ID)
	require.NoError(t, err)
	require.Equal(t, "14:30", got.DateFrom.Format("15:04"))
	require.Equal(t, "Custom/Office", got.DateFrom.Location().String())
	for _, edit := range []func(*models.Event){
		func(e *models.Event) { e.Recurrence.Start.Value = "bad" },
		func(e *models.Event) { e.Recurrence.Overrides[0].Cancelled = false },
		func(e *models.Event) { e.Recurrence.Overrides = nil; e.DateFrom.Time = e.DateFrom.Add(time.Minute) },
		func(e *models.Event) { e.RRule = "FREQ=DAILY;INTERVAL=0" },
	} {
		event := recurrenceMaster()
		edit(&event)
		s, db := exportService(t)
		require.NotPanics(t, func() { require.ErrorIs(t, s.AddEvents(t.Context(), []models.Event{event}), port.ErrInvalidEvent) })
		require.Zero(t, db.addCalls)
	}
	master = recurrenceMaster()
	s.db = &editStore{event: &master, err: port.ErrConflict}
	updated := master
	require.ErrorIs(t, s.UpdateEvent(t.Context(), master.ID, &updated), port.ErrConflict)
}

func TestICSSelectsEffectiveRecurrenceAndRetainsMaster(t *testing.T) {
	master := recurrenceMaster()
	master.Disabled = false
	master.RRule = "FREQ=DAILY;COUNT=1"
	moved := master
	moved.Recurrence = nil
	moved.RRule = ""
	moved.DateFrom.Time = moved.DateFrom.AddDate(1, 0, 0)
	moved.DateTo.Time = moved.DateTo.AddDate(1, 0, 0)
	master.Recurrence.Overrides = []models.OccurrenceOverride{{RecurrenceID: *master.Recurrence.Start, Event: &moved}}
	s, db := exportService(t, master)
	got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2027"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, master, got[0])
	got, err = s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
	require.NoError(t, err)
	require.Empty(t, got)
	db.events[0].Recurrence.Overrides[0].Cancelled = true
	db.events[0].Recurrence.Overrides[0].Event = nil
	got, err = s.GetEventsICS(t.Context(), exportQuery(t, "year=2026,2027"))
	require.NoError(t, err)
	require.Empty(t, got)
	db.events[0].Recurrence.Start.Value = "bad"
	_, err = s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
	require.Error(t, err)
}

func TestICSSelectsHighFrequencySeriesWithoutMaterializing(t *testing.T) {
	for _, rule := range []string{"FREQ=MINUTELY", "FREQ=SECONDLY"} {
		for _, metadata := range []bool{false, true} {
			master := recurrenceMaster()
			master.Disabled = false
			master.RRule = rule
			master.Recurrence.Overrides = nil
			if !metadata {
				master.Recurrence = nil
			}
			s, _ := exportService(t, master)
			got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
			require.NoError(t, err)
			require.Equal(t, []models.Event{master}, got)
		}
	}
}

func TestPersistenceRejectsDurationProjectionMismatch(t *testing.T) {
	for _, detached := range []bool{false, true} {
		master := recurrenceMaster()
		master.Recurrence.Overrides = nil
		target := &master
		if detached {
			override := master
			override.RRule = ""
			r := *master.Recurrence
			override.Recurrence = &r
			master.Recurrence.Overrides = []models.OccurrenceOverride{{RecurrenceID: *r.Start, Event: &override}}
			target = &override
		}
		target.Recurrence.End = nil
		target.Recurrence.Duration = "PT2H"
		s, db := exportService(t)
		require.ErrorIs(t, s.AddEvents(t.Context(), []models.Event{master}), port.ErrInvalidEvent)
		require.Zero(t, db.addCalls)
		current := recurrenceMaster()
		current.Recurrence.Overrides = nil
		store := &editStore{event: &current}
		s.db = store
		require.ErrorIs(t, s.UpdateEvent(t.Context(), master.ID, &master), port.ErrInvalidEvent)
		require.Zero(t, store.writes)
		target.DateTo.Time = target.DateFrom.Add(2 * time.Hour)
		require.NoError(t, validatePersistedEvent(master))
	}
}

func TestICSMaterializesEnhancedSets(t *testing.T) {
	for _, rule := range []string{"FUNC:GoodFriday", "RRULE:FREQ=DAILY;COUNT=3 RRULE:FREQ=DAILY;INTERVAL=2;COUNT=3"} {
		master := recurrenceMaster()
		master.Disabled = false
		master.RRule = rule
		master.Recurrence = &models.Recurrence{}
		if strings.HasPrefix(rule, "RRULE:") {
			master.Recurrence.ExDates = []models.CalendarDate{{Value: "20260102T090000Z"}}
			moved := master
			moved.RRule, moved.Recurrence = "", nil
			moved.DateFrom.Time = master.DateFrom.AddDate(0, 0, 4)
			moved.DateTo.Time = moved.DateFrom.Add(time.Hour)
			master.Recurrence.Overrides = []models.OccurrenceOverride{{RecurrenceID: models.CalendarDate{Value: "20260103T090000Z"}, Event: &moved}}
		}
		s, db := exportService(t, master)
		got, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2026,2026"))
		require.NoError(t, err)
		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		want, err := ical.Occurrences(t.Context(), master, from, from.AddDate(1, 0, 0))
		require.NoError(t, err)
		require.Len(t, got, len(want))
		require.NotEmpty(t, got)
		ids := map[string]bool{}
		for i, event := range got {
			require.Empty(t, event.RRule)
			require.Nil(t, event.Recurrence)
			require.Nil(t, event.RecurrenceID)
			require.False(t, event.IsOverride)
			require.False(t, ids[event.ID], "distinct original keys need distinct IDs even when moved to the same instant")
			ids[event.ID] = true
			require.True(t, event.DateFrom.Equal(want[i].DateFrom.Time))
			require.True(t, event.DateTo.Equal(want[i].DateTo.Time))
		}
		data, err := ical.GenerateICS(got, "")
		require.NoError(t, err)
		require.NotContains(t, data, "RRULE:")
		require.NotContains(t, data, "FUNC:")
		require.NotContains(t, data, "RECURRENCE-ID:")
		parsed, err := ical.ParseICS(strings.NewReader(data), time.UTC)
		require.NoError(t, err)
		require.Len(t, parsed, len(got))
		again, err := s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
		require.NoError(t, err)
		require.Equal(t, got, again)
		if len(master.Recurrence.Overrides) > 0 {
			moved := db.events[0].Recurrence.Overrides[0].Event
			moved.DateFrom.Time = moved.DateFrom.Add(time.Hour)
			moved.DateTo.Time = moved.DateTo.Add(time.Hour)
			again, err = s.GetEventsICS(t.Context(), exportQuery(t, "year=2026"))
			require.NoError(t, err)
			for _, event := range again {
				require.True(t, ids[event.ID], "moving an override must not change its export ID")
			}
		}
	}
}
