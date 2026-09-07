package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/rakunlabs/query"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

type relationService struct {
	port.CalendarService
	q         *query.Query
	relations []models.Relation
	events    []models.Event
	called    bool
}

func (s *relationService) AddRelations(_ context.Context, v []models.Relation) error {
	s.called, s.relations = true, v
	return nil
}

func (s *relationService) RemoveRelation(_ context.Context, q *query.Query) error {
	s.called, s.q = true, q
	return nil
}

func (s *relationService) GetEvents(_ context.Context, q *query.Query) ([]models.Event, error) {
	s.called, s.q = true, q
	return s.events, nil
}

func (s *relationService) GetEventsICS(_ context.Context, q *query.Query) ([]models.Event, error) {
	s.called, s.q = true, q
	return s.events, nil
}

func (s *relationService) GetRelations(_ context.Context, q *query.Query) ([]models.Relation, error) {
	s.called, s.q = true, q
	return s.relations, nil
}

func (s *relationService) GetRelationsCount(context.Context, *query.Query) (uint64, error) {
	return uint64(len(s.relations)), nil
}

func (s *relationService) AddIcal(_ context.Context, r io.Reader, _ *time.Location, _ types.Null[string], _ string) error {
	s.called = true
	_, err := io.Copy(io.Discard, r)
	return err
}

func relationRequest(t *testing.T, s *relationService, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := NewHTTP(s)
	require.NoError(t, err)
	mux := ada.NewMux()
	h.RegisterRoutes(mux)
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User", "tester")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestAddRelationsValidation(t *testing.T) {
	for _, body := range []string{
		`[]`, `null`, `{}`, `{"entity":" "}`, `{"entity":"x"}`,
		`{"entity":"x","event_group":""}`, `{"entity":"x","event_id":" "}`,
		`{"entity":"x","event_group":"g","event_id":""}`,
		`[{"entity":"x","event_group":"g"},{"entity":"x"}]`,
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodPost, "/relations", body)
		require.Equal(t, http.StatusBadRequest, w.Code, body, w.Body.String())
		require.False(t, s.called)
	}
	for _, body := range []string{
		`{"entity":"x","event_group":"g"}`,
		`{"entity":"x","event_id":"e"}`,
		`{"entity":"x","event_group":"g","event_id":"e"}`,
		`[{"entity":"x","event_group":"g"}]`,
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodPost, "/relations", body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, s.relations, 1)
		require.Equal(t, "tester", s.relations[0].UpdatedBy)
	}
}

func TestDeleteExactRelations(t *testing.T) {
	value := "R&D, (west)|entity=other +% / O'Brien"
	for _, targets := range []url.Values{
		{"event_group": {value}}, {"event_id": {value}},
		{"event_group": {value}, "event_id": {value}},
	} {
		targets.Set("_exact", "true")
		targets.Set("entity", value)
		s := &relationService{}
		w := relationRequest(t, s, http.MethodDelete, "/relations?"+targets.Encode(), "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, s.q.Where, 3)
		for _, expression := range s.q.Where {
			cmp := expression.(*query.ExpressionCmp)
			if targets.Has(cmp.Field) {
				require.Equal(t, query.OperatorEq, cmp.Operator)
				require.Equal(t, value, cmp.Value)
			} else {
				require.Equal(t, query.OperatorIs, cmp.Operator)
				require.Nil(t, cmp.Value)
			}
		}
	}
	for _, raw := range []string{
		"_exact=false&entity=x&event_id=e", "_exact=true&entity=x", "_exact=true&event_id=e",
		"_exact=true&entity=x&event_id=", "_exact=true&entity=x&event_id=e&event_id=f",
		"_exact=true&entity=x&entity[eq]=y&event_id=e", "_exact=true&entity=x&event_id[in]=e,f",
		"_exact=true&entity=x&event_id=e&_limit=1", "_exact=true&entity=x&event_id=e&unknown=x",
		"_exact=true&_exact=true&entity=x&event_id=e", "entity=%zz",
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodDelete, "/relations?"+raw, "")
		require.Equal(t, http.StatusBadRequest, w.Code, raw, w.Body.String())
		require.False(t, s.called)
	}
	// Entity-only bulk removal is a shipped external API.
	s := &relationService{}
	w := relationRequest(t, s, http.MethodDelete, "/relations?entity=x", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, s.q.Where, 1)
}

func TestRelationPagination(t *testing.T) {
	s := &relationService{relations: []models.Relation{{Entity: "x", EventGroup: types.NewNull("g")}}}
	w := relationRequest(t, s, http.MethodGet, "/relations?_limit=200&_offset=200&_sort=entity,event_group,event_id", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, uint64(200), s.q.GetLimit())
	require.Equal(t, uint64(200), s.q.GetOffset())
	require.Len(t, s.q.Sort, 3)
	s.relations = nil
	w = relationRequest(t, s, http.MethodGet, "/relations?_limit=200&_offset=400", "")
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestOccurrencesLiteralFilters(t *testing.T) {
	base := "/occurrences?from=2026-01-01T00:00:00Z&to=2026-01-03T00:00:00Z"
	for _, value := range []string{"a,b", "R&D + 50% / O'Brien", "(west)|entity=other", "unbalanced (", "unbalanced )"} {
		s := &relationService{}
		for range 30 {
			s.events = append(s.events, models.Event{
				DateFrom: types.Time{Time: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)},
				DateTo:   types.Time{Time: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)},
			})
		}
		w := relationRequest(t, s, http.MethodGet, base+"&"+url.Values{"entity": {value}, "event_group[eq]": {value}}.Encode(), "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Nil(t, s.q.Limit)
		require.Nil(t, s.q.Offset)
		require.Len(t, s.q.Where, 2)
		for _, field := range []string{"entity", "event_group"} {
			require.Equal(t, []string{value}, s.q.GetValues(field))
			require.Equal(t, query.OperatorEq, s.q.Values[field][0].Operator)
		}
		var response Response[[]models.Event]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Len(t, response.Payload, 30)
	}
	for _, raw := range []string{
		"entity=", "entity=x&entity=y", "entity=x&entity[eq]=y", "entity[in]=a,b",
		"entity[like]=x", "unknown=x", "_limit=1", "limit=1", "_offset=1", "_sort=entity",
		"entity=%zz", "from=2026-01-01T00:00:00Z",
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodGet, base+"&"+raw, "")
		require.Equal(t, http.StatusBadRequest, w.Code, raw, w.Body.String())
		require.False(t, s.called)
	}
}

func TestICSUploadLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		status int
	}{
		{"small", 100, http.StatusOK},
		{"oversized", 10 << 20, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			file, err := writer.CreateFormFile("file", "calendar.ics")
			require.NoError(t, err)
			_, err = io.Copy(file, strings.NewReader(strings.Repeat("x", tc.size)))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			s := &relationService{}
			h, err := NewHTTP(s)
			require.NoError(t, err)
			mux := ada.NewMux()
			h.RegisterRoutes(mux)
			r := httptest.NewRequest(http.MethodPost, "/ics", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.status == http.StatusOK, s.called)
		})
	}
}

func TestICSExplicitEqualityScopes(t *testing.T) {
	for _, entity := range []string{"Team (west", "Team west)", "Team (west)", "Team,west|entity=other"} {
		for _, group := range []string{"Work", "Work (R&D, +50% / O'Brien|year=1999", "Work)"} {
			// Exercise both parameter orders, as URLSearchParams preserves insertion order.
			entityParam := url.Values{"entity[eq]": {entity}}.Encode()
			groupParam := url.Values{"event_group[eq]": {group}}.Encode()
			for _, raw := range []string{
				entityParam + "&" + groupParam + "&year=2025,2026",
				groupParam + "&" + entityParam + "&year=2025,2026",
			} {
				s := &relationService{}
				w := relationRequest(t, s, http.MethodGet, "/ics?"+raw, "")
				require.Equal(t, http.StatusOK, w.Code, raw, w.Body.String())
				require.True(t, s.called)
				require.Len(t, s.q.Where, 2)
				require.ElementsMatch(t, []query.Expression{
					query.NewExpressionCmp(query.OperatorEq, "entity", entity),
					query.NewExpressionCmp(query.OperatorEq, "event_group", group),
				}, s.q.Where)
				require.Equal(t, []string{entity}, s.q.GetValues("entity"))
				require.Equal(t, []string{group}, s.q.GetValues("event_group"))
				require.Equal(t, []string{"2025", "2026"}, s.q.GetValues("year"))
				require.Equal(t, query.OperatorIn, s.q.Values["year"][0].Operator)
			}
		}
	}
	for _, raw := range []string{
		"entity[eq]=", "event_group[eq]=", "entity[eq]=x&entity[eq]=y",
		"event_group[eq]=x&event_group[eq]=y", "entity[eq]=%zz", "entity[eq]=x&unknown=y",
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodGet, "/ics?"+raw, "")
		require.Equal(t, http.StatusBadRequest, w.Code, raw, w.Body.String())
		require.False(t, s.called)
	}
}

func TestICSLegacyQuerySyntax(t *testing.T) {
	h, err := NewHTTP(nil)
	require.NoError(t, err)
	for _, raw := range []string{
		"entity=one,two&event_group=Work,Home&year=2025,2026",
		"entity[in]=one,two&event_group[in]=Work,Home&year=2025&year=2026",
		"(entity=one&event_group[eq]=Work)|entity=two&year=2025,2026",
		"entity=one|entity=two&year[in]=2025,2026",
		"entity=one,two&event_group[eq]=Work&year=2025,2026",
		"entity[eq]=one&entity=one,two&year=2025,2026",
		"entity[eq]=one",
	} {
		s := &relationService{}
		w := relationRequest(t, s, http.MethodGet, "/ics?"+raw, "")
		require.Equal(t, http.StatusOK, w.Code, raw, w.Body.String())
		want, err := parseQuery(raw, h.Validator.GetICS, query.WithSkipExpressionCmp("year"))
		require.NoError(t, err)
		require.ElementsMatch(t, want.Where, s.q.Where, raw)
		for field, expressions := range want.Values {
			require.ElementsMatch(t, expressions, s.q.Values[field], raw)
		}
	}
}
