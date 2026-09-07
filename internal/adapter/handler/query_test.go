package handler

import (
	"testing"

	"github.com/rakunlabs/query"
	"github.com/stretchr/testify/require"
)

func TestParseQueryControls(t *testing.T) {
	h, err := NewHTTP(nil)
	require.NoError(t, err)

	for name, validator := range map[string]*query.Validator{
		"events":    h.Validator.GetEvents,
		"relations": h.Validator.GetRelations,
	} {
		t.Run(name, func(t *testing.T) {
			for _, raw := range []string{
				"limit=10&offset=5&sort=-entity",
				"_limit=10&_offset=5&_sort=-entity",
				"limit=10&_offset=5&sort=-entity",
				"limit=1&offset=2&sort=entity&_limit=10&_offset=5&_sort=-entity",
				"_limit=10&_offset=5&_sort=-entity&limit=1&offset=2&sort=entity",
			} {
				q, err := parseQuery(raw, validator, query.WithDefaultLimit(DefaultLimit))
				require.NoError(t, err, raw)
				require.Equal(t, uint64(10), q.GetLimit(), raw)
				require.Equal(t, uint64(5), q.GetOffset(), raw)
				require.Equal(t, []query.ExpressionSort{{Field: "entity", Desc: true}}, q.Sort, raw)
				require.Empty(t, q.Where, raw)
			}

			q, err := parseQuery("", validator, query.WithDefaultLimit(DefaultLimit))
			require.NoError(t, err)
			require.Equal(t, DefaultLimit, q.GetLimit())
		})
	}

	for name, tc := range map[string]struct {
		validator *query.Validator
		base      string
	}{
		"events":           {h.Validator.GetEvents, ""},
		"delete events":    {h.Validator.DeleteEvents, "id=a&"},
		"relations":        {h.Validator.GetRelations, ""},
		"delete relations": {h.Validator.DeleteRelations, "entity=x&"},
		"holidays":         {h.Validator.GetEventsDate, "date=2026-01-01&"},
		"ics":              {h.Validator.GetICS, ""},
	} {
		t.Run(name+" rejects fields", func(t *testing.T) {
			for _, raw := range []string{"fields=id", "_fields=id"} {
				_, err := parseQuery(tc.base+raw, tc.validator)
				require.ErrorContains(t, err, "fields is not allowed")
			}
		})
	}

	for _, raw := range []string{"limit=1", "offset=1", "sort=id", "_limit=1", "_offset=1", "_sort=id"} {
		_, err := parseQuery("id=a&"+raw, h.Validator.DeleteEvents)
		require.Error(t, err)
		_, err = parseQuery("entity=x&"+raw, h.Validator.DeleteRelations)
		require.Error(t, err)
	}
}

func TestParseQueryListsAndSkippedValues(t *testing.T) {
	h, err := NewHTTP(nil)
	require.NoError(t, err)

	for _, raw := range []string{"id=a,b", "id=a%2Cb", "id[in]=a,b"} {
		q, err := parseQuery(raw, h.Validator.DeleteEvents)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, q.GetValues("id"))
		require.Equal(t, query.OperatorIn, q.Values["id"][0].Operator)
	}

	q, err := parseQuery("id[eq]=a,b", h.Validator.DeleteEvents)
	require.NoError(t, err)
	require.Equal(t, []string{"a,b"}, q.GetValues("id"))
	require.Equal(t, query.OperatorEq, q.Values["id"][0].Operator)

	q, err = parseQuery("entity=a,b&event_id=c,d", h.Validator.GetRelations)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, q.GetValues("entity"))
	require.Equal(t, []string{"c", "d"}, q.GetValues("event_id"))

	q, err = parseQuery("date=2026-01-01&entity=a,b", h.Validator.GetEventsDate, query.WithSkipExpressionCmp("date"))
	require.NoError(t, err)
	require.Equal(t, []string{"2026-01-01"}, q.GetValues("date"))
	require.Len(t, q.Where, 1)
	require.Equal(t, "entity", q.Where[0].(*query.ExpressionCmp).Field)

	_, err = parseQuery("date=2026-01-01,2026-01-02", h.Validator.GetEventsDate, query.WithSkipExpressionCmp("date"))
	require.Error(t, err) // Dates only allow equality, not an implicit IN list.

	q, err = parseQuery("year=2025,2026", h.Validator.GetICS, query.WithSkipExpressionCmp("year"))
	require.NoError(t, err)
	require.Equal(t, []string{"2025", "2026"}, q.GetValues("year"))
	require.Equal(t, query.OperatorIn, q.Values["year"][0].Operator)
	require.Empty(t, q.Where)
}

func TestParseQueryEncodedAmpersand(t *testing.T) {
	h, err := NewHTTP(nil)
	require.NoError(t, err)
	q, err := parseQuery("name=R%26D&limit=10", h.Validator.GetEvents)
	require.NoError(t, err)
	require.Equal(t, []string{"R&D"}, q.GetValues("name"))
	require.Len(t, q.Where, 1)
	require.Equal(t, uint64(10), q.GetLimit())
}
