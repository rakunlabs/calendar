package ical

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

func TestRecurrenceSetDetachedRoundTrip(t *testing.T) {
	data := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:series\r\nRECURRENCE-ID:20260102T090000Z\r\nSTATUS:CANCELLED\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:series\r\nRECURRENCE-ID:20260201T090000Z\r\nDTSTART:20260104T120000Z\r\nDURATION:PT2H\r\nSUMMARY:Moved\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:series\r\nDTSTART:20260101T090000Z\r\nDURATION:PT1H\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEXDATE:20260103T090000Z\r\nRDATE;VALUE=PERIOD:20260105T090000Z/PT3H\r\nRDATE:20260101T090000Z\r\nBEGIN:VALARM\r\nDTSTART:invalid\r\nSUMMARY:Wrong\r\nDURATION:invalid\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR"
	events, err := ParseICS(strings.NewReader(data), nil)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Len(t, events[0].Recurrence.Overrides, 2)
	require.Empty(t, events[0].Name)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 6))
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "20260101T090000Z", got[0].RecurrenceID.Value)
	require.Equal(t, "20260201T090000Z", got[1].RecurrenceID.Value)
	require.True(t, got[1].IsOverride)
	require.Equal(t, "series", got[1].ID)
	require.Equal(t, 3*time.Hour, got[2].DateTo.Sub(got[2].DateFrom.Time))
	exported, err := GenerateICS(events, "")
	require.NoError(t, err)
	require.Equal(t, 3, strings.Count(exported, "UID:series"))
	back, err := ParseICS(strings.NewReader(exported), nil)
	require.NoError(t, err)
	require.Equal(t, events, back)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Occurrences(ctx, events[0], from, from.AddDate(0, 0, 6))
	require.ErrorIs(t, err, context.Canceled)
}

func TestRecurrenceNominalDuration(t *testing.T) {
	for _, duration := range []string{"P1D", "PT24H"} {
		data := "BEGIN:VEVENT\nUID:dst\nDTSTART;TZID=America/New_York:20260307T120000\nDURATION:" + duration + "\nRRULE:FREQ=DAILY;COUNT=2\nEND:VEVENT"
		events, err := ParseICS(strings.NewReader(data), nil)
		require.NoError(t, err)
		from := events[0].DateFrom.Time
		got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 3))
		require.NoError(t, err)
		require.Len(t, got, 2)
		if duration == "P1D" {
			require.Equal(t, 23*time.Hour, got[0].DateTo.Sub(got[0].DateFrom.Time))
			require.Equal(t, 12, got[0].DateTo.Hour())
		} else {
			require.Equal(t, 24*time.Hour, got[0].DateTo.Sub(got[0].DateFrom.Time))
		}
	}
}

func TestCalendarDateLeapAndFloating(t *testing.T) {
	d := models.CalendarDate{Value: "20161231T235960Z"}
	got, err := ResolveCalendarDate(d, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 59, got.Second())
	require.Equal(t, "20161231T235960Z", d.Value)
	data := "BEGIN:VEVENT\nUID:leap\nDTSTART:20161231T235960Z\nDURATION:PT1H\nEND:VEVENT"
	events, err := ParseICS(strings.NewReader(data), nil)
	require.NoError(t, err)
	out, err := GenerateICS(events, "")
	require.NoError(t, err)
	require.Contains(t, out, "DTSTART:20161231T235960Z")
	loc, err := time.LoadLocation("Europe/Amsterdam")
	require.NoError(t, err)
	events, err = ParseICS(strings.NewReader("BEGIN:VEVENT\nDTSTART:20260101T090000\nDURATION:PT1H\nEND:VEVENT"), loc)
	require.NoError(t, err)
	out, err = GenerateICS(events, "")
	require.NoError(t, err)
	require.Contains(t, out, "DTSTART:20260101T090000\r\n")
}

func TestRecurrenceMalformed(t *testing.T) {
	for _, property := range []string{"RECURRENCE-ID;RANGE=THISANDFUTURE:20260101T090000Z", "RDATE;VALUE=PERIOD:20260101T090000Z", "RDATE;VALUE=PERIOD:20260101T090000Z/20260101T080000Z", "EXDATE;VALUE=DATE:20260101", "EXDATE:invalid", "DTEND:20260101T100000Z", "DURATION:PT2H"} {
		_, err := ParseICS(strings.NewReader("BEGIN:VEVENT\nDTSTART:20260101T090000Z\nDURATION:PT1H\n"+property+"\nEND:VEVENT"), nil)
		require.Error(t, err, property)
	}
	for _, data := range []string{"BEGIN:VEVENT\nDTSTART:20260101T090000Z", "BEGIN:VEVENT\nEND:VALARM", "BEGIN:VEVENT\ninvalid\nEND:VEVENT"} {
		_, err := ParseICS(strings.NewReader(data), nil)
		require.Error(t, err)
	}
}

func TestRecurrenceSetUnionAndPeriodPrecedence(t *testing.T) {
	data := "BEGIN:VEVENT\nUID:union\nDTSTART:20260101T090000Z\nDURATION:PT5H\nRRULE:FREQ=WEEKLY;BYDAY=FR;COUNT=1\nRDATE:20260103T090000Z,20260103T090000Z\nRDATE;VALUE=PERIOD:20260103T090000Z/20260103T100000Z\nEND:VEVENT"
	events, err := ParseICS(strings.NewReader(data), nil)
	require.NoError(t, err)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 4))
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, 1, got[0].DateFrom.Day())
	require.Equal(t, 2, got[1].DateFrom.Day())
	require.Equal(t, time.Hour, got[2].DateTo.Sub(got[2].DateFrom.Time))
	got, err = Occurrences(t.Context(), events[0], from.AddDate(0, 0, 2).Add(11*time.Hour), from.AddDate(0, 0, 3))
	require.NoError(t, err)
	require.Empty(t, got)
	has, err := HasOccurrence(t.Context(), events[0], from.AddDate(0, 0, 2).Add(11*time.Hour), from.AddDate(0, 0, 3))
	require.NoError(t, err)
	require.False(t, has)
}

func TestRecurrenceSubDailySet(t *testing.T) {
	for _, freq := range []string{"HOURLY", "MINUTELY", "SECONDLY"} {
		data := "BEGIN:VEVENT\nDTSTART:20260101T090000Z\nDURATION:PT1S\nRRULE:FREQ=" + freq + ";COUNT=3\nEND:VEVENT"
		events, err := ParseICS(strings.NewReader(data), nil)
		require.NoError(t, err)
		got, err := Occurrences(t.Context(), events[0], events[0].DateFrom.Time, events[0].DateFrom.AddDate(0, 0, 1))
		require.NoError(t, err)
		require.Len(t, got, 3)
	}
}

func TestValidateEventRecurrenceMetadata(t *testing.T) {
	start := models.CalendarDate{Value: "20260101T090000Z"}
	e := models.Event{Recurrence: &models.Recurrence{Start: &start, Duration: "PT1H"}}
	require.NoError(t, ValidateEvent(e))
	e.Recurrence.Overrides = []models.OccurrenceOverride{{RecurrenceID: start, Cancelled: true}}
	require.NoError(t, ValidateEvent(e))
	e.Recurrence.Overrides = append(e.Recurrence.Overrides, e.Recurrence.Overrides[0])
	require.ErrorContains(t, ValidateEvent(e), "duplicate RECURRENCE-ID")
	e.Recurrence.Overrides = nil
	e.Recurrence.ExDates = make([]models.CalendarDate, 100001)
	require.ErrorContains(t, ValidateEvent(e), "work limit")
	require.ErrorContains(t, ValidateEvent(models.Event{Recurrence: &models.Recurrence{Duration: "PT1H"}}), "DTSTART")
}

func TestRecurrenceSubDailyDistantWindow(t *testing.T) {
	events, err := ParseICS(strings.NewReader("BEGIN:VEVENT\nDTSTART:20000101T000000Z\nDURATION:PT2S\nRRULE:FREQ=SECONDLY\nEND:VEVENT"), nil)
	require.NoError(t, err)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := Occurrences(t.Context(), events[0], from, from.Add(2*time.Second))
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, from.Add(-time.Second), got[0].DateFrom.Time)
}

func TestRecurrenceBerlinFold(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	want := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	for _, tzid := range []string{"", "Europe/Berlin"} {
		got, err := ResolveCalendarDate(models.CalendarDate{Value: "20261025T023000", TZID: tzid}, loc, nil)
		require.NoError(t, err)
		require.True(t, want.Equal(got), "got %s", got)
	}
	for _, tc := range []struct {
		name, properties, detached string
		count                      int
		moved                      bool
	}{
		{name: "rule", count: 1},
		{name: "EXDATE", properties: "EXDATE;TZID=Europe/Berlin:20261025T023000\n"},
		{name: "cancel", detached: "BEGIN:VEVENT\nUID:fold\nRECURRENCE-ID;TZID=Europe/Berlin:20261025T023000\nSTATUS:CANCELLED\nEND:VEVENT\n"},
		{name: "override", count: 1, moved: true, detached: "BEGIN:VEVENT\nUID:fold\nRECURRENCE-ID;TZID=Europe/Berlin:20261025T023000\nDTSTART;TZID=Europe/Berlin:20261025T043000\nDURATION:PT15M\nEND:VEVENT\n"},
		{name: "RDATE dedup", count: 1, properties: "RDATE;TZID=Europe/Berlin:20261025T023000\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := "BEGIN:VEVENT\nUID:fold\nDTSTART;TZID=Europe/Berlin:20261024T023000\nDURATION:PT15M\nRRULE:FREQ=DAILY;COUNT=2\n" + tc.properties + "END:VEVENT\n" + tc.detached
			events, err := ParseICS(strings.NewReader(data), nil)
			require.NoError(t, err)
			from := want.Add(-30 * time.Minute)
			to := from.Add(24 * time.Hour)
			got, err := Occurrences(t.Context(), events[0], from, to)
			require.NoError(t, err)
			require.Len(t, got, tc.count)
			has, err := HasOccurrence(t.Context(), events[0], from, to)
			require.NoError(t, err)
			require.Equal(t, tc.count > 0, has)
			if tc.count > 0 {
				expected := want
				if tc.moved {
					expected = want.Add(3 * time.Hour)
				}
				require.True(t, expected.Equal(got[0].DateFrom.Time))
				require.Equal(t, tc.moved, got[0].IsOverride)
			}
		})
	}
	// A standalone RDATE and explicit DTSTART must resolve to the same first fold.
	for _, dates := range []string{"DTSTART;TZID=Europe/Berlin:20261025T023000\n", "DTSTART;TZID=Europe/Berlin:20261024T023000\nRDATE;TZID=Europe/Berlin:20261025T023000\n"} {
		events, err := ParseICS(strings.NewReader("BEGIN:VEVENT\n"+dates+"DURATION:PT15M\nEND:VEVENT"), nil)
		require.NoError(t, err)
		got, err := Occurrences(t.Context(), events[0], want, want.Add(time.Hour))
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.True(t, want.Equal(got[0].DateFrom.Time))
	}
}

func TestValidateEventUnusedTimezones(t *testing.T) {
	for _, tzid := range []string{"", "UTC"} {
		start := models.CalendarDate{Value: "20260101T090000Z"}
		event := models.Event{Tz: tzid, Recurrence: &models.Recurrence{Start: &start, Duration: "PT1H", Timezones: []string{"BEGIN:VTIMEZONE\nTZID:Unused\nEND:VTIMEZONE"}}}
		require.Error(t, ValidateEvent(event))
		_, err := HasOccurrence(t.Context(), event, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
		require.Error(t, err)
	}
	// Definitions inherited by detached events are merged without repetition.
	raw := "BEGIN:VTIMEZONE\nTZID:Unused\nBEGIN:STANDARD\nDTSTART:19700101T000000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE"
	start := models.CalendarDate{Value: "20260101T090000Z"}
	event := models.Event{Recurrence: &models.Recurrence{Start: &start, Duration: "PT1H", Timezones: []string{raw, raw}}}
	require.Equal(t, []string{raw}, eventTimezoneDefinitions(event, []string{raw}))
	require.NoError(t, ValidateEvent(event))
	other := strings.ReplaceAll(raw, "+0100", "+0200")
	event.Recurrence.Timezones = append(event.Recurrence.Timezones, other)
	require.ErrorContains(t, ValidateEvent(event), "conflicting VTIMEZONE")
}

func TestEventTimesEffectiveProjection(t *testing.T) {
	start := models.CalendarDate{Value: "20260328T120000", TZID: "Europe/Berlin"}
	event := models.Event{DateFrom: types.Time{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, DateTo: types.Time{Time: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)}, Recurrence: &models.Recurrence{Start: &start, Duration: "P1D"}}
	before := event
	a, b, err := EventTimes(event)
	require.NoError(t, err)
	require.Equal(t, "2026-03-28T12:00:00+01:00", a.Format(time.RFC3339))
	require.Equal(t, "2026-03-29T12:00:00+02:00", b.Format(time.RFC3339))
	require.Equal(t, 23*time.Hour, b.Sub(a))
	require.Equal(t, before, event)
	event.Recurrence.Duration = "PT24H"
	_, b, err = EventTimes(event)
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, b.Sub(a))
	end := models.CalendarDate{Value: "20260329T140000", TZID: "Europe/Berlin"}
	event.Recurrence.End = &end
	_, _, err = EventTimes(event)
	require.ErrorContains(t, err, "mutually exclusive")
	event.Recurrence.Duration = ""
	_, b, err = EventTimes(event)
	require.NoError(t, err)
	require.Equal(t, 14, b.Hour())
}

func TestHasOccurrenceShortCircuitsExpansion(t *testing.T) {
	// DTSTART and the next minute are excluded, forcing the iterator callback
	// to stop on a later survivor rather than expanding an entire minutely year.
	data := "BEGIN:VEVENT\nDTSTART:20260101T000000Z\nDURATION:PT1S\nRRULE:FREQ=MINUTELY\nEXDATE:20260101T000000Z,20260101T000100Z\nEND:VEVENT"
	events, err := ParseICS(strings.NewReader(data), nil)
	require.NoError(t, err)
	from := events[0].DateFrom.Time
	to := from.AddDate(1, 0, 0)
	has, err := HasOccurrence(t.Context(), events[0], from, to)
	require.NoError(t, err)
	require.True(t, has)
	got, err := Occurrences(t.Context(), events[0], from, to)
	require.ErrorContains(t, err, "result limit (20000)")
	require.Nil(t, got)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	has, err = HasOccurrence(ctx, events[0], from, to)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, has)
	_, err = HasOccurrence(t.Context(), events[0], to, from)
	require.Error(t, err)
}

func TestHasOccurrenceEffectiveSet(t *testing.T) {
	for _, tc := range []struct {
		name, properties, detached string
		want                       bool
	}{
		{name: "all excluded", properties: "RRULE:FREQ=DAILY;COUNT=2\nEXDATE:20260101T090000Z,20260102T090000Z\n"},
		{name: "RDATE survives", properties: "EXDATE:20260101T090000Z\nRDATE:20260102T090000Z\n", want: true},
		{name: "cancelled", detached: "BEGIN:VEVENT\nUID:exists\nRECURRENCE-ID:20260101T090000Z\nSTATUS:CANCELLED\nEND:VEVENT\n"},
		{name: "moved out", detached: "BEGIN:VEVENT\nUID:exists\nRECURRENCE-ID:20260101T090000Z\nDTSTART:20260201T090000Z\nDURATION:PT1H\nEND:VEVENT\n"},
		{name: "moved in", properties: "EXDATE:20260101T090000Z\n", detached: "BEGIN:VEVENT\nUID:exists\nRECURRENCE-ID:20260201T090000Z\nDTSTART:20260102T090000Z\nDURATION:PT1H\nEND:VEVENT\n", want: true},
		{name: "FUNC anchor is not an instance", properties: "FUNC:GoodFriday\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			properties := tc.properties
			data := "BEGIN:VEVENT\nUID:exists\nDTSTART:20260101T090000Z\nDURATION:PT1H\n" + properties + "END:VEVENT\n" + tc.detached
			events, err := ParseICS(strings.NewReader(data), nil)
			require.NoError(t, err)
			if strings.HasPrefix(properties, "FUNC:") {
				events[0].RRule = strings.TrimSpace(properties)
			}
			from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			to := from.AddDate(0, 0, 3)
			has, err := HasOccurrence(t.Context(), events[0], from, to)
			require.NoError(t, err)
			require.Equal(t, tc.want, has)
			got, err := Occurrences(t.Context(), events[0], from, to)
			require.NoError(t, err)
			require.Equal(t, len(got) > 0, has)
		})
	}
}

func TestOccurrencesResultLimitBoundary(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Second)}, RRule: "FREQ=MINUTELY;COUNT=20000"}
	got, err := Occurrences(t.Context(), event, start, start.AddDate(1, 0, 0))
	require.NoError(t, err)
	require.Len(t, got, 20000)
	event.RRule = "FREQ=MINUTELY;COUNT=20001"
	got, err = Occurrences(t.Context(), event, start, start.AddDate(1, 0, 0))
	require.ErrorContains(t, err, "result limit (20000)")
	require.Nil(t, got)
}
