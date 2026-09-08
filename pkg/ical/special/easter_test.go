package special

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWesternEasterDatesAndOffsets(t *testing.T) {
	for _, date := range []string{"1818-03-22", "1943-04-25", "1954-04-18", "1981-04-19", "2000-04-23", "2024-03-31", "2025-04-20", "2026-04-05", "2038-04-25", "2100-03-28"} {
		t.Run(date, func(t *testing.T) {
			easter, err := time.Parse("2006-01-02", date)
			require.NoError(t, err)
			require.Equal(t, easter, CalculateEasterDate(easter.Year()))
			for _, tc := range []struct {
				name   string
				fn     func(int) time.Time
				offset int
			}{
				{"GoodFriday", GoodFriday, -2},
				{"EasterSunday", EasterSunday, 0},
				{"EasterMonday", EasterMonday, 1},
				{"AscensionDay", AscensionDay, 39},
				{"WhitSunday", WhitSunday, 49},
				{"WhitMonday", WhitMonday, 50},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.Equal(t, easter.AddDate(0, 0, tc.offset), tc.fn(easter.Year()))
				})
			}
		})
	}
}
