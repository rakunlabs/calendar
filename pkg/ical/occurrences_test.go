package ical

import (
	"testing"
	"time"

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
