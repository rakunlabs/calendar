package server

import (
	"context"
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
	s, err := NewServer(context.Background(), &fakeService{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/calendar/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<div id="app">`) || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("UI response: %d %v %s", w.Code, w.Header(), w.Body)
	}
	asset := regexp.MustCompile(`src="(/calendar/assets/[^" ]+\.js)"`).FindStringSubmatch(w.Body.String())
	if len(asset) != 2 {
		t.Fatal("built JS asset not found in embedded HTML")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, asset[1], nil))
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
