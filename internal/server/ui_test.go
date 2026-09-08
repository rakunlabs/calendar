package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/internal/core/domain"
	"github.com/worldline-go/types"
)

func TestEmbeddedUI(t *testing.T) {
	s, err := NewServer(context.Background(), &fakeService{}, "")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/calendar/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<div id="app">`) || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("UI response: %d %v %s", w.Code, w.Header(), w.Body)
	}
	asset := regexp.MustCompile(`src="(\./assets/[^" ]+\.js)"`).FindStringSubmatch(w.Body.String())
	if len(asset) != 2 {
		t.Fatal("built JS asset not found in embedded HTML")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/calendar/"+strings.TrimPrefix(asset[1], "./"), nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	for _, path := range []string{"/calendar/v1/not-an-api", "/calendar/assets/missing.js"} {
		w = httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), `<div id="app">`) {
			t.Fatalf("missing path %s: %d %s", path, w.Code, w.Body)
		}
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/calendar/settings", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<div id="app">`) {
		t.Fatalf("SPA fallback: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/calendar/", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("UI HEAD: %d %s", w.Code, w.Body)
	}
}

func TestBasePath(t *testing.T) {
	for _, tt := range []struct{ name, input, prefix string }{
		{"default", "", "/calendar"},
		{"bare", "calendar", "/calendar"},
		{"leading slash", "/calendar", "/calendar"},
		{"trailing slash", "calendar/", "/calendar"},
		{"both slashes", "/calendar/", "/calendar"},
		{"whitespace only", " \t\n", "/calendar"},
		{"surrounding whitespace", " \t/calendar/\n", "/calendar"},
		{"nested", "tools/team/calendar/", "/tools/team/calendar"},
		{"root", "/", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeService{events: []domain.Event{{ID: "id"}}}
			s, err := NewServer(context.Background(), f, tt.input)
			if err != nil {
				t.Fatal(err)
			}
			get := func(path string) *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				return w
			}
			w := get(tt.prefix + "/")
			if w.Code != http.StatusOK || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `<div id="app">`) {
				t.Fatalf("UI: %d %v %s", w.Code, w.Header(), w.Body)
			}
			assets := regexp.MustCompile(`(?:src|href)="(\./assets/[^" ]+\.(?:js|css))"`).FindAllStringSubmatch(w.Body.String(), -1)
			if len(assets) == 0 {
				t.Fatal("built assets not found in embedded HTML")
			}
			for _, asset := range assets {
				w = get(tt.prefix + "/" + strings.TrimPrefix(asset[1], "./"))
				if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
					t.Fatalf("asset %s: %d %v", asset[1], w.Code, w.Header())
				}
			}
			if tt.prefix != "" {
				w = get("/")
				if w.Code != http.StatusNotFound {
					t.Fatalf("outside mount: %d", w.Code)
				}
				w = get(tt.prefix)
				if w.Code != http.StatusMovedPermanently {
					t.Fatalf("folder redirect: %d %v", w.Code, w.Header())
				}
			}
			w = get(tt.prefix + "/v1/events/id")
			if w.Code != http.StatusOK || f.called != "GetEvent" || f.id != "id" {
				t.Fatalf("API: %d called=%s id=%s body=%s", w.Code, f.called, f.id, w.Body)
			}
			w = get(tt.prefix + "/swagger/doc.json")
			var doc struct{ BasePath string }
			if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || doc.BasePath != tt.prefix+"/v1" {
				t.Fatalf("swagger: %d basePath=%q", w.Code, doc.BasePath)
			}
		})
	}
}

func TestOccurrencesAPI(t *testing.T) {
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	f := &fakeService{events: []domain.Event{{ID: "series", DateFrom: types.Time{Time: start}, DateTo: types.Time{Time: start.Add(time.Hour)}, RRule: "RRULE:FREQ=DAILY;COUNT=2"}}}
	w := request(t, f, "GET", "/calendar/v1/occurrences?from=2026-01-01T00:00:00Z&to=2026-01-03T00:00:00Z", "", "")
	if w.Code != 200 || strings.Count(w.Body.String(), `"id":"series"`) != 2 {
		t.Fatalf("occurrences: %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/occurrences", "/occurrences?from=invalid&to=invalid", "/occurrences?from=2026-01-01T00:00:00Z&to=2025-01-01T00:00:00Z", "/occurrences?from=2026-01-01T00:00:00Z&to=2028-01-01T00:00:00Z"} {
		w = request(t, &fakeService{}, "GET", "/calendar/v1"+path, "", "")
		if w.Code != 400 {
			t.Fatalf("invalid range: %d %s", w.Code, w.Body)
		}
	}
}
