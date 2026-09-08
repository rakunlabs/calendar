package ical

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/pkg/ical/special"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

func TestOccurrences(t *testing.T) {
	for _, tc := range []struct {
		name, start, end, rule, from, to string
		want                             []string
	}{
		{"overlap", "2026-01-01T00:00:00Z", "2026-01-04T00:00:00Z", "", "2026-01-03T00:00:00Z", "2026-01-05T00:00:00Z", []string{"2026-01-01"}},
		{"exclusive end", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "", "2026-01-02T00:00:00Z", "2026-01-03T00:00:00Z", nil},
		{"count before range", "2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z", "RRULE:FREQ=DAILY;COUNT=2", "2026-01-03T00:00:00Z", "2026-02-01T00:00:00Z", nil},
		{"weekly default weekday", "2026-01-05T10:00:00Z", "2026-01-05T11:00:00Z", "RRULE:FREQ=WEEKLY;COUNT=3", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", []string{"2026-01-05", "2026-01-12", "2026-01-19"}},
		{"monthly 31 skips short months", "2026-01-31T10:00:00Z", "2026-01-31T11:00:00Z", "RRULE:FREQ=MONTHLY;COUNT=3", "2026-01-01T00:00:00Z", "2026-06-01T00:00:00Z", []string{"2026-01-31", "2026-03-31", "2026-05-31"}},
		{"special", "2020-01-01T00:00:00Z", "2020-01-02T00:00:00Z", "FUNC:GoodFriday", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z", []string{"2026-04-03"}},
		{"special exclusive query end", "2020-01-01T09:00:00Z", "2020-01-01T11:00:00Z", "FUNC:GoodFriday", "2026-04-01T00:00:00Z", "2026-04-03T09:00:00Z", nil},
		{"special exclusive occurrence end", "2020-01-01T09:00:00Z", "2020-01-01T11:00:00Z", "FUNC:GoodFriday", "2026-04-03T11:00:00Z", "2026-04-04T00:00:00Z", nil},
		{"special timed overlap", "2020-01-01T09:00:00Z", "2020-01-01T11:00:00Z", "FUNC:GoodFriday", "2026-04-03T10:00:00Z", "2026-04-04T00:00:00Z", []string{"2026-04-03"}},
		{"recurring overlap", "2026-01-01T00:00:00Z", "2026-01-04T00:00:00Z", "RRULE:FREQ=WEEKLY", "2026-01-03T00:00:00Z", "2026-01-05T00:00:00Z", []string{"2026-01-01"}},
		{"until inclusive", "2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z", "RRULE:FREQ=DAILY;UNTIL=20260102T100000Z", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", []string{"2026-01-01", "2026-01-02"}},
		{"recurring exclusive upper bound", "2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z", "RRULE:FREQ=DAILY", "2026-01-01T11:00:00Z", "2026-01-02T10:00:00Z", nil},
		{"mixed rules sorted and deduplicated", "2020-01-01T00:00:00Z", "2020-01-02T00:00:00Z", "FUNC:EasterSunday FUNC:GoodFriday RRULE:FREQ=YEARLY;BYMONTH=4;BYMONTHDAY=3", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z", []string{"2026-04-03", "2026-04-05"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parse := func(s string) time.Time { v, err := time.Parse(time.RFC3339, s); require.NoError(t, err); return v }
			event := models.Event{ID: "original", DateFrom: types.Time{Time: parse(tc.start)}, DateTo: types.Time{Time: parse(tc.end)}, RRule: tc.rule}
			got, err := Occurrences(t.Context(), event, parse(tc.from), parse(tc.to))
			require.NoError(t, err)
			var days []string
			for _, occurrence := range got {
				days = append(days, occurrence.DateFrom.Format("2006-01-02"))
				require.Equal(t, "original", occurrence.ID)
			}
			require.Equal(t, tc.want, days)
		})
	}
}

func TestOccurrencesInvalidRulesAndCancellation(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}}
	for _, rule := range []string{"RRULE:FREQ=DAILY;BYHOUR=no", "RRULE:FREQ=DAILY;BYMONTH=13", "RRULE:FREQ=INVALID", "RRULE:FREQ=DAILY;INTERVAL=0"} {
		event.RRule = rule
		_, err := Occurrences(t.Context(), event, start, start.AddDate(1, 0, 0))
		require.Error(t, err)
	}
	event.RRule = "RRULE:FREQ=DAILY"
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Occurrences(ctx, event, start, start.AddDate(1, 0, 0))
	require.ErrorIs(t, err, context.Canceled)
}

func TestOccurrencesDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	require.NoError(t, err)
	start := time.Date(2026, 3, 28, 0, 0, 0, 0, loc)
	event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(0, 0, 1)}, AllDay: true, Tz: loc.String(), RRule: "RRULE:FREQ=DAILY;COUNT=3"}
	got, err := Occurrences(t.Context(), event, start, start.AddDate(0, 0, 4))
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, 23*time.Hour, got[1].DateTo.Sub(got[1].DateFrom.Time))
	require.Equal(t, 0, got[2].DateFrom.Hour())
}

func TestOccurrenceOverrideInheritsSeriesScope(t *testing.T) {
	start := time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)
	override := models.Event{DateFrom: types.Time{Time: start.Add(time.Hour)}, DateTo: types.Time{Time: start.Add(2 * time.Hour)}}
	master := models.Event{
		ID: "series", DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)},
		EventGroup: types.Null[string]{V: "team", Valid: true}, Disabled: true,
		Recurrence: &models.Recurrence{Overrides: []models.OccurrenceOverride{{RecurrenceID: CalendarDateFromTime(start, false), Event: &override}}},
	}
	got, err := Occurrences(t.Context(), master, start, start.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, master.EventGroup, got[0].EventGroup)
	require.True(t, got[0].Disabled)
	require.Equal(t, "series", got[0].ID)
	require.False(t, override.Disabled)
}

func TestOccurrencesRangesAndCancellation(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, rule := range []string{"", "FREQ=DAILY", "FUNC:EasterSunday", "FUNC:EasterSunday RRULE:FREQ=DAILY"} {
		t.Run(rule, func(t *testing.T) {
			event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(1, 0, 0)}, RRule: rule}
			for _, end := range []time.Time{start, start.Add(-time.Hour)} {
				got, err := Occurrences(t.Context(), event, start, end)
				require.Error(t, err)
				require.Empty(t, got)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err := Occurrences(ctx, event, start, start.AddDate(1, 0, 0))
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestOccurrencesFuncDurationAndAnchor(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	require.NoError(t, err)
	for _, allDay := range []bool{false, true} {
		start := time.Date(2023, 4, 7, 9, 30, 15, 123, loc)
		if allDay {
			start = time.Date(2023, 4, 7, 0, 0, 0, 0, loc)
		}
		event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(0, 0, 3)}, Tz: loc.String(), AllDay: allDay, RRule: "func:GoodFriday"}
		got, err := Occurrences(t.Context(), event, time.Date(2024, 1, 1, 0, 0, 0, 0, loc), time.Date(2025, 1, 1, 0, 0, 0, 0, loc))
		require.NoError(t, err)
		require.Len(t, got, 1)
		want := time.Date(2024, 3, 29, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), loc)
		require.Equal(t, want, got[0].DateFrom.Time)
		wantEnd := want.Add(72 * time.Hour)
		if allDay {
			wantEnd = want.AddDate(0, 0, 3)
		}
		require.Equal(t, wantEnd, got[0].DateTo.Time)
		got, err = Occurrences(t.Context(), event, start.AddDate(-3, 0, 0), start)
		require.NoError(t, err)
		require.Empty(t, got)
		// A DTSTART later than the holiday must exclude that year's candidate too.
		event.DateFrom.Time = start.AddDate(0, 0, 1)
		event.DateTo.Time = start.AddDate(0, 0, 4)
		got, err = Occurrences(t.Context(), event, start.AddDate(0, -1, 0), start.AddDate(0, 1, 0))
		require.NoError(t, err)
		require.Empty(t, got)
	}
}

func TestOccurrencesFuncLongOverlapAndLimit(t *testing.T) {
	start := time.Date(2020, 4, 12, 0, 0, 0, 0, time.UTC)
	for _, allDay := range []bool{false, true} {
		event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(3, 0, 0)}, RRule: "FUNC:EasterSunday", AllDay: allDay}
		from := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		got, err := Occurrences(t.Context(), event, from, from.AddDate(0, 0, 1))
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, start, got[0].DateFrom.Time)
		_, err = Occurrences(t.Context(), event, start, start.AddDate(100001, 0, 0))
		require.ErrorContains(t, err, "result limit (20000)")
	}
}

func TestOccurrencesCancellationDuringFunc(t *testing.T) {
	const name = "TESTCANCELLATION"
	t.Cleanup(func() { delete(special.Funcs, name) })
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}, RRule: "FUNC:" + name}
	for _, years := range []int{0, 10} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		special.Funcs[name] = func(year int) time.Time {
			calls++
			cancel()
			return special.EasterSunday(year)
		}
		_, err := Occurrences(ctx, event, start, start.AddDate(years, 6, 0))
		cancel()
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, calls)
	}
}

func TestOccurrencesAllDayTimeSelectorsAndDateUntil(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	for _, tc := range []struct {
		rule  string
		count int
	}{
		{"FREQ=DAILY;BYHOUR=9,17;BYMINUTE=30;BYSECOND=60;COUNT=2", 2},
		{"FREQ=DAILY;BYHOUR=9,17;BYSETPOS=1;COUNT=2", 2},
		{"FREQ=DAILY;BYHOUR=9,17;BYSETPOS=-1;COUNT=2", 2},
		{"FREQ=DAILY;BYHOUR=9,17;BYSETPOS=2;COUNT=2", 1},
		{"FREQ=MONTHLY;BYDAY=TH;BYHOUR=9,17;BYSETPOS=1;COUNT=2", 2},
		{"FREQ=DAILY;BYHOUR=9;UNTIL=20260102", 2},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			event := models.Event{DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.AddDate(0, 0, 1)}, Tz: loc.String(), AllDay: true, RRule: tc.rule}
			original := event
			got, err := Occurrences(t.Context(), event, start, start.AddDate(0, 2, 0))
			require.NoError(t, err)
			require.Len(t, got, tc.count)
			require.Equal(t, original, event)
			for _, occurrence := range got {
				require.Equal(t, 0, occurrence.DateFrom.Hour())
				require.Equal(t, 0, occurrence.DateFrom.Minute())
				require.Equal(t, 0, occurrence.DateFrom.Second())
				require.Equal(t, occurrence.DateFrom.AddDate(0, 0, 1), occurrence.DateTo.Time)
			}
		})
	}
}

func TestOccurrencesTimingOnlyPayload(t *testing.T) {
	zone := "BEGIN:VTIMEZONE\nTZID:Unused\nCOMMENT:" + strings.Repeat("x", 300*1024) + "\nBEGIN:STANDARD\nDTSTART:19700101T000000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE"
	date := func(value string) *models.CalendarDate { return &models.CalendarDate{Value: value} }
	event := models.Event{ID: "slim", RRule: "FREQ=DAILY;COUNT=3", Recurrence: &models.Recurrence{
		Start: date("20260101T090000Z"), Duration: "PT1H", Timezones: []string{zone},
		ExDates: []models.CalendarDate{*date("20260103T090000Z")},
		RDates:  []models.RecurrencePeriod{{Start: *date("20260104T090000Z")}},
		Overrides: []models.OccurrenceOverride{{RecurrenceID: *date("20260102T090000Z"), Event: &models.Event{Recurrence: &models.Recurrence{
			Start: date("20260102T120000Z"), End: date("20260102T135960Z"), Timezones: []string{zone},
		}}}},
	}}
	before, err := json.Marshal(event)
	require.NoError(t, err)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, err := Occurrences(t.Context(), event, from, from.AddDate(0, 0, 5))
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, occurrence := range got {
		r := occurrence.Recurrence
		require.NotNil(t, r)
		require.NotNil(t, r.Start)
		require.Nil(t, r.ExDates)
		require.Nil(t, r.RDates)
		require.Nil(t, r.Overrides)
		require.Nil(t, r.Timezones)
		a, b, err := EventTimes(occurrence)
		require.NoError(t, err)
		require.True(t, a.Equal(occurrence.DateFrom.Time))
		require.True(t, b.Equal(occurrence.DateTo.Time))
	}
	require.True(t, got[1].IsOverride)
	require.Equal(t, "20260102T090000Z", got[1].RecurrenceID.Value)
	require.Equal(t, "20260102T120000Z", got[1].Recurrence.Start.Value)
	require.Equal(t, "20260102T135960Z", got[1].Recurrence.End.Value)
	payload, err := json.Marshal(got)
	require.NoError(t, err)
	require.Less(t, len(payload), 10000)
	require.NotContains(t, string(payload), "VTIMEZONE")
	has, err := HasOccurrence(t.Context(), event, from, from.AddDate(0, 0, 5))
	require.NoError(t, err)
	require.True(t, has)
	got[0].Recurrence.Start.Value = "changed"
	got[1].Recurrence.End.Value = "changed"
	after, err := json.Marshal(event)
	require.NoError(t, err)
	require.Equal(t, before, after, "response timing must not alias the master graph")
}

func TestOccurrencesEffectivePeriodMetadata(t *testing.T) {
	for _, ending := range []string{"20260101T120000", "PT2H"} {
		for _, plainFirst := range []bool{false, true} {
			period := "RDATE;TZID=Europe/Berlin;VALUE=PERIOD:20260101T100000/" + ending + "\n"
			plain := "RDATE:20260101T090000Z\n"
			properties := period + plain
			if plainFirst {
				properties = plain + period
			}
			data := "BEGIN:VEVENT\nUID:period\nDTSTART:20260101T090000Z\nDURATION:PT1H\nRRULE:FREQ=DAILY;COUNT=2\n" + properties + "END:VEVENT"
			events, err := ParseICS(strings.NewReader(data), nil)
			require.NoError(t, err)
			from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 3))
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.Equal(t, "20260101T090000Z", got[0].RecurrenceID.Value)
			require.Empty(t, got[0].RecurrenceID.TZID)
			require.Equal(t, models.CalendarDate{Value: "20260101T100000", TZID: "Europe/Berlin"}, *got[0].Recurrence.Start)
			if ending == "PT2H" {
				require.Equal(t, ending, got[0].Recurrence.Duration)
				require.Nil(t, got[0].Recurrence.End)
			} else {
				require.Equal(t, models.CalendarDate{Value: ending, TZID: "Europe/Berlin"}, *got[0].Recurrence.End)
				require.Empty(t, got[0].Recurrence.Duration)
			}
			a, b, err := EventTimes(got[0])
			require.NoError(t, err)
			require.True(t, a.Equal(got[0].DateFrom.Time))
			require.True(t, b.Equal(got[0].DateTo.Time))
			require.Equal(t, 2*time.Hour, b.Sub(a))
			require.Equal(t, "20260102T090000Z", got[1].Recurrence.Start.Value)
			require.Equal(t, "PT1H", got[1].Recurrence.Duration)
		}
	}
}

func TestOccurrencesPreserveOriginalLeapEnd(t *testing.T) {
	for _, rule := range []string{"", "RRULE:FREQ=DAILY;COUNT=2\n"} {
		data := "BEGIN:VEVENT\nDTSTART:20161231T230000Z\nDTEND:20161231T235960Z\n" + rule + "END:VEVENT"
		events, err := ParseICS(strings.NewReader(data), nil)
		require.NoError(t, err)
		from := events[0].DateFrom.Time
		got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 2))
		require.NoError(t, err)
		require.Equal(t, "20161231T235960Z", got[0].Recurrence.End.Value)
		require.Equal(t, "20161231T230000Z", got[0].RecurrenceID.Value)
		if rule != "" {
			require.Len(t, got, 2)
			require.Equal(t, "20170101T230000Z", got[1].Recurrence.Start.Value)
			require.Equal(t, "20170101T235959Z", got[1].Recurrence.End.Value)
		}
	}
}

func TestOccurrencesEffectiveEndInSecondFold(t *testing.T) {
	data := "BEGIN:VEVENT\nDTSTART;TZID=Europe/Berlin:20261024T013000\nDTEND;TZID=Europe/Berlin:20261024T033000\nRRULE:FREQ=DAILY;COUNT=2\nEND:VEVENT"
	events, err := ParseICS(strings.NewReader(data), nil)
	require.NoError(t, err)
	from := events[0].DateFrom.Time
	got, err := Occurrences(t.Context(), events[0], from, from.AddDate(0, 0, 2))
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "20261025T013000Z", got[1].Recurrence.End.Value)
	_, end, err := EventTimes(got[1])
	require.NoError(t, err)
	require.True(t, end.Equal(got[1].DateTo.Time))
}
