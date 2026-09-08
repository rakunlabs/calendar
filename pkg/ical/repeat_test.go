package ical

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRepeatFormats(t *testing.T) {
	for _, input := range []string{"FREQ=DAILY;COUNT=2", "freq=daily;count=2", "RRULE:FREQ=DAILY;COUNT=2", "rRuLe:FREQ=DAILY;COUNT=2"} {
		t.Run(input, func(t *testing.T) {
			repeat, err := ParseRepeat(" \n" + input + "\t fUnC:goodfriday\n")
			require.NoError(t, err)
			require.Len(t, repeat.RRule, 1)
			require.Equal(t, "DAILY", repeat.RRule[0].Freq)
			require.Equal(t, 2, *repeat.RRule[0].Count)
			require.Len(t, repeat.Func, 1)
			require.Equal(t, "2026-04-03", repeat.Func[0](2026).Format("2006-01-02"))
		})
	}
	for _, input := range []string{"", " \n", "RRULE:", "FUNC:", "FUNC:unknown", "FREQ=NOPE", "OTHER:FREQ=DAILY"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseRepeat(input)
			require.Error(t, err)
		})
	}
}
