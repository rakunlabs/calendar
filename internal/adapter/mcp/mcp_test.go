package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/query"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/adapter/mcp"
	"github.com/rakunlabs/calendar/internal/core/domain"
	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/internal/core/service"
)

// repo is an in-memory CalendarPort so the tests exercise the real service rules
// (validation, recurrence guards, optimistic concurrency) rather than a stub.
type repo struct {
	events    []domain.Event
	relations []domain.Relation
}

func (r *repo) AddEvents(_ context.Context, events []domain.Event) error {
	for i := range events {
		if events[i].ID == "" {
			events[i].ID = ulid.Make().String()
		}
		events[i].UpdatedAt = types.Time{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		r.events = append(r.events, events[i])
	}
	return nil
}

func (r *repo) GetEvent(_ context.Context, id string) (*domain.Event, error) {
	for i := range r.events {
		if r.events[i].ID == id {
			found := r.events[i]
			return &found, nil
		}
	}
	return nil, nil
}

func (r *repo) UpdateEvent(_ context.Context, id string, event *domain.Event) error {
	for i := range r.events {
		if r.events[i].ID == id {
			event.ID, event.UpdatedAt = id, types.Time{Time: r.events[i].UpdatedAt.Add(time.Second)}
			r.events[i] = *event
			return nil
		}
	}
	return fmt.Errorf("%w: event no longer exists", port.ErrConflict)
}

func (r *repo) RemoveEvent(_ context.Context, ids ...string) error {
	r.events = slices.DeleteFunc(r.events, func(e domain.Event) bool { return slices.Contains(ids, e.ID) })
	return nil
}

func (r *repo) GetEvents(_ context.Context, q *query.Query) ([]domain.Event, error) {
	found := []domain.Event{}
	for _, event := range r.events {
		if r.matches(event, q) {
			found = append(found, event)
		}
	}
	if limit := q.GetLimit(); limit > 0 {
		offset := min(q.GetOffset(), uint64(len(found)))
		found = found[offset:min(offset+limit, uint64(len(found)))]
	}
	return found, nil
}

func (r *repo) GetEventsWithFunc(ctx context.Context, q *query.Query, fn func(domain.Event) error) error {
	events, err := r.GetEvents(ctx, q)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := fn(event); err != nil {
			return err
		}
	}
	return nil
}

func (r *repo) GetEventsCount(ctx context.Context, q *query.Query) (uint64, error) {
	count := uint64(0)
	for _, event := range r.events {
		if r.matches(event, q) {
			count++
		}
	}
	return count, nil
}

func (r *repo) matches(event domain.Event, q *query.Query) bool {
	for _, expression := range q.Where {
		cmp, ok := expression.(*query.ExpressionCmp)
		if !ok {
			continue
		}
		text, _ := cmp.Value.(string)
		switch cmp.Field {
		case "event_group":
			if !event.EventGroup.Valid || event.EventGroup.V != text {
				return false
			}
		case "name":
			if !strings.Contains(strings.ToLower(event.Name), strings.ToLower(strings.Trim(text, "%"))) {
				return false
			}
		case "disabled":
			if event.Disabled != (cmp.Value == true) {
				return false
			}
		case "entity":
			assigned := false
			for _, relation := range r.relations {
				assigned = assigned || (relation.Entity == text &&
					((relation.EventID.Valid && relation.EventID.V == event.ID) ||
						(relation.EventGroup.Valid && event.EventGroup.Valid && relation.EventGroup.V == event.EventGroup.V)))
			}
			if !assigned {
				return false
			}
		}
	}
	return true
}

func (r *repo) AddRelations(_ context.Context, relations []domain.Relation) error {
	r.relations = append(r.relations, relations...)
	return nil
}
func (r *repo) RemoveRelation(context.Context, *query.Query) error { return nil }
func (r *repo) GetRelations(context.Context, *query.Query) ([]domain.Relation, error) {
	return r.relations, nil
}
func (r *repo) GetRelationsCount(context.Context, *query.Query) (uint64, error) {
	return uint64(len(r.relations)), nil
}

func connect(t *testing.T, readOnly bool, user string) (*sdk.ClientSession, *repo) {
	t.Helper()
	store := &repo{}
	svc, err := service.NewCalendarService(t.Context(), store)
	require.NoError(t, err)

	handler := mcp.New(svc, readOnly).HTTPHandler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			r.Header.Set("X-User", user)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	session, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(t.Context(),
		&sdk.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session, store
}

// call runs a tool and decodes its structured output, failing on a tool error.
func call[T any](t *testing.T, session *sdk.ClientSession, name string, args map[string]any) T {
	t.Helper()
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.False(t, result.IsError, "tool %s failed: %s", name, text(result))
	raw, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func callErr(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.True(t, result.IsError, "tool %s unexpectedly succeeded", name)
	return text(result)
}

func text(result *sdk.CallToolResult) string {
	parts := []string{}
	for _, content := range result.Content {
		if item, ok := content.(*sdk.TextContent); ok {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestToolsExposedByMode(t *testing.T) {
	names := func(readOnly bool) []string {
		session, _ := connect(t, readOnly, "")
		list, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)
		found := []string{}
		for _, tool := range list.Tools {
			found = append(found, tool.Name)
		}
		slices.Sort(found)
		return found
	}

	require.Equal(t, []string{"find_free_time", "list_events", "list_occurrences"}, names(true))
	require.Equal(t, []string{
		"cancel_occurrence", "create_event", "delete_event",
		"find_free_time", "list_events", "list_occurrences", "update_event",
	}, names(false))
}

func TestReadOnlyRejectsWrites(t *testing.T) {
	session, store := connect(t, true, "")
	_, err := session.CallTool(t.Context(), &sdk.CallToolParams{
		Name: "create_event", Arguments: map[string]any{"name": "Nope", "all_day": true, "start_date": "2026-09-15"},
	})
	require.ErrorContains(t, err, "create_event")
	require.Empty(t, store.events)
}

func TestCreateEventTimedAndAllDay(t *testing.T) {
	session, store := connect(t, false, "ai-assistant")

	timed := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Design review", "description": "Quarterly", "event_group": "Team",
		"start": "2026-09-15T09:00:00", "end": "2026-09-15T10:30:00", "time_zone": "Europe/Istanbul",
	})
	require.Equal(t, "2026-09-15T09:00:00+03:00", timed.Event.Start)
	require.Equal(t, "2026-09-15T10:30:00+03:00", timed.Event.End)
	require.False(t, timed.Event.AllDay)
	require.Equal(t, "Team", timed.Event.EventGroup)
	require.NotEmpty(t, timed.Event.ID)

	allDay := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Team trip", "all_day": true,
		"start_date": "2026-09-21", "end_date": "2026-09-23", "time_zone": "Europe/Istanbul",
	})
	// The inclusive last day is reported back, while storage keeps the exclusive end.
	require.Equal(t, "2026-09-21", allDay.Event.StartDate)
	require.Equal(t, "2026-09-23", allDay.Event.EndDate)

	stored, err := store.GetEvent(t.Context(), allDay.Event.ID)
	require.NoError(t, err)
	require.Equal(t, "2026-09-24", stored.DateTo.Format("2006-01-02"))
	require.Equal(t, "ai-assistant", stored.UpdatedBy, "the caller's audit label must be stored")
}

func TestCreateEventRejectsInvalidInput(t *testing.T) {
	session, store := connect(t, false, "")

	require.Contains(t, callErr(t, session, "create_event", map[string]any{
		"name": "", "all_day": true, "start_date": "2026-09-15",
	}), "name is required")
	require.Contains(t, callErr(t, session, "create_event", map[string]any{
		"name": "Backwards", "start": "2026-09-15T10:00:00", "end": "2026-09-15T09:00:00",
	}), "end must be after start")
	require.Contains(t, callErr(t, session, "create_event", map[string]any{
		"name": "Bad zone", "all_day": true, "start_date": "2026-09-15", "time_zone": "Mars/Olympus",
	}), "unknown IANA time zone")
	require.Contains(t, callErr(t, session, "create_event", map[string]any{
		"name": "All day needs a date", "all_day": true, "start_date": "2026-09-15T09:00:00",
	}), "2006-01-02")
	// 02:30 does not exist in Berlin on the spring daylight-saving transition.
	require.Contains(t, callErr(t, session, "create_event", map[string]any{
		"name": "Lost hour", "start": "2026-03-29T02:30:00", "end": "2026-03-29T04:00:00", "time_zone": "Europe/Berlin",
	}), "daylight saving")

	require.Empty(t, store.events)
}

func TestListOccurrencesExpandsRecurrence(t *testing.T) {
	session, _ := connect(t, false, "")
	created := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Standup", "event_group": "Team", "rrule": "RRULE:FREQ=DAILY",
		"start": "2026-09-14T09:00:00", "end": "2026-09-14T09:15:00", "time_zone": "Europe/Istanbul",
	})

	out := call[mcp.ListOccurrencesOutput](t, session, "list_occurrences", map[string]any{
		"from": "2026-09-14", "to": "2026-09-17", "time_zone": "Europe/Istanbul",
	})
	require.Equal(t, 3, out.Count)
	require.False(t, out.Truncated)
	for i, occurrence := range out.Occurrences {
		require.Equal(t, created.Event.ID, occurrence.ID, "occurrences keep their series id")
		require.Equal(t, fmt.Sprintf("2026-09-%02dT09:00:00+03:00", 14+i), occurrence.Start)
		require.NotEmpty(t, occurrence.RecurrenceID)
	}

	limited := call[mcp.ListOccurrencesOutput](t, session, "list_occurrences", map[string]any{
		"from": "2026-09-14", "to": "2026-09-17", "time_zone": "Europe/Istanbul", "limit": 2,
	})
	require.Equal(t, 2, limited.Count)
	require.True(t, limited.Truncated)

	require.Contains(t, callErr(t, session, "list_occurrences", map[string]any{
		"from": "2026-09-17", "to": "2026-09-14",
	}), "to must be after from")
	require.Contains(t, callErr(t, session, "list_occurrences", map[string]any{
		"from": "2026-01-01", "to": "2028-01-01",
	}), "400 days")
}

func TestListOccurrencesFiltersAndHidesDisabled(t *testing.T) {
	session, _ := connect(t, false, "")
	for _, group := range []string{"Team", "Personal"} {
		call[mcp.EventOutput](t, session, "create_event", map[string]any{
			"name": group + " sync", "event_group": group,
			"start": "2026-09-15T09:00:00", "end": "2026-09-15T10:00:00",
		})
	}
	hidden := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Archived", "event_group": "Team", "disabled": true,
		"start": "2026-09-15T11:00:00", "end": "2026-09-15T12:00:00",
	})
	require.True(t, hidden.Event.Disabled)

	out := call[mcp.ListOccurrencesOutput](t, session, "list_occurrences", map[string]any{
		"from": "2026-09-15", "to": "2026-09-16", "event_group": "Team",
	})
	require.Len(t, out.Occurrences, 1)
	require.Equal(t, "Team sync", out.Occurrences[0].Name)
}

func TestListEventsSearchesCatalog(t *testing.T) {
	session, _ := connect(t, false, "")
	for _, name := range []string{"Budget planning", "Sprint planning", "Retro"} {
		call[mcp.EventOutput](t, session, "create_event", map[string]any{
			"name": name, "all_day": true, "start_date": "2026-09-15",
		})
	}

	out := call[mcp.ListEventsOutput](t, session, "list_events", map[string]any{"name": "PLANNING"})
	require.Equal(t, uint64(2), out.Total)
	require.Len(t, out.Events, 2)

	page := call[mcp.ListEventsOutput](t, session, "list_events", map[string]any{"name": "planning", "limit": 1, "offset": 1})
	require.Equal(t, uint64(2), page.Total, "total counts every match, not just the page")
	require.Len(t, page.Events, 1)
	require.Equal(t, "Sprint planning", page.Events[0].Name)
}

func TestFindFreeTime(t *testing.T) {
	session, _ := connect(t, false, "")
	call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Morning block", "start": "2026-09-15T09:00:00", "end": "2026-09-15T11:00:00", "time_zone": "Europe/Istanbul",
	})
	call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Overlapping", "start": "2026-09-15T10:30:00", "end": "2026-09-15T12:00:00", "time_zone": "Europe/Istanbul",
	})

	out := call[mcp.FindFreeTimeOutput](t, session, "find_free_time", map[string]any{
		"from": "2026-09-15", "to": "2026-09-16", "time_zone": "Europe/Istanbul",
		"duration_minutes": 60, "day_start": "09:00", "day_end": "17:00",
	})
	// Overlapping meetings merge into one busy block, leaving the afternoon free.
	require.Equal(t, []mcp.Slot{{Start: "2026-09-15T12:00:00+03:00", End: "2026-09-15T17:00:00+03:00", Minutes: 300}}, out.Slots)
	require.Equal(t, "Europe/Istanbul", out.TimeZone)
}

func TestFindFreeTimeSkipsAllDayAndWeekends(t *testing.T) {
	session, _ := connect(t, false, "")
	// 2026-09-18 is a Friday; the 19th and 20th are the weekend.
	call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Public holiday", "all_day": true, "start_date": "2026-09-18", "time_zone": "Europe/Istanbul",
	})

	out := call[mcp.FindFreeTimeOutput](t, session, "find_free_time", map[string]any{
		"from": "2026-09-18", "to": "2026-09-22", "time_zone": "Europe/Istanbul", "duration_minutes": 30,
	})
	require.Len(t, out.Slots, 1, "the holiday and the weekend are unavailable")
	require.Equal(t, "2026-09-21T09:00:00+03:00", out.Slots[0].Start)

	weekend := call[mcp.FindFreeTimeOutput](t, session, "find_free_time", map[string]any{
		"from": "2026-09-19", "to": "2026-09-21", "time_zone": "Europe/Istanbul",
		"duration_minutes": 30, "include_weekends": true,
	})
	require.Len(t, weekend.Slots, 2)

	require.Contains(t, callErr(t, session, "find_free_time", map[string]any{
		"from": "2026-09-15", "to": "2026-09-16", "duration_minutes": 0,
	}), "duration_minutes must be positive")
	require.Contains(t, callErr(t, session, "find_free_time", map[string]any{
		"from": "2026-09-15", "to": "2026-09-16", "duration_minutes": 30, "day_start": "18:00", "day_end": "09:00",
	}), "day_end must be after day_start")
}

func TestUpdateEventChangesOnlySuppliedFields(t *testing.T) {
	session, store := connect(t, false, "")
	created := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Review", "description": "Keep me", "event_group": "Team",
		"start": "2026-09-15T09:00:00", "end": "2026-09-15T10:00:00", "time_zone": "Europe/Istanbul",
	})

	renamed := call[mcp.EventOutput](t, session, "update_event", map[string]any{
		"id": created.Event.ID, "name": "Renamed review",
	})
	require.Equal(t, "Renamed review", renamed.Event.Name)
	require.Equal(t, "Keep me", renamed.Event.Description)
	require.Equal(t, created.Event.Start, renamed.Event.Start, "timing is untouched when it is not supplied")

	moved := call[mcp.EventOutput](t, session, "update_event", map[string]any{
		"id": created.Event.ID, "start": "2026-09-16T14:00:00", "end": "2026-09-16T15:00:00",
	})
	require.Equal(t, "2026-09-16T14:00:00+03:00", moved.Event.Start)
	require.Equal(t, "Renamed review", moved.Event.Name)

	converted := call[mcp.EventOutput](t, session, "update_event", map[string]any{
		"id": created.Event.ID, "all_day": true, "start_date": "2026-09-16", "end_date": "2026-09-17",
	})
	require.True(t, converted.Event.AllDay)
	require.Equal(t, "2026-09-17", converted.Event.EndDate)

	stored, err := store.GetEvent(t.Context(), created.Event.ID)
	require.NoError(t, err)
	require.Equal(t, "Europe/Istanbul", stored.Tz, "the event keeps its zone")

	require.Contains(t, callErr(t, session, "update_event", map[string]any{"id": "missing"}), "not found")
	require.Contains(t, callErr(t, session, "update_event", map[string]any{
		"id": created.Event.ID, "all_day": false,
	}), "requires the matching start and end")
}

func TestCancelOccurrenceKeepsTheRestOfTheSeries(t *testing.T) {
	session, store := connect(t, false, "")
	created := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Standup", "rrule": "RRULE:FREQ=DAILY",
		"start": "2026-09-14T09:00:00", "end": "2026-09-14T09:15:00", "time_zone": "Europe/Istanbul",
	})

	out := call[mcp.MessageOutput](t, session, "cancel_occurrence", map[string]any{
		"id": created.Event.ID, "occurrence_start": "2026-09-15T09:00:00", "time_zone": "Europe/Istanbul",
	})
	require.Contains(t, out.Message, "Cancelled")

	remaining := call[mcp.ListOccurrencesOutput](t, session, "list_occurrences", map[string]any{
		"from": "2026-09-14", "to": "2026-09-17", "time_zone": "Europe/Istanbul",
	})
	require.Equal(t, 2, remaining.Count)
	for _, occurrence := range remaining.Occurrences {
		require.NotEqual(t, "2026-09-15T09:00:00+03:00", occurrence.Start)
	}

	repeated := call[mcp.MessageOutput](t, session, "cancel_occurrence", map[string]any{
		"id": created.Event.ID, "occurrence_start": "2026-09-15T09:00:00", "time_zone": "Europe/Istanbul",
	})
	require.Contains(t, repeated.Message, "already cancelled")
	stored, err := store.GetEvent(t.Context(), created.Event.ID)
	require.NoError(t, err)
	require.Len(t, stored.Recurrence.Overrides, 1, "cancelling twice must not duplicate the exception")

	require.Contains(t, callErr(t, session, "cancel_occurrence", map[string]any{
		"id": created.Event.ID, "occurrence_start": "2026-09-15T18:00:00", "time_zone": "Europe/Istanbul",
	}), "no occurrence")

	single := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "One off", "start": "2026-09-15T13:00:00", "end": "2026-09-15T14:00:00", "time_zone": "Europe/Istanbul",
	})
	require.Contains(t, callErr(t, session, "cancel_occurrence", map[string]any{
		"id": single.Event.ID, "occurrence_start": "2026-09-15T13:00:00", "time_zone": "Europe/Istanbul",
	}), "does not repeat")
}

func TestDeleteEvent(t *testing.T) {
	session, store := connect(t, false, "")
	created := call[mcp.EventOutput](t, session, "create_event", map[string]any{
		"name": "Temporary", "all_day": true, "start_date": "2026-09-15",
	})

	out := call[mcp.DeleteEventOutput](t, session, "delete_event", map[string]any{
		"ids": []string{created.Event.ID},
	})
	require.Equal(t, 1, out.Deleted)
	require.Empty(t, store.events)

	require.Contains(t, callErr(t, session, "delete_event", map[string]any{"ids": []string{"  "}}),
		"at least one event id")
}
