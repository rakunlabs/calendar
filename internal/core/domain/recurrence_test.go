package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecurrenceDatabaseJSON(t *testing.T) {
	r := Recurrence{Start: &CalendarDate{Value: "20260101T090000Z"}, Overrides: []OccurrenceOverride{
		{RecurrenceID: CalendarDate{Value: "20260102T090000Z"}, Cancelled: true},
	}}
	value, err := r.Value()
	require.NoError(t, err)
	data, ok := value.([]byte)
	require.True(t, ok, "types.JSON returns JSON bytes")
	for _, input := range []any{data, string(data)} {
		var got Recurrence
		require.NoError(t, got.Scan(input))
		require.Equal(t, r, got)
		require.NoError(t, got.Scan(nil))
		require.Equal(t, Recurrence{}, got)
	}
	for _, input := range []any{[]byte(`{"start":`), "invalid", 42} {
		got := r
		require.Error(t, got.Scan(input))
		require.Equal(t, r, got, "failed scans must not modify the receiver")
	}
}

func TestEventRecurrenceWireShape(t *testing.T) {
	for _, input := range []string{`{}`, `{"recurrence":null}`} {
		var event Event
		require.NoError(t, json.Unmarshal([]byte(input), &event))
		require.Nil(t, event.Recurrence)
		data, err := json.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(data), `"recurrence"`)
	}
	for _, input := range []string{`{}`, `{"start":{"value":"20260101T090000Z"}}`} {
		var event Event
		require.NoError(t, json.Unmarshal([]byte(`{"recurrence":`+input+`}`), &event))
		require.NotNil(t, event.Recurrence)
		data, err := json.Marshal(event)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &fields))
		require.JSONEq(t, input, string(fields["recurrence"]))
	}
}
