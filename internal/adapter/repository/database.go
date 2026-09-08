package repository

import (
	"context"
	"errors"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/query"
	"github.com/rakunlabs/query/adapter/adaptergoqu"
	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/models"
)

type QeuryHoliday struct {
	ID       *int64
	Provider *int64
	Country  *string
}

var (
	TableEventsStr    = "calendar_events"
	TableRelationsStr = "calendar_relations"

	TableEvents   exp.IdentifierExpression
	TableRelation exp.IdentifierExpression

	Schema          exp.IdentifierExpression
	TableEventsAs   exp.AliasedExpression
	TableRelationAs exp.AliasedExpression
)

var ErrStopLoop = errors.New("stop loop")

func setSchema(schema string) {
	Schema = goqu.S(schema)
	TableEvents = Schema.Table(TableEventsStr)
	TableRelation = Schema.Table(TableRelationsStr)

	TableEventsAs = TableEvents.As(TableEventsStr)
	TableRelationAs = TableRelation.As(TableRelationsStr)
}

func (db *Database) AddEvents(ctx context.Context, events []models.Event) error {
	if len(events) == 0 {
		return nil
	}
	updatedAt := types.Time{Time: time.Now().UTC().Truncate(time.Microsecond)}
	rows := make([]goqu.Record, len(events))

	for i := range events {
		events[i].RecurrenceID = nil
		events[i].IsOverride = false
		if events[i].ID == "" {
			events[i].ID = ulid.Make().String()
		}
		events[i].UpdatedAt = updatedAt
		row, err := exp.NewRecordFromStruct(events[i], true, false)
		if err != nil {
			return err
		}
		// Mixed batches need the same columns despite recurrence's omitnil tag.
		if events[i].Recurrence == nil {
			row["recurrence"] = nil
		}
		rows[i] = row
	}

	_, err := db.q.Insert(TableEvents).
		Rows(rows).
		OnConflict(goqu.DoNothing()).
		Executor().ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (db *Database) getEventsSelect(q *query.Query) *goqu.SelectDataset {
	selectDataSet := adaptergoqu.Select(q, db.q.From(TableEventsAs),
		adaptergoqu.WithDefaultSelect(TableEventsStr+".*"),
		adaptergoqu.WithRename(map[string]string{
			"entity":      TableRelationsStr + ".entity",
			"event_group": TableEventsStr + ".event_group",
		}),
	).Distinct()

	if q.HasAny("entity") {
		selectDataSet = selectDataSet.InnerJoin(TableRelationAs, goqu.On(
			goqu.Or(
				goqu.Ex{TableRelationsStr + ".event_id": goqu.I(TableEventsStr + ".id")},
				goqu.Ex{TableRelationsStr + ".event_group": goqu.I(TableEventsStr + ".event_group")},
			),
		))
	}

	return selectDataSet
}

func (db *Database) GetEventsCount(ctx context.Context, q *query.Query) (uint64, error) {
	var count uint64
	_, err := db.getEventsSelect(q).
		ClearOrder().ClearLimit().ClearOffset().
		Select(goqu.COUNT(goqu.DISTINCT("id"))).
		Executor().ScanValContext(ctx, &count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (db *Database) GetEvents(ctx context.Context, q *query.Query) ([]models.Event, error) {
	var events []models.Event

	if err := db.getEventsSelect(q).Executor().ScanStructsContext(ctx, &events); err != nil {
		return nil, err
	}

	return events, nil
}

func (db *Database) GetEventsWithFunc(ctx context.Context, q *query.Query, fn func(models.Event) error) error {
	scanner, err := db.getEventsSelect(q).Executor().ScannerContext(ctx)
	if err != nil {
		return err
	}
	defer scanner.Close()

	for scanner.Next() {
		var event models.Event
		if err := scanner.ScanStruct(&event); err != nil {
			return err
		}
		if err := fn(event); err != nil {
			if errors.Is(err, ErrStopLoop) {
				break
			}

			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

func (db *Database) GetEvent(ctx context.Context, id string) (*models.Event, error) {
	var event models.Event

	found, err := db.q.From(TableEvents).
		Where(goqu.Ex{
			"id": id,
		}).
		Executor().ScanStructContext(ctx, &event)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, nil
	}

	return &event, nil
}

func (db *Database) UpdateEvent(ctx context.Context, id string, event *models.Event) error {
	updated := *event
	updated.RecurrenceID = nil
	updated.IsOverride = false
	updated.UpdatedAt = types.Time{Time: time.Now().UTC().Truncate(time.Microsecond)}
	if !updated.UpdatedAt.After(event.UpdatedAt.Time) {
		updated.UpdatedAt = types.Time{Time: event.UpdatedAt.Add(time.Microsecond)}
	}
	where := goqu.Ex{"id": id}
	if event.Recurrence != nil {
		if event.UpdatedAt.IsZero() {
			return port.ErrConflict
		}
		where["updated_at"] = event.UpdatedAt
	} else {
		// A concurrent recurrence-aware edit must not be overwritten by a legacy PUT.
		where["recurrence"] = nil
	}

	result, err := db.q.Update(TableEvents).
		Set(&updated).
		Where(where).
		Executor().ExecContext(ctx)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return port.ErrConflict
	}
	*event = updated

	return nil
}

func (db *Database) RemoveEvent(ctx context.Context, id ...string) error {
	_, err := db.q.Delete(TableEvents).
		Where(goqu.Ex{
			"id": goqu.Op{"in": id},
		}).
		Executor().ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}

// /////////////////////////////////////////////////////////////
// Relation
// /////////////////////////////////////////////////////////////

func (db *Database) AddRelations(ctx context.Context, relations []models.Relation) error {
	updatedAt := types.Time{Time: time.Now()}

	for i := range relations {
		relations[i].UpdatedAt = updatedAt
	}

	_, err := db.q.Insert(TableRelation).
		Rows(relations).
		OnConflict(goqu.DoNothing()).
		Executor().ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (db *Database) RemoveRelation(ctx context.Context, q *query.Query) error {
	_, err := db.q.Delete(TableRelation).
		Where(adaptergoqu.Expression(q)...).
		Executor().ExecContext(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (db *Database) GetRelationsCount(ctx context.Context, q *query.Query) (uint64, error) {
	count, err := adaptergoqu.Select(q, db.q.From(TableRelation)).ClearOrder().ClearLimit().ClearOffset().CountContext(ctx)
	if err != nil {
		return 0, err
	}

	return uint64(count), nil
}

func (db *Database) GetRelations(ctx context.Context, q *query.Query) ([]models.Relation, error) {
	var relations []models.Relation

	if err := adaptergoqu.Select(q, db.q.From(TableRelation)).Executor().ScanStructsContext(ctx, &relations); err != nil {
		return nil, err
	}

	return relations, nil
}
