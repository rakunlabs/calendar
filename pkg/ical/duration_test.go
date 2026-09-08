package ical

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCalendarDuration(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, day := range []time.Time{time.Date(2026, 3, 7, 12, 0, 0, 0, loc), time.Date(2026, 10, 31, 12, 0, 0, 0, loc)} {
		nominal, err := parseDuration("P1D")
		require.NoError(t, err)
		exact, err := parseDuration("PT24H")
		require.NoError(t, err)
		require.Equal(t, 12, nominal.end(day).Hour())
		require.Equal(t, 24*time.Hour, exact.end(day).Sub(day))
		require.NotEqual(t, nominal.end(day), exact.end(day))
	}
	for _, value := range []string{"P", "PT", "P1DT", "P1M", "P1Y", "P1W1D", "-P1D", "P0D", "PT0S", "PT1.5H", "PT999999999999999999999999H", "P999999999D"} {
		_, err := parseDuration(value)
		require.Error(t, err, value)
	}
	d, err := parseDuration("+P2DT3H4M5S")
	require.NoError(t, err)
	require.Equal(t, 2, d.days)
	require.Equal(t, 3*time.Hour+4*time.Minute+5*time.Second, d.exact)
	d, err = parseDuration("P2W")
	require.NoError(t, err)
	require.Equal(t, 14, d.days)
}
