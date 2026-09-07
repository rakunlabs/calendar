package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/internal/config"
	"github.com/rakunlabs/calendar/internal/core/domain"
	"github.com/rakunlabs/query"
	"github.com/worldline-go/types"
)

type fakeService struct {
	events    []domain.Event
	relations []domain.Relation
	err       error
	countErr  error
	panicGet  bool
	called    string
	id        string
	user      string
	q         *query.Query
	tz        *time.Location
	group     types.Null[string]
}

func (f *fakeService) AddEvents(_ context.Context, v []domain.Event) error {
	f.called, f.events = "AddEvents", v
	return f.err
}
func (f *fakeService) GetEvents(_ context.Context, q *query.Query) ([]domain.Event, error) {
	f.called, f.q = "GetEvents", q
	return f.events, f.err
}
func (f *fakeService) GetEventsCount(context.Context, *query.Query) (uint64, error) {
	return 7, f.countErr
}
func (f *fakeService) GetEvent(_ context.Context, id string) (*domain.Event, error) {
	if f.panicGet {
		panic("private panic")
	}
	f.called, f.id = "GetEvent", id
	if len(f.events) == 0 {
		return nil, f.err
	}
	return &f.events[0], f.err
}
func (f *fakeService) UpdateEvent(_ context.Context, id string, v *domain.Event) error {
	f.called, f.id, f.events = "UpdateEvent", id, []domain.Event{*v}
	return f.err
}
func (f *fakeService) RemoveEvent(_ context.Context, ids ...string) error {
	f.called, f.id = "RemoveEvent", strings.Join(ids, ",")
	return f.err
}
func (f *fakeService) AddRelations(_ context.Context, v []domain.Relation) error {
	f.called, f.relations = "AddRelations", v
	return f.err
}
func (f *fakeService) GetRelations(_ context.Context, q *query.Query) ([]domain.Relation, error) {
	f.called, f.q = "GetRelations", q
	return f.relations, f.err
}
func (f *fakeService) GetRelationsCount(context.Context, *query.Query) (uint64, error) {
	return 7, f.countErr
}
func (f *fakeService) RemoveRelation(_ context.Context, q *query.Query) error {
	f.called, f.q = "RemoveRelation", q
	return f.err
}
func (f *fakeService) AddIcal(_ context.Context, _ io.Reader, tz *time.Location, group types.Null[string], user string) error {
	f.called, f.tz, f.group, f.user = "AddIcal", tz, group, user
	return f.err
}
func (f *fakeService) GetEventsICS(_ context.Context, q *query.Query) ([]domain.Event, error) {
	f.called, f.q = "GetEventsICS", q
	return f.events, f.err
}

func request(t *testing.T, f *fakeService, method, path, body, user string) *httptest.ResponseRecorder {
	t.Helper()
	s, err := NewServer(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-User", user)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func assertJSON(t *testing.T, w *httptest.ResponseRecorder, code int, want string) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, code, w.Body)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(w.Body.Bytes(), &gotValue); err != nil {
		t.Fatalf("invalid response JSON: %v; body = %s", err, w.Body)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("body = %s, want %s", w.Body, want)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("middleware panic", func(t *testing.T) {
		s, err := NewServer(context.Background(), &fakeService{})
		if err != nil {
			t.Fatal(err)
		}
		s.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "1")
				panic("private middleware error")
			})
		})
		s.GET("/middleware-panic", func(c *ada.Context) error { return nil })
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/middleware-panic", nil))
		assertJSON(t, w, 500, `{"message":{"text":"Internal Server Error"}}`)
		if w.Header().Get("Content-Length") != "" {
			t.Fatal("stale content length retained")
		}
	})

	t.Run("HTTP error panic is redacted", func(t *testing.T) {
		s, err := NewServer(context.Background(), &fakeService{})
		if err != nil {
			t.Fatal(err)
		}
		s.GET("/panic", func(c *ada.Context) error {
			panic(ada.WrapHTTPError(http.StatusBadRequest, errors.New("private panic")))
		})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
		assertJSON(t, w, 500, `{"message":{"text":"Internal Server Error"}}`)
	})

	t.Run("partial response is not overwritten", func(t *testing.T) {
		s, err := NewServer(context.Background(), &fakeService{})
		if err != nil {
			t.Fatal(err)
		}
		s.GET("/partial", func(c *ada.Context) error {
			_ = c.SendString("partial")
			panic("private panic")
		})
		w := httptest.NewRecorder()
		defer func() {
			if got := recover(); got != http.ErrAbortHandler {
				t.Errorf("panic = %v, want http.ErrAbortHandler", got)
			}
			if w.Body.String() != "partial" {
				t.Errorf("response changed: %s", w.Body)
			}
		}()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/partial", nil))
	})
}

func TestQueryEncodingAndPagination(t *testing.T) {
	f := &fakeService{events: []domain.Event{{ID: "id"}}}
	w := request(t, f, http.MethodGet, "/calendar/v1/events?name=R%26D%7Cteam&limit=10&offset=5&sort=-name", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body)
	}
	if f.q == nil || f.q.GetLimit() != 10 || f.q.GetOffset() != 5 ||
		!reflect.DeepEqual(f.q.GetValues("name"), []string{"R&D|team"}) ||
		!reflect.DeepEqual(f.q.Sort, []query.ExpressionSort{{Field: "name", Desc: true}}) {
		t.Fatalf("parsed query = %#v", f.q)
	}
}

func TestErrorEnvelopes(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body string
		service                  fakeService
		code                     int
		want                     string
	}{
		{"missing event", "GET", "/events/missing", "", fakeService{}, 404, `{"message":{"text":"event not found"}}`},
		{"empty events", "GET", "/events", "", fakeService{}, 404, `{"message":{"text":"no events found"}}`},
		{"empty relations", "GET", "/relations", "", fakeService{}, 404, `{"message":{"text":"no relations found"}}`},
		{"read error", "GET", "/events/id", "", fakeService{err: errors.New("read failed")}, 500, `{"message":{"text":"Internal Server Error","error":"read failed"}}`},
		{"bare write error", "POST", "/events", `{}`, fakeService{err: errors.New("private failure")}, 500, `{"message":{"text":"Internal Server Error"}}`},
		{"count error", "GET", "/events", "", fakeService{events: []domain.Event{{ID: "id"}}, countErr: errors.New("count failed")}, 500, `{"message":{"text":"events count failed","error":"count failed"}}`},
		{"missing entity", "POST", "/relations", `{}`, fakeService{}, 400, `{"message":{"text":"missing entity"}}`},
		{"malformed JSON", "POST", "/events", `{`, fakeService{}, 400, `{"message":{"text":"Internal Server Error","error":"binding: failed to decode JSON: unexpected EOF"}}`},
		{"empty PUT", "PUT", "/events/id", "", fakeService{}, 400, `{"message":{"text":"Internal Server Error","error":"EOF"}}`},
		{"unknown route", "GET", "/unknown", "", fakeService{}, 404, `{"message":{"text":"Not Found"}}`},
		{"wrong method", "PATCH", "/events/id", "", fakeService{}, 405, `{"message":{"text":"Method Not Allowed"}}`},
		{"panic", "GET", "/events/id", "", fakeService{panicGet: true}, 500, `{"message":{"text":"Internal Server Error"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := request(t, &tt.service, tt.method, "/calendar/v1"+tt.path, tt.body, "")
			assertJSON(t, w, tt.code, tt.want)
		})
	}
}

func TestBindingAndIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body, user string
		count                          int
		want                           string
	}{
		{"event object", "POST", "/events", `{"id":"a","updated_by":"spoof"}`, "alice", 1, `{"message":{"text":"Events added"},"payload":["a"]}`},
		{"event list", "POST", "/events", `[{"id":"a"},{"id":"b"}]`, "alice", 2, `{"message":{"text":"Events added"},"payload":["a","b"]}`},
		{"event empty", "POST", "/events", `[]`, "alice", 0, `{"message":{"text":"Events added"},"payload":[]}`},
		{"event null", "POST", "/events", `null`, "alice", 0, `{"message":{"text":"Events added"},"payload":[]}`},
		{"absent user", "POST", "/events", `{"updated_by":"spoof"}`, "", 1, `{"message":{"text":"Events added"},"payload":[""]}`},
		{"put trailing JSON", "PUT", "/events/path-id", `{"id":"body-id","updated_by":"spoof"} {}`, "alice", 1, `{"message":{"text":"Event updated"}}`},
		{"put null", "PUT", "/events/path-id", `null`, "alice", 1, `{"message":{"text":"Event updated"}}`},
		{"relation object", "POST", "/relations", `{"entity":"a","event_group":"g","updated_by":"spoof"}`, "alice", 1, `{"message":{"text":"Relations added"}}`},
		{"relation list", "POST", "/relations", `[{"entity":"a","event_group":"g"},{"entity":"b","event_id":"e"}]`, "alice", 2, `{"message":{"text":"Relations added"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeService{}
			w := request(t, f, tt.method, "/calendar/v1"+tt.path, tt.body, tt.user)
			assertJSON(t, w, 200, tt.want)
			if strings.Contains(tt.path, "relations") {
				if len(f.relations) != tt.count {
					t.Fatalf("relations = %#v", f.relations)
				}
				for _, v := range f.relations {
					if v.UpdatedBy != tt.user {
						t.Fatalf("identity = %q", v.UpdatedBy)
					}
				}
			} else {
				if len(f.events) != tt.count {
					t.Fatalf("events = %#v", f.events)
				}
				for _, v := range f.events {
					if v.UpdatedBy != tt.user {
						t.Fatalf("identity = %q", v.UpdatedBy)
					}
				}
			}
			if tt.method == "PUT" && f.id != "path-id" {
				t.Fatalf("update path ID = %q", f.id)
			}
		})
	}
}

func TestListBindingContentType(t *testing.T) {
	for _, path := range []string{"/calendar/v1/events", "/calendar/v1/relations"} {
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/json; charset=utf-8"} {
			t.Run(path+"/"+contentType, func(t *testing.T) {
				f := &fakeService{}
				s, err := NewServer(context.Background(), f)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`[{"entity":"a","event_group":"g"}]`))
				if contentType != "" {
					r.Header.Set("Content-Type", contentType)
				}
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if strings.HasPrefix(contentType, "application/json") {
					if w.Code != http.StatusOK || f.called == "" {
						t.Fatalf("valid JSON: %d, service=%s, body=%s", w.Code, f.called, w.Body)
					}
				} else if w.Code != http.StatusBadRequest || f.called != "" {
					t.Fatalf("invalid content type: %d, service=%s, body=%s", w.Code, f.called, w.Body)
				}
			})
		}
	}
}

func TestQueryValidation(t *testing.T) {
	for _, tt := range []struct{ method, path, body string }{
		{"POST", "/relations", "[]"},
		{"POST", "/relations", "null"},
		{"POST", "/relations", `{"entity":"a"}`},
		{"GET", "/events?unknown=value", ""},
		{"GET", "/events?_fields=id", ""},
		{"DELETE", "/events", ""},
		{"DELETE", "/events?id=", ""},
		{"DELETE", "/events?id[ne]=a", ""},
		{"DELETE", "/events?id=a&_limit=1", ""},
		{"DELETE", "/relations", ""},
		{"GET", "/relations?entity[like]=a", ""},
		{"GET", "/holidays", ""},
		{"GET", "/ics?unknown=value", ""},
		{"POST", "/events", `{} {}`},
		{"POST", "/relations", `[] {}`},
	} {
		t.Run(tt.method+tt.path+tt.body, func(t *testing.T) {
			f := &fakeService{}
			w := request(t, f, tt.method, "/calendar/v1"+tt.path, tt.body, "")
			if w.Code != 400 || f.called != "" {
				t.Fatalf("status = %d, called = %s, body = %s", w.Code, f.called, w.Body)
			}
			var body struct{ Message struct{ Text string } }
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Message.Text == "" {
				t.Fatalf("invalid error envelope: %s", w.Body)
			}
		})
	}
}

func TestReadAndDeleteRoutes(t *testing.T) {
	for _, tt := range []struct{ method, path, called string }{
		{"GET", "/events", "GetEvents"},
		{"GET", "/events/path-id", "GetEvent"},
		{"GET", "/relations", "GetRelations"},
		{"GET", "/holidays?date=2026-09-06", "GetEvents"},
		{"DELETE", "/events/path-id", "RemoveEvent"},
		{"DELETE", "/events?id=a,b", "RemoveEvent"},
		{"DELETE", "/relations?entity=a", "RemoveRelation"},
	} {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			f := &fakeService{events: []domain.Event{{ID: "id"}}, relations: []domain.Relation{{Entity: "entity"}}}
			w := request(t, f, tt.method, "/calendar/v1"+tt.path, "", "")
			if w.Code != 200 || f.called != tt.called {
				t.Fatalf("status = %d, called = %s, body = %s", w.Code, f.called, w.Body)
			}
			if tt.method == "DELETE" {
				if tt.called == "RemoveEvent" && w.Body.Len() != 0 {
					t.Fatalf("delete body = %s", w.Body)
				}
				if tt.path == "/events?id=a,b" && f.id != "a,b" {
					t.Fatalf("delete IDs = %q", f.id)
				}
				if tt.called == "RemoveRelation" {
					assertJSON(t, w, 200, `{"message":{"text":"Relation removed"}}`)
				}
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if _, ok := body["message"]; ok {
				t.Fatalf("unexpected message: %s", w.Body)
			}
			if tt.called == "GetEvent" {
				if f.id != "path-id" || body["payload"][0] != '{' {
					t.Fatalf("object payload/path ID: %s / %q", w.Body, f.id)
				}
			} else {
				if body["payload"][0] != '[' {
					t.Fatalf("list payload: %s", w.Body)
				}
				if tt.path == "/events" || tt.path == "/relations" {
					if string(body["meta"]) != `{"total_item_count":7,"limit":25}` {
						t.Fatalf("pagination: %s", body["meta"])
					}
				}
			}
		})
	}
}

func TestICSAndSwagger(t *testing.T) {
	f := &fakeService{}
	s, err := NewServer(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	form := multipart.NewWriter(&data)
	file, err := form.CreateFormFile("file", "events.ics")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	_ = form.Close()
	r := httptest.NewRequest("POST", "/calendar/v1/ics?event_group=NL&tz=Europe/Amsterdam", &data)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-User", "alice")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	assertJSON(t, w, 200, `{"message":{"text":"ICS added"}}`)
	if f.called != "AddIcal" || f.user != "alice" || f.tz.String() != "Europe/Amsterdam" || f.group != types.NewNull("NL") {
		t.Fatalf("ICS arguments: %#v", f)
	}

	w = request(t, f, "GET", "/calendar/v1/ics?entity=Team,Other", "", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/calendar" ||
		w.Header().Get("Content-Disposition") != "attachment; filename=team_other.ics" || !strings.Contains(w.Body.String(), "BEGIN:VCALENDAR") {
		t.Fatalf("ICS download: %d %v %s", w.Code, w.Header(), w.Body)
	}

	for _, path := range []string{"/calendar/swagger/index.html", "/calendar/swagger/doc.json", "/calendar/swagger/swagger-ui.css"} {
		w := request(t, f, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatalf("swagger %s: %d %s", path, w.Code, w.Body)
		}
		if strings.HasSuffix(path, "doc.json") {
			var doc struct {
				BasePath string
				Info     struct{ Title, Version string }
				Paths    map[string]any
			}
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if doc.BasePath != "/calendar/v1" || doc.Info.Title != config.ServiceName || doc.Info.Version != config.ServiceVersion || doc.Paths["/events/{id}"] == nil {
				t.Fatalf("swagger metadata: %+v", doc)
			}
		}
	}
}

func TestMiddlewareAndHead(t *testing.T) {
	f := &fakeService{}
	s, err := NewServer(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/calendar/v1/events", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("X-Request-Id", "test-request")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Header().Get("Server") != config.ServiceName || w.Header().Get("X-Request-Id") != "test-request" || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("middleware headers: %v", w.Header())
	}
	r = httptest.NewRequest(http.MethodOptions, "/calendar/v1/events", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	r.Header.Set("Access-Control-Request-Headers", "content-type,x-user,authorization")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || !strings.Contains(strings.ToLower(w.Header().Get("Access-Control-Allow-Headers")), "x-user") {
		t.Fatalf("CORS preflight: %d %v", w.Code, w.Header())
	}
	w = request(t, f, http.MethodHead, "/calendar/v1/unknown", "", "")
	if w.Code != 404 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %s", w.Code, w.Body)
	}
}
