package repository

import (
	"net/url"
	"testing"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/rakunlabs/query"
	"github.com/stretchr/testify/suite"
	"github.com/worldline-go/test/container/containerpostgres"
	"github.com/worldline-go/types"
)

var migrations = []string{
	"migrations/01_events.sql",
	"migrations/02_relations.sql",
	"migrations/03_relation_uniqueness.sql",
}

type DatabaseSuite struct {
	suite.Suite
	container *containerpostgres.Container
	db        *Database
}

func (s *DatabaseSuite) SetupSuite() {
	s.container = containerpostgres.New(s.T())
	s.container.ExecuteFiles(s.T(), migrations)

	s.db = newDB(s.container.Sql(), "public")
}

func TestDatabase(t *testing.T) {
	suite.Run(t, new(DatabaseSuite))
}

func (s *DatabaseSuite) TestCountIgnoresPaginationAndSort() {
	ctx := s.T().Context()
	start := time.Now()
	events := []models.Event{
		{Name: "Count test one", EventGroup: types.NewNull("count-test"), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}},
		{Name: "Count test two", EventGroup: types.NewNull("count-test"), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}},
	}
	s.Require().NoError(s.db.AddEvents(ctx, events))
	defer s.db.RemoveEvent(ctx, events[0].ID, events[1].ID)
	q, err := query.Parse("event_group=count-test&_limit=1&_offset=10&_sort=name")
	s.Require().NoError(err)
	count, err := s.db.GetEventsCount(ctx, q)
	s.Require().NoError(err)
	s.Equal(uint64(2), count)
	s.Require().NoError(s.db.AddRelations(ctx, []models.Relation{
		{Entity: "count-test", EventID: types.NewNull(events[0].ID)},
		{Entity: "count-test", EventID: types.NewNull(events[1].ID)},
	}))
	q, err = query.Parse("entity=count-test&_limit=1&_offset=10&_sort=event_id")
	s.Require().NoError(err)
	count, err = s.db.GetRelationsCount(ctx, q)
	s.Require().NoError(err)
	s.Equal(uint64(2), count)
}

func (s *DatabaseSuite) TestRelationIdentityAndEventFiltering() {
	ctx := s.T().Context()
	entity, group := "R&D, (west)|other +%", "group, (one)&two"
	start := time.Now()
	events := []models.Event{
		{ID: "relation-direct", Name: "direct", EventGroup: types.NewNull("elsewhere"), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}},
		{ID: "relation-group", Name: "group", EventGroup: types.NewNull(group), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}},
		{ID: "relation-unrelated", Name: "unrelated", EventGroup: types.NewNull("elsewhere"), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}},
	}
	s.Require().NoError(s.db.AddEvents(ctx, events))
	defer s.db.RemoveEvent(ctx, events[0].ID, events[1].ID, events[2].ID)
	relations := []models.Relation{
		{Entity: entity, EventGroup: types.NewNull(group)},
		{Entity: entity, EventID: types.NewNull(events[0].ID)},
		{Entity: entity, EventGroup: types.NewNull(group), EventID: types.NewNull(events[0].ID)},
		{Entity: "unmatched", EventGroup: types.NewNull("missing-group")},
		{Entity: "other-entity", EventGroup: types.NewNull(group)},
	}
	for range 2 {
		s.Require().NoError(s.db.AddRelations(ctx, relations))
	}
	q, err := query.Parse("", query.WithExpressionCmp("entity", query.NewExpressionCmp(query.OperatorEq, "entity", entity)))
	s.Require().NoError(err)
	defer s.db.RemoveRelation(ctx, q)
	gotRelations, err := s.db.GetRelations(ctx, q)
	s.Require().NoError(err)
	s.Require().Len(gotRelations, 3)
	got, err := s.db.GetEvents(ctx, q)
	s.Require().NoError(err)
	s.Require().Len(got, 2) // Group OR direct matches, deduplicated across rules.
	filtered, err := query.Parse("", query.WithExpressionCmp("entity", query.NewExpressionCmp(query.OperatorEq, "entity", entity)),
		query.WithExpressionCmp("event_group", query.NewExpressionCmp(query.OperatorEq, "event_group", group)))
	s.Require().NoError(err)
	got, err = s.db.GetEvents(ctx, filtered)
	s.Require().NoError(err)
	s.Require().Len(got, 1)
	s.Equal(events[1].ID, got[0].ID)
	count, err := s.db.GetEventsCount(ctx, filtered)
	s.Require().NoError(err)
	s.Equal(uint64(1), count)
	unmatched, err := query.Parse("entity=unmatched")
	s.Require().NoError(err)
	got, err = s.db.GetEvents(ctx, unmatched)
	s.Require().NoError(err)
	s.Empty(got)

	// Delete a group-only row, then an event-only row, without touching the
	// combined rule or another entity's assignment to the same group.
	for _, field := range []string{"event_group", "event_id"} {
		value, nullField := group, "event_id"
		if field == "event_id" {
			value, nullField = events[0].ID, "event_group"
		}
		exact, err := query.Parse("", query.WithExpressionCmp("entity", query.NewExpressionCmp(query.OperatorEq, "entity", entity)),
			query.WithExpressionCmp(field, query.NewExpressionCmp(query.OperatorEq, field, value)),
			query.WithExpressionCmp(nullField, query.NewExpressionCmp(query.OperatorIs, nullField, nil)))
		s.Require().NoError(err)
		for range 2 {
			s.Require().NoError(s.db.RemoveRelation(ctx, exact))
		}
	}
	gotRelations, err = s.db.GetRelations(ctx, q)
	s.Require().NoError(err)
	s.Require().Len(gotRelations, 1)
	s.True(gotRelations[0].EventGroup.Valid && gotRelations[0].EventID.Valid)
	// Both-target rows retain their OR behavior after the individual rows go.
	got, err = s.db.GetEvents(ctx, q)
	s.Require().NoError(err)
	s.Len(got, 2)
	exact, err := query.Parse(url.Values{"entity[eq]": {entity}, "event_group[eq]": {group}, "event_id[eq]": {events[0].ID}}.Encode())
	s.Require().NoError(err)
	s.Require().NoError(s.db.RemoveRelation(ctx, exact))
	gotRelations, err = s.db.GetRelations(ctx, q)
	s.Require().NoError(err)
	s.Empty(gotRelations)
	other, err := query.Parse("entity=other-entity")
	s.Require().NoError(err)
	gotRelations, err = s.db.GetRelations(ctx, other)
	s.Require().NoError(err)
	s.Len(gotRelations, 1)
	s.Require().NoError(s.db.RemoveRelation(ctx, other))
	s.Require().NoError(s.db.RemoveRelation(ctx, unmatched))
}

func (s *DatabaseSuite) TestEventsNoDefaultLimit() {
	ctx := s.T().Context()
	start := time.Now()
	events := make([]models.Event, 230)
	for i := range events {
		events[i] = models.Event{Name: "unpaginated", EventGroup: types.NewNull("unpaginated"), DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}}
	}
	s.Require().NoError(s.db.AddEvents(ctx, events))
	defer s.db.q.Delete(TableEvents).Where(goqu.Ex{"event_group": "unpaginated"}).Executor().ExecContext(ctx)
	q, err := query.Parse("event_group[eq]=unpaginated")
	s.Require().NoError(err)
	got, err := s.db.GetEvents(ctx, q)
	s.Require().NoError(err)
	s.Len(got, len(events))
}

func (s *DatabaseSuite) TearDownSuite() {
	s.container.Stop(s.T())
}

func (s *DatabaseSuite) TestAddEvents() {
	events := []models.Event{
		{
			Name:        "New Year",
			Description: "The most wonderful time of the year",
			DateFrom:    types.Time{Time: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
			DateTo:      types.Time{Time: time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)},
		},
	}

	err := s.db.AddEvents(s.T().Context(), events)
	s.Require().NoError(err)

	parse, err := query.Parse("", query.WithExpressionCmp("id", &query.ExpressionCmp{
		Operator: query.OperatorEq,
		Field:    "id",
		Value:    events[0].ID,
	}))
	s.Require().NoError(err)

	result, err := s.db.GetEvents(s.T().Context(), parse)
	s.Require().NoError(err)

	s.Require().Len(result, 1)
	s.Require().Equal(events[0].ID, result[0].ID)
	s.Require().Equal(events[0].Name, result[0].Name)
	s.Require().Equal(events[0].Description, result[0].Description)
	s.Require().Equal(events[0].DateFrom.Local(), result[0].DateFrom.Local(), "DateFrom wrong")
	s.Require().Equal(events[0].DateTo.Local(), result[0].DateTo.Local(), "DateTo wrong")
	s.Require().Equal(events[0].RRule, result[0].RRule)
	s.Require().Equal(events[0].Disabled, result[0].Disabled)
	s.Require().Equal(events[0].UpdatedAt.Truncate(time.Millisecond), result[0].UpdatedAt.Truncate(time.Millisecond), "UpdatedAt wrong")
	s.Require().Equal(events[0].UpdatedBy, result[0].UpdatedBy)

	// remove events
	err = s.db.RemoveEvent(s.T().Context(), events[0].ID)
	s.Require().NoError(err)
	// check if removed
	parse, err = query.Parse("", query.WithExpressionCmp("id", &query.ExpressionCmp{
		Operator: query.OperatorEq,
		Field:    "id",
		Value:    events[0].ID,
	}))
	s.Require().NoError(err)

	result, err = s.db.GetEvents(s.T().Context(), parse)
	s.Require().NoError(err)
	s.Require().Len(result, 0)
}

func (s *DatabaseSuite) TestUpdateEvent() {
	// Add an event
	event := models.Event{
		Name:        "Original Event",
		Description: "Original Description",
		DateFrom:    types.Time{Time: time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC)},
		DateTo:      types.Time{Time: time.Date(2023, 2, 2, 0, 0, 0, 0, time.UTC)},
	}
	err := s.db.AddEvents(s.T().Context(), []models.Event{event})
	s.Require().NoError(err)

	// Fetch the event to get its ID
	parse, err := query.Parse("", query.WithExpressionCmp("name", &query.ExpressionCmp{
		Operator: query.OperatorEq,
		Field:    "name",
		Value:    event.Name,
	}))
	s.Require().NoError(err)
	result, err := s.db.GetEvents(s.T().Context(), parse)
	s.Require().NoError(err)
	s.Require().Len(result, 1)
	eventID := result[0].ID

	// Update the event
	updated := result[0]
	updated.Name = "Updated Event"
	updated.Description = "Updated Description"
	err = s.db.UpdateEvent(s.T().Context(), eventID, &updated)
	s.Require().NoError(err)

	// Fetch and check
	got, err := s.db.GetEvent(s.T().Context(), eventID)
	s.Require().NoError(err)
	s.Require().NotNil(got)
	s.Require().Equal("Updated Event", got.Name)
	s.Require().Equal("Updated Description", got.Description)

	// Cleanup
	_ = s.db.RemoveEvent(s.T().Context(), eventID)
}

func (s *DatabaseSuite) TestGetEventNotFound() {
	got, err := s.db.GetEvent(s.T().Context(), "non-existent-id")
	s.Require().NoError(err)
	s.Require().Nil(got)
}

func (s *DatabaseSuite) TestAddMultipleEvents() {
	events := []models.Event{
		{
			Name:        "Event 1",
			Description: "Desc 1",
			DateFrom:    types.Time{Time: time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC)},
			DateTo:      types.Time{Time: time.Date(2023, 3, 2, 0, 0, 0, 0, time.UTC)},
		},
		{
			Name:        "Event 2",
			Description: "Desc 2",
			DateFrom:    types.Time{Time: time.Date(2023, 3, 3, 0, 0, 0, 0, time.UTC)},
			DateTo:      types.Time{Time: time.Date(2023, 3, 4, 0, 0, 0, 0, time.UTC)},
		},
	}
	err := s.db.AddEvents(s.T().Context(), events)
	s.Require().NoError(err)

	// Check both events exist
	for _, e := range events {
		parse, err := query.Parse("", query.WithExpressionCmp("name", &query.ExpressionCmp{
			Operator: query.OperatorEq,
			Field:    "name",
			Value:    e.Name,
		}))
		s.Require().NoError(err)
		result, err := s.db.GetEvents(s.T().Context(), parse)
		s.Require().NoError(err)
		s.Require().Len(result, 1)
		// Cleanup
		_ = s.db.RemoveEvent(s.T().Context(), result[0].ID)
	}
}

func (s *DatabaseSuite) TestRemoveEventNotFound() {
	// Should not error even if event does not exist
	err := s.db.RemoveEvent(s.T().Context(), "non-existent-id")
	s.Require().NoError(err)
}

func (s *DatabaseSuite) TestAddEventWithAllFields() {
	event := models.Event{
		Name:        "Full Event",
		Description: "All fields set",
		EventGroup:  types.NewNull("group1"),
		DateFrom:    types.Time{Time: time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC)},
		DateTo:      types.Time{Time: time.Date(2023, 4, 2, 0, 0, 0, 0, time.UTC)},
		Tz:          "Europe/Paris",
		AllDay:      true,
		RRule:       "RRULE:FREQ=YEARLY;BYMONTH=4;BYMONTHDAY=1",
		Disabled:    true,
		UpdatedBy:   "tester",
	}
	err := s.db.AddEvents(s.T().Context(), []models.Event{event})
	s.Require().NoError(err)

	// Fetch and check
	parse, err := query.Parse("", query.WithExpressionCmp("name", &query.ExpressionCmp{
		Operator: query.OperatorEq,
		Field:    "name",
		Value:    event.Name,
	}))
	s.Require().NoError(err)
	result, err := s.db.GetEvents(s.T().Context(), parse)
	s.Require().NoError(err)
	s.Require().Len(result, 1)
	got := result[0]
	s.Require().Equal(event.Name, got.Name)
	s.Require().Equal(event.Description, got.Description)
	s.Require().Equal(event.EventGroup, got.EventGroup)
	s.Require().Equal(event.Tz, got.Tz)
	s.Require().Equal(event.AllDay, got.AllDay)
	s.Require().Equal(event.RRule, got.RRule)
	s.Require().Equal(event.Disabled, got.Disabled)
	s.Require().Equal(event.UpdatedBy, got.UpdatedBy)

	// Cleanup
	_ = s.db.RemoveEvent(s.T().Context(), got.ID)
}
