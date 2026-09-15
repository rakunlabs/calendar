// Package mcp exposes the calendar over the Model Context Protocol so an AI
// client can read the schedule and, unless the server is read-only, change it.
// Tools call the same service the REST API uses, so recurrence, time zone and
// concurrency rules are identical; nothing here is a second source of truth.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rakunlabs/query"
	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/core/domain"
	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/ical"
)

// MaxRangeDays matches the REST occurrences endpoint; a wider window is rejected
// instead of being silently truncated.
const MaxRangeDays = 400

const instructions = `Calendar over MCP.

Read the schedule with list_occurrences (expanded, recurrence-aware) and search the
catalog with list_events. find_free_time returns open slots for scheduling.

Times: supply an IANA time_zone (for example Europe/Istanbul). Timed values accept
RFC3339 or wall-clock "2006-01-02T15:04:05" read in that zone. All-day events use
plain dates, and end_date is the inclusive last day.

Writes: create_event and update_event change the whole series. update_event needs
the event id from a read tool. cancel_occurrence removes one occurrence of a
recurring event; delete_event removes the event itself.`

// Handler serves MCP over streamable HTTP.
type Handler struct {
	service  port.CalendarService
	readOnly bool
}

func New(service port.CalendarService, readOnly bool) *Handler {
	return &Handler{service: service, readOnly: readOnly}
}

// HTTPHandler builds a stateless MCP endpoint. Each request gets its own server so
// the audit label travels with the caller instead of leaking between sessions.
func (h *Handler) HTTPHandler() http.Handler {
	return sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		user := strings.TrimSpace(r.Header.Get("X-User"))
		if user == "" {
			user = "mcp"
		}
		return h.server(user)
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func (h *Handler) server(user string) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "calendar", Title: "Calendar"},
		&sdk.ServerOptions{Instructions: instructions})

	read := &sdk.ToolAnnotations{ReadOnlyHint: true}
	sdk.AddTool(s, &sdk.Tool{Name: "list_occurrences", Annotations: read,
		Description: "List calendar occurrences in a date range, expanding recurring events. Use this to answer what is scheduled."},
		h.listOccurrences)
	sdk.AddTool(s, &sdk.Tool{Name: "list_events", Annotations: read,
		Description: "Search the event catalog by name, group or entity. Returns stored events with their ids and repeat rules, not expanded occurrences."},
		h.listEvents)
	sdk.AddTool(s, &sdk.Tool{Name: "find_free_time", Annotations: read,
		Description: "Find free slots of a given length within working hours, skipping everything already scheduled."},
		h.findFreeTime)

	if h.readOnly {
		return s
	}

	sdk.AddTool(s, &sdk.Tool{Name: "create_event",
		Annotations: &sdk.ToolAnnotations{DestructiveHint: ptr(false)},
		Description: "Create a calendar event. Set all_day with start_date/end_date, or supply start/end for a timed event."},
		h.createEvent(user))
	sdk.AddTool(s, &sdk.Tool{Name: "update_event",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true},
		Description: "Update an existing event by id. Only the supplied fields change; this edits the whole series."},
		h.updateEvent(user))
	sdk.AddTool(s, &sdk.Tool{Name: "cancel_occurrence",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true},
		Description: "Cancel a single occurrence of a recurring event, leaving the rest of the series in place."},
		h.cancelOccurrence(user))
	sdk.AddTool(s, &sdk.Tool{Name: "delete_event",
		Description: "Permanently delete events by id, including every occurrence of a recurring event."},
		h.deleteEvent)

	return s
}

func ptr[T any](v T) *T { return &v }

// Occurrence is the model-facing shape of an event or one of its occurrences.
type Occurrence struct {
	ID           string `json:"id" jsonschema:"Event id; pass this to update_event or delete_event"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	EventGroup   string `json:"event_group,omitempty"`
	AllDay       bool   `json:"all_day"`
	Start        string `json:"start,omitempty" jsonschema:"Start of a timed occurrence, RFC3339 in its own zone"`
	End          string `json:"end,omitempty" jsonschema:"Exclusive end of a timed occurrence"`
	StartDate    string `json:"start_date,omitempty" jsonschema:"First day of an all-day event"`
	EndDate      string `json:"end_date,omitempty" jsonschema:"Inclusive last day of an all-day event"`
	TimeZone     string `json:"time_zone,omitempty"`
	RRule        string `json:"rrule,omitempty" jsonschema:"Repeat rule, empty when the event does not repeat"`
	RecurrenceID string `json:"recurrence_id,omitempty" jsonschema:"Identity of this occurrence within its series"`
	Disabled     bool   `json:"disabled,omitempty"`
}

type rangeInput struct {
	From       string `json:"from" jsonschema:"Inclusive range start, a date or timestamp"`
	To         string `json:"to" jsonschema:"Exclusive range end, at most 400 days after from"`
	TimeZone   string `json:"time_zone,omitempty" jsonschema:"IANA zone used to read from/to and report results, default UTC"`
	Entity     string `json:"entity,omitempty" jsonschema:"Only events assigned to this entity"`
	EventGroup string `json:"event_group,omitempty" jsonschema:"Only events in this group"`
}

type ListOccurrencesInput struct {
	rangeInput
	Limit int `json:"limit,omitempty" jsonschema:"Maximum occurrences to return, default 200"`
}

type ListOccurrencesOutput struct {
	Occurrences []Occurrence `json:"occurrences"`
	Count       int          `json:"count"`
	Truncated   bool         `json:"truncated,omitempty" jsonschema:"True when more occurrences exist than were returned"`
}

func (h *Handler) listOccurrences(ctx context.Context, _ *sdk.CallToolRequest, in ListOccurrencesInput) (*sdk.CallToolResult, ListOccurrencesOutput, error) {
	occurrences, _, err := h.expand(ctx, in.rangeInput)
	if err != nil {
		return nil, ListOccurrencesOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	out := ListOccurrencesOutput{Count: len(occurrences), Occurrences: []Occurrence{}}
	if len(occurrences) > limit {
		occurrences, out.Truncated = occurrences[:limit], true
	}
	for _, occurrence := range occurrences {
		out.Occurrences = append(out.Occurrences, describe(occurrence))
	}
	out.Count = len(out.Occurrences)
	return nil, out, nil
}

type ListEventsInput struct {
	Name            string `json:"name,omitempty" jsonschema:"Case-insensitive substring of the event name"`
	EventGroup      string `json:"event_group,omitempty"`
	Entity          string `json:"entity,omitempty"`
	IncludeDisabled bool   `json:"include_disabled,omitempty"`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum events to return, default 25"`
	Offset          int    `json:"offset,omitempty"`
}

type ListEventsOutput struct {
	Events []Occurrence `json:"events"`
	Total  uint64       `json:"total" jsonschema:"Total matching events, which may exceed the returned page"`
}

func (h *Handler) listEvents(ctx context.Context, _ *sdk.CallToolRequest, in ListEventsInput) (*sdk.CallToolResult, ListEventsOutput, error) {
	q := newQuery()
	addEq(q, "entity", in.Entity)
	addEq(q, "event_group", in.EventGroup)
	if name := strings.TrimSpace(in.Name); name != "" {
		add(q, query.NewExpressionCmp(query.OperatorILike, "name", "%"+name+"%"))
	}
	if !in.IncludeDisabled {
		add(q, query.NewExpressionCmp(query.OperatorEq, "disabled", false))
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	q.SetLimit(uint64(limit))
	if in.Offset > 0 {
		q.SetOffset(uint64(in.Offset))
	}

	events, err := h.service.GetEvents(ctx, q)
	if err != nil {
		return nil, ListEventsOutput{}, err
	}
	total, err := h.service.GetEventsCount(ctx, q)
	if err != nil {
		return nil, ListEventsOutput{}, err
	}
	out := ListEventsOutput{Total: total, Events: []Occurrence{}}
	for _, event := range events {
		out.Events = append(out.Events, describe(event))
	}
	return nil, out, nil
}

type FindFreeTimeInput struct {
	rangeInput
	DurationMinutes int    `json:"duration_minutes" jsonschema:"Length of the slot to look for"`
	DayStart        string `json:"day_start,omitempty" jsonschema:"Earliest time of day to consider, HH:MM, default 09:00"`
	DayEnd          string `json:"day_end,omitempty" jsonschema:"Latest time of day to consider, HH:MM, default 18:00"`
	IncludeWeekends bool   `json:"include_weekends,omitempty"`
	Limit           int    `json:"limit,omitempty" jsonschema:"Maximum slots to return, default 20"`
}

type Slot struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Minutes int    `json:"minutes"`
}

type FindFreeTimeOutput struct {
	Slots    []Slot `json:"slots"`
	TimeZone string `json:"time_zone"`
}

func (h *Handler) findFreeTime(ctx context.Context, _ *sdk.CallToolRequest, in FindFreeTimeInput) (*sdk.CallToolResult, FindFreeTimeOutput, error) {
	if in.DurationMinutes <= 0 {
		return nil, FindFreeTimeOutput{}, fmt.Errorf("duration_minutes must be positive")
	}
	dayStart, err := parseClock(in.DayStart, 9*60)
	if err != nil {
		return nil, FindFreeTimeOutput{}, fmt.Errorf("day_start: %w", err)
	}
	dayEnd, err := parseClock(in.DayEnd, 18*60)
	if err != nil {
		return nil, FindFreeTimeOutput{}, fmt.Errorf("day_end: %w", err)
	}
	if dayEnd <= dayStart {
		return nil, FindFreeTimeOutput{}, fmt.Errorf("day_end must be after day_start")
	}
	occurrences, window, err := h.expand(ctx, in.rangeInput)
	if err != nil {
		return nil, FindFreeTimeOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	slots := freeSlots(occurrences, window, time.Duration(in.DurationMinutes)*time.Minute,
		dayStart, dayEnd, in.IncludeWeekends, limit)
	return nil, FindFreeTimeOutput{Slots: slots, TimeZone: window.loc.String()}, nil
}

type EventInput struct {
	Name        string `json:"name" jsonschema:"Event title"`
	Description string `json:"description,omitempty"`
	EventGroup  string `json:"event_group,omitempty" jsonschema:"Calendar group this event belongs to"`
	AllDay      bool   `json:"all_day,omitempty"`
	Start       string `json:"start,omitempty" jsonschema:"Start of a timed event; RFC3339 or wall-clock in time_zone"`
	End         string `json:"end,omitempty" jsonschema:"Exclusive end of a timed event"`
	StartDate   string `json:"start_date,omitempty" jsonschema:"First day of an all-day event, YYYY-MM-DD"`
	EndDate     string `json:"end_date,omitempty" jsonschema:"Inclusive last day of an all-day event; defaults to start_date"`
	TimeZone    string `json:"time_zone,omitempty" jsonschema:"IANA zone of this event, default UTC"`
	RRule       string `json:"rrule,omitempty" jsonschema:"Repeat rule such as RRULE:FREQ=WEEKLY;BYDAY=MO"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type EventOutput struct {
	Event Occurrence `json:"event"`
}

func (h *Handler) createEvent(user string) sdk.ToolHandlerFor[EventInput, EventOutput] {
	return func(ctx context.Context, _ *sdk.CallToolRequest, in EventInput) (*sdk.CallToolResult, EventOutput, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, EventOutput{}, fmt.Errorf("name is required")
		}
		loc, err := zone(in.TimeZone)
		if err != nil {
			return nil, EventOutput{}, err
		}
		start, end := in.Start, in.End
		if in.AllDay {
			start, end = in.StartDate, in.EndDate
		}
		from, to, err := eventTimes(in.AllDay, start, end, loc)
		if err != nil {
			return nil, EventOutput{}, err
		}
		event := domain.Event{
			Name: in.Name, Description: in.Description, AllDay: in.AllDay,
			DateFrom: from, DateTo: to, Tz: loc.String(), RRule: strings.TrimSpace(in.RRule),
			Disabled: in.Disabled, UpdatedBy: user,
		}
		if group := strings.TrimSpace(in.EventGroup); group != "" {
			event.EventGroup = types.Null[string]{V: group, Valid: true}
		}
		events := []domain.Event{event}
		if err := h.service.AddEvents(ctx, events); err != nil {
			return nil, EventOutput{}, err
		}
		return nil, EventOutput{Event: describe(events[0])}, nil
	}
}

type UpdateEventInput struct {
	ID          string  `json:"id" jsonschema:"Event id from list_events or list_occurrences"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	EventGroup  *string `json:"event_group,omitempty"`
	AllDay      *bool   `json:"all_day,omitempty"`
	Start       *string `json:"start,omitempty"`
	End         *string `json:"end,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	TimeZone    *string `json:"time_zone,omitempty"`
	RRule       *string `json:"rrule,omitempty" jsonschema:"Repeat rule; pass an empty string to stop repeating"`
	Disabled    *bool   `json:"disabled,omitempty"`
}

func (h *Handler) updateEvent(user string) sdk.ToolHandlerFor[UpdateEventInput, EventOutput] {
	return func(ctx context.Context, _ *sdk.CallToolRequest, in UpdateEventInput) (*sdk.CallToolResult, EventOutput, error) {
		current, err := h.load(ctx, in.ID)
		if err != nil {
			return nil, EventOutput{}, err
		}
		updated := *current
		apply(&updated.Name, in.Name)
		apply(&updated.Description, in.Description)
		apply(&updated.Disabled, in.Disabled)
		if in.RRule != nil {
			updated.RRule = strings.TrimSpace(*in.RRule)
		}
		if in.EventGroup != nil {
			group := strings.TrimSpace(*in.EventGroup)
			updated.EventGroup = types.Null[string]{V: group, Valid: group != ""}
		}
		loc := current.DateFrom.Location()
		if in.TimeZone != nil {
			if loc, err = zone(*in.TimeZone); err != nil {
				return nil, EventOutput{}, err
			}
			updated.Tz = loc.String()
		}
		allDay := current.AllDay
		apply(&allDay, in.AllDay)
		if timing := in.Start != nil || in.End != nil || in.StartDate != nil || in.EndDate != nil; timing || allDay != current.AllDay || in.TimeZone != nil {
			start, end := value(in.Start, describe(*current).Start), value(in.End, describe(*current).End)
			if allDay {
				start, end = value(in.StartDate, describe(*current).StartDate), value(in.EndDate, describe(*current).EndDate)
			}
			// Switching between timed and all-day leaves the other pair empty.
			if strings.TrimSpace(start) == "" {
				return nil, EventOutput{}, fmt.Errorf("changing all_day requires the matching start and end fields")
			}
			from, to, err := eventTimes(allDay, start, end, loc)
			if err != nil {
				return nil, EventOutput{}, err
			}
			updated.AllDay, updated.DateFrom, updated.DateTo = allDay, from, to
		}
		if err := h.service.UpdateEvent(ctx, in.ID, &updated); err != nil {
			return nil, EventOutput{}, err
		}
		return nil, EventOutput{Event: describe(updated)}, nil
	}
}

type CancelOccurrenceInput struct {
	ID              string `json:"id" jsonschema:"Recurring event id"`
	OccurrenceStart string `json:"occurrence_start" jsonschema:"Start of the occurrence to cancel, as reported by list_occurrences"`
	TimeZone        string `json:"time_zone,omitempty" jsonschema:"Zone used to read occurrence_start, default the event zone"`
}

type MessageOutput struct {
	Message string `json:"message"`
}

func (h *Handler) cancelOccurrence(user string) sdk.ToolHandlerFor[CancelOccurrenceInput, MessageOutput] {
	return func(ctx context.Context, _ *sdk.CallToolRequest, in CancelOccurrenceInput) (*sdk.CallToolResult, MessageOutput, error) {
		current, err := h.load(ctx, in.ID)
		if err != nil {
			return nil, MessageOutput{}, err
		}
		if strings.TrimSpace(current.RRule) == "" && (current.Recurrence == nil || len(current.Recurrence.RDates) == 0) {
			return nil, MessageOutput{}, fmt.Errorf("%q does not repeat; use delete_event instead", current.Name)
		}
		loc := current.DateFrom.Location()
		if strings.TrimSpace(in.TimeZone) != "" {
			if loc, err = zone(in.TimeZone); err != nil {
				return nil, MessageOutput{}, err
			}
		}
		start, err := parseMoment(in.OccurrenceStart, loc)
		if err != nil {
			return nil, MessageOutput{}, fmt.Errorf("occurrence_start: %w", err)
		}
		// A cancelled occurrence no longer expands, so recorded exceptions are
		// matched first; otherwise a repeat call would look like a missing occurrence.
		if current.Recurrence != nil {
			for _, override := range current.Recurrence.Overrides {
				if !override.Cancelled {
					continue
				}
				when, err := ical.ResolveCalendarDate(override.RecurrenceID, loc, current.Recurrence.Timezones)
				if err != nil {
					return nil, MessageOutput{}, err
				}
				if when.Equal(start) {
					return nil, MessageOutput{Message: "Occurrence was already cancelled."}, nil
				}
			}
		}
		// Expanding the surrounding day keeps the stored occurrence identity, which
		// is its original scheduled start and not necessarily this instant.
		day := start.AddDate(0, 0, -1)
		occurrences, err := ical.Occurrences(ctx, *current, day, day.AddDate(0, 0, 3))
		if err != nil {
			return nil, MessageOutput{}, err
		}
		index := slices.IndexFunc(occurrences, func(o domain.Event) bool { return o.DateFrom.Equal(start) })
		if index < 0 {
			return nil, MessageOutput{}, fmt.Errorf("no occurrence of %q starts at %s", current.Name, start.Format(time.RFC3339))
		}
		target := occurrences[index]
		if target.RecurrenceID == nil {
			return nil, MessageOutput{}, fmt.Errorf("occurrence of %q has no recurrence identity to cancel", current.Name)
		}
		updated := *current
		updated.UpdatedBy = user
		recurrence := domain.Recurrence{}
		if current.Recurrence != nil {
			recurrence = *current.Recurrence
			recurrence.Overrides = slices.Clone(current.Recurrence.Overrides)
		}
		recurrence.Overrides = append(recurrence.Overrides,
			domain.OccurrenceOverride{RecurrenceID: *target.RecurrenceID, Cancelled: true})
		updated.Recurrence = &recurrence
		if err := h.service.UpdateEvent(ctx, in.ID, &updated); err != nil {
			return nil, MessageOutput{}, err
		}
		return nil, MessageOutput{Message: fmt.Sprintf("Cancelled the %s occurrence of %q. The rest of the series is unchanged.",
			start.Format(time.RFC3339), current.Name)}, nil
	}
}

type DeleteEventInput struct {
	IDs []string `json:"ids" jsonschema:"Event ids to delete"`
}

type DeleteEventOutput struct {
	Deleted int `json:"deleted"`
}

func (h *Handler) deleteEvent(ctx context.Context, _ *sdk.CallToolRequest, in DeleteEventInput) (*sdk.CallToolResult, DeleteEventOutput, error) {
	ids := []string{}
	for _, id := range in.IDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, DeleteEventOutput{}, fmt.Errorf("ids must contain at least one event id")
	}
	if err := h.service.RemoveEvent(ctx, ids...); err != nil {
		return nil, DeleteEventOutput{}, err
	}
	return nil, DeleteEventOutput{Deleted: len(ids)}, nil
}

func (h *Handler) load(ctx context.Context, id string) (*domain.Event, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("id is required")
	}
	event, err := h.service.GetEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fmt.Errorf("%w: event %s not found", port.ErrInvalidEvent, id)
	}
	return event, nil
}

type window struct {
	from, to time.Time
	loc      *time.Location
}

// expand resolves the requested range and returns every occurrence overlapping it.
func (h *Handler) expand(ctx context.Context, in rangeInput) ([]domain.Event, window, error) {
	loc, err := zone(in.TimeZone)
	if err != nil {
		return nil, window{}, err
	}
	from, err := parseMoment(in.From, loc)
	if err != nil {
		return nil, window{}, fmt.Errorf("from: %w", err)
	}
	to, err := parseMoment(in.To, loc)
	if err != nil {
		return nil, window{}, fmt.Errorf("to: %w", err)
	}
	if !to.After(from) {
		return nil, window{}, fmt.Errorf("to must be after from")
	}
	if to.Sub(from) > MaxRangeDays*24*time.Hour {
		return nil, window{}, fmt.Errorf("range must not exceed %d days", MaxRangeDays)
	}
	q := newQuery()
	addEq(q, "entity", in.Entity)
	addEq(q, "event_group", in.EventGroup)
	events, err := h.service.GetEvents(ctx, q)
	if err != nil {
		return nil, window{}, err
	}
	result := []domain.Event{}
	for _, event := range events {
		if event.Disabled {
			continue
		}
		occurrences, err := ical.Occurrences(ctx, event, from, to)
		if err != nil {
			return nil, window{}, err
		}
		result = append(result, occurrences...)
		if len(result) > 20000 {
			return nil, window{}, errors.New("too many occurrences; select a smaller range")
		}
	}
	slices.SortFunc(result, func(a, b domain.Event) int { return a.DateFrom.Compare(b.DateFrom.Time) })
	return result, window{from: from, to: to, loc: loc}, nil
}

func newQuery() *query.Query {
	q := query.New()
	q.Values = make(map[string][]*query.ExpressionCmp)
	return q
}

func add(q *query.Query, expr *query.ExpressionCmp) {
	q.Values[expr.Field] = append(q.Values[expr.Field], expr)
	q.Where = append(q.Where, expr)
}

func addEq(q *query.Query, field, value string) {
	if value = strings.TrimSpace(value); value != "" {
		add(q, query.NewExpressionCmp(query.OperatorEq, field, value))
	}
}

func apply[T any](target *T, value *T) {
	if value != nil {
		*target = *value
	}
}

func value(given *string, fallback string) string {
	if given != nil {
		return *given
	}
	return fallback
}
