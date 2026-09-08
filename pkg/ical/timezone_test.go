package ical

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
)

func TestTimezoneCacheReusesExplicitDates(t *testing.T) {
	definitions := []string{testCustomTimezone, testCustomTimezone}
	resolved, err := resolveTimezoneDefinitions(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("got %d compiled definitions, want 1", len(resolved))
	}
	location := resolved[0].location
	date := models.CalendarDate{TZID: "America/New_York", Value: "20500701T120000"}
	for i := 0; i < 1000; i++ {
		got, err := ResolveCalendarDate(date, nil, definitions)
		if err != nil {
			t.Fatal(err)
		}
		if got.Location() != location {
			t.Fatalf("explicit date %d recompiled its timezone", i)
		}
	}
	if err := ValidateTimezoneDefinitions(definitions); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateVTimezones([]models.Event{{Tz: date.TZID, Recurrence: &models.Recurrence{Timezones: definitions}}}); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLocation(date.TZID, definitions)
	if err != nil || got != location {
		t.Fatalf("validation/export did not reuse compilation: %v", err)
	}
}

func TestTimezoneCacheConcurrentMiss(t *testing.T) {
	var cache timezoneCache
	results := make(chan *time.Location, 32)
	for i := 0; i < cap(results); i++ {
		go func() {
			loc, err := cache.load(testCustomTimezone)
			if err != nil {
				t.Error(err)
			}
			results <- loc
		}()
	}
	first := <-results
	if first == nil {
		t.Fatal("nil compiled location")
	}
	for i := 1; i < cap(results); i++ {
		if got := <-results; got != first {
			t.Fatal("concurrent cache miss compiled a second location")
		}
	}
}

func TestTimezoneCacheBoundedLRU(t *testing.T) {
	var cache timezoneCache
	raw := func(i int) string {
		return fmt.Sprintf("BEGIN:VTIMEZONE\nTZID:Cache/%d\nBEGIN:STANDARD\nDTSTART:19700101T000000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0100\nEND:STANDARD\nEND:VTIMEZONE\n", i)
	}
	locations := make([]*time.Location, timezoneCacheLimit)
	for i := range locations {
		var err error
		locations[i], err = cache.load(raw(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cache.load(raw(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.load(raw(timezoneCacheLimit)); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != timezoneCacheLimit {
		t.Fatalf("cache contains %d entries", len(cache.entries))
	}
	if got, err := cache.load(raw(0)); err != nil || got != locations[0] {
		t.Fatal("recently used entry was evicted")
	}
	if got, err := cache.load(raw(1)); err != nil || got == locations[1] {
		t.Fatal("least recently used entry was not evicted")
	}
	if len(cache.entries) != timezoneCacheLimit {
		t.Fatal("cache exceeded capacity after reloading evicted entry")
	}
	if _, err := cache.load("invalid"); err == nil {
		t.Fatal("invalid definition was cached")
	}
	if len(cache.entries) != timezoneCacheLimit {
		t.Fatal("invalid definition changed cache size")
	}
}

func TestValidateTimezoneDefinitions(t *testing.T) {
	if err := ValidateTimezoneDefinitions(nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTimezoneDefinitions([]string{testCustomTimezone, testCustomTimezone}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"bad",
		strings.Replace(testCustomTimezone, "BEGIN:STANDARD", "BEGIN:STANDARD\r\nTZNAME:"+strings.Repeat("x", 257), 1),
		strings.ReplaceAll(testCustomTimezone, "+0100", "+0030"),
	} {
		definitions := []string{testCustomTimezone, invalid}
		if err := ValidateTimezoneDefinitions(definitions); err == nil {
			t.Fatal("accepted invalid or conflicting unreferenced definition")
		}
		if _, err := ResolveLocation("America/New_York", definitions); err == nil {
			t.Fatal("cached match skipped validation of remaining definitions")
		}
	}
}

func BenchmarkResolveCalendarDateCustom1000(b *testing.B) {
	definitions := []string{testCustomTimezone, testCustomTimezone}
	if err := ValidateTimezoneDefinitions(definitions); err != nil {
		b.Fatal(err)
	}
	date := models.CalendarDate{TZID: "America/New_York", Value: "20500701T120000"}
	b.ReportAllocs()
	for b.Loop() {
		for i := 0; i < 1000; i++ {
			if _, err := ResolveCalendarDate(date, nil, definitions); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestGenerateVTimezonesRejectsScopeCollision(t *testing.T) {
	custom := models.Event{Tz: "America/New_York", Recurrence: &models.Recurrence{Timezones: []string{testCustomTimezone}}}
	for _, plain := range []models.Event{
		{Tz: "America/New_York"},
		{Recurrence: &models.Recurrence{Start: &models.CalendarDate{TZID: "America/New_York"}}},
		{Recurrence: &models.Recurrence{End: &models.CalendarDate{TZID: "America/New_York"}}},
		{Recurrence: &models.Recurrence{ExDates: []models.CalendarDate{{TZID: "America/New_York"}}}},
		{Recurrence: &models.Recurrence{RDates: []models.RecurrencePeriod{{Start: models.CalendarDate{TZID: "America/New_York"}}}}},
		{Recurrence: &models.Recurrence{Overrides: []models.OccurrenceOverride{{RecurrenceID: models.CalendarDate{TZID: "America/New_York"}}}}},
		{Recurrence: &models.Recurrence{Overrides: []models.OccurrenceOverride{{Event: &models.Event{Tz: "America/New_York"}}}}},
	} {
		for _, events := range [][]models.Event{{custom, plain}, {plain, custom}} {
			if _, err := GenerateVTimezones(events); err == nil || !strings.Contains(err.Error(), "scope collision") {
				t.Fatalf("expected scope collision, got %v", err)
			}
		}
	}
	custom.Tz = "UTC" // Even an unreferenced emitted definition would shadow IANA.
	if _, err := GenerateVTimezones([]models.Event{custom, {Tz: "America/New_York"}}); err == nil {
		t.Fatal("unreferenced custom definition shadowed IANA")
	}
}

func TestGenerateVTimezonesOverrideInheritsScope(t *testing.T) {
	e := models.Event{Tz: "America/New_York", Recurrence: &models.Recurrence{
		Timezones: []string{testCustomTimezone},
		Overrides: []models.OccurrenceOverride{{Event: &models.Event{Tz: "America/New_York"}}},
	}}
	got, err := GenerateVTimezones([]models.Event{e})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != testCustomTimezone {
		t.Fatal("override did not inherit master definition")
	}
	if e.Recurrence.Overrides[0].Event.Recurrence != nil {
		t.Fatal("export mutated override recurrence")
	}
}

func TestGenerateVTimezonesNilRecurrence(t *testing.T) {
	for _, events := range [][]models.Event{nil, {{}}, {{Tz: "Etc/GMT+5"}}} {
		if _, err := GenerateVTimezones(events); err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if e.Recurrence != nil {
				t.Fatal("export mutated nil recurrence")
			}
		}
	}
}

const testCustomTimezone = "BEGIN:VTIMEZONE\r\nTZID:America/New_York\r\n" +
	"BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\n" +
	"BEGIN:DAYLIGHT\r\nDTSTART:20000326T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU\r\nEND:DAYLIGHT\r\n" +
	"BEGIN:STANDARD\r\nDTSTART:20001029T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n"

func TestResolveLocationScopedDefinition(t *testing.T) {
	loc, err := ResolveLocation("America/New_York", []string{testCustomTimezone})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		date   string
		offset int
	}{{"1900-07-01T00:00:00Z", 3600}, {"2050-01-01T00:00:00Z", 3600}, {"2050-07-01T00:00:00Z", 7200}, {"9999-07-01T00:00:00Z", 7200}} {
		instant, _ := time.Parse(time.RFC3339, test.date)
		_, got := instant.In(loc).Zone()
		if got != test.offset {
			t.Errorf("%s: got %d, want %d", test.date, got, test.offset)
		}
	}
	other := strings.ReplaceAll(strings.ReplaceAll(testCustomTimezone, "+0200", "+0400"), "+0100", "+0300")
	loc2, err := ResolveLocation("America/New_York", []string{other})
	if err != nil {
		t.Fatal(err)
	}
	_, got := time.Date(2050, 7, 1, 0, 0, 0, 0, loc2).Zone()
	if got != 14400 {
		t.Fatalf("definition cache leaked: %d", got)
	}
	iana, err := ResolveLocation("America/New_York", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, got = time.Date(2050, 7, 1, 0, 0, 0, 0, iana).Zone()
	if got != -14400 {
		t.Fatalf("custom definition leaked into IANA: %d", got)
	}
}

func TestResolveLocationTransitionBoundaries(t *testing.T) {
	loc, err := ResolveLocation("America/New_York", []string{testCustomTimezone})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		utc, wall string
		dst       bool
	}{
		{"2024-03-31T00:59:59Z", "2024-03-31T01:59:59", false},
		{"2024-03-31T01:00:00Z", "2024-03-31T03:00:00", true},
		{"2024-10-27T00:59:59Z", "2024-10-27T02:59:59", true},
		{"2024-10-27T01:00:00Z", "2024-10-27T02:00:00", false},
	} {
		u, _ := time.Parse(time.RFC3339, test.utc)
		got := u.In(loc)
		if got.Format("2006-01-02T15:04:05") != test.wall || got.IsDST() != test.dst {
			t.Errorf("%s: %s, DST %v", test.utc, got, got.IsDST())
		}
	}
}

func TestTimezoneHistoricalRDateAndUntil(t *testing.T) {
	raw := "BEGIN:VTIMEZONE\nTZID:History\nBEGIN:STANDARD\nDTSTART:19000101T000000\nTZOFFSETFROM:+001932\nTZOFFSETTO:+0100\nEND:STANDARD\n" +
		"BEGIN:DAYLIGHT\nDTSTART:20000326T020000\nTZOFFSETFROM:+0100\nTZOFFSETTO:+0200\nRRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU;UNTIL=20010325T010000Z\nRDATE:20050327T020000\nEND:DAYLIGHT\n" +
		"BEGIN:STANDARD\nDTSTART:20001029T030000\nTZOFFSETFROM:+0200\nTZOFFSETTO:+0100\nRRULE:FREQ=YEARLY;BYMONTH=10;BYDAY=SU;BYSETPOS=-1;COUNT=2\nRDATE:20051030T030000\nEND:STANDARD\nEND:VTIMEZONE\n"
	loc, err := ResolveLocation("History", []string{raw})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ year, offset int }{{1800, 1172}, {1901, 3600}, {2000, 7200}, {2001, 7200}, {2002, 3600}, {2005, 7200}, {2006, 3600}} {
		_, got := time.Date(test.year, 7, 1, 0, 0, 0, 0, time.UTC).In(loc).Zone()
		if got != test.offset {
			t.Errorf("year %d: got %d want %d", test.year, got, test.offset)
		}
	}
}

func TestTimezoneRejectsInvalidDefinitions(t *testing.T) {
	for _, raw := range []string{
		"", "BEGIN:VTIMEZONE\nTZID:Bad\nEND:VTIMEZONE",
		strings.Replace(testCustomTimezone, "TZOFFSETFROM:+0100", "TZOFFSETFROM:+2460", 1),
		strings.Replace(testCustomTimezone, "DTSTART:19700101T000000", "DTSTART:00000101T000000", 1),
		strings.Replace(testCustomTimezone, "DTSTART:19700101T000000", "DTSTART:19700101T000000Z", 1),
		strings.Replace(testCustomTimezone, "FREQ=YEARLY", "FREQ=MONTHLY", 1),
		strings.Replace(testCustomTimezone, "FREQ=YEARLY", "FREQ=YEARLY;BYHOUR=2", 1),
		strings.Replace(testCustomTimezone, "FREQ=YEARLY", "FREQ=YEARLY;UNTIL=20250330T020000", 1),
		strings.Replace(testCustomTimezone, "END:DAYLIGHT", "EXDATE:20250330T020000\r\nEND:DAYLIGHT", 1),
		strings.Replace(testCustomTimezone, "END:VTIMEZONE", "BEGIN:VEVENT\r\nEND:VEVENT\r\nEND:VTIMEZONE", 1),
	} {
		if _, err := ResolveLocation("UTC", []string{raw}); err == nil {
			t.Errorf("accepted invalid definition %q", raw)
		}
	}
	if _, err := ResolveLocation("America/New_York", []string{testCustomTimezone, strings.ReplaceAll(testCustomTimezone, "+0100", "+0300")}); err == nil {
		t.Fatal("accepted conflicting TZIDs")
	}
}

func TestGenerateVTimezonesPreservesRaw(t *testing.T) {
	e := models.Event{Tz: "America/New_York", Recurrence: &models.Recurrence{Timezones: []string{testCustomTimezone}}}
	got, err := GenerateVTimezones([]models.Event{e, e})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != testCustomTimezone {
		t.Fatal("raw definition not preserved or deduplicated")
	}
	e.Recurrence.Timezones = []string{"bad"}
	if _, err := GenerateVTimezones([]models.Event{e}); err == nil {
		t.Fatal("accepted invalid raw definition")
	}
}

func TestGenerateVTimezonesRecurrenceReferences(t *testing.T) {
	e := models.Event{Recurrence: &models.Recurrence{
		Start:     &models.CalendarDate{TZID: "Etc/GMT+1"},
		End:       &models.CalendarDate{TZID: "Etc/GMT+2"},
		ExDates:   []models.CalendarDate{{TZID: "Etc/GMT+3"}},
		RDates:    []models.RecurrencePeriod{{Start: models.CalendarDate{TZID: "Etc/GMT+4"}, End: &models.CalendarDate{TZID: "Etc/GMT+5"}}},
		Overrides: []models.OccurrenceOverride{{RecurrenceID: models.CalendarDate{TZID: "Etc/GMT+6"}, Event: &models.Event{Tz: "Etc/GMT+7"}}},
	}}
	definitions, err := GenerateVTimezones([]models.Event{e})
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 7 {
		t.Fatalf("got %d definitions, want 7", len(definitions))
	}
	for _, raw := range definitions {
		if _, _, err := parseTimezoneDefinition(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimezoneFoldedRule(t *testing.T) {
	raw := strings.ReplaceAll(testCustomTimezone, "BYMONTH=", "\r\n BYMONTH=")
	if _, err := ResolveLocation("America/New_York", []string{raw}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateVTimezonesIANARoundTrip(t *testing.T) {
	for _, name := range []string{"America/New_York", "Europe/Paris", "Australia/Lord_Howe", "Pacific/Apia", "Africa/Casablanca", "Etc/GMT+5"} {
		t.Run(name, func(t *testing.T) {
			original, err := time.LoadLocation(name)
			if err != nil {
				t.Fatal(err)
			}
			definitions, err := GenerateVTimezones([]models.Event{{Tz: name}})
			if err != nil {
				t.Fatal(err)
			}
			if len(definitions) != 1 || len(definitions[0]) > 10*1024*1024 {
				t.Fatalf("unexpected export size")
			}
			restored, err := ResolveLocation(name, definitions)
			if err != nil {
				t.Fatal(err)
			}
			for _, year := range []int{1, 1800, 1900, 1945, 1970, 2000, 2024, 2050, 2100, 2400, 9999} {
				for month := time.January; month <= time.December; month++ {
					instant := time.Date(year, month, 15, 12, 0, 0, 0, time.UTC)
					wantName, want := instant.In(original).Zone()
					gotName, got := instant.In(restored).Zone()
					if want != got || wantName != gotName || instant.In(original).IsDST() != instant.In(restored).IsDST() {
						t.Fatalf("%s: offset %d want %d; DST %v want %v", instant, got, want, instant.In(restored).IsDST(), instant.In(original).IsDST())
					}
				}
			}
			// Check both sides of every real and synthetic future boundary.
			for instant := time.Date(1800, 1, 1, 0, 0, 0, 0, original); instant.Year() < 2053; {
				_, boundary := instant.ZoneBounds()
				if boundary.IsZero() {
					break
				}
				if !boundary.After(instant) {
					instant = instant.Add(time.Hour)
					continue
				}
				for _, delta := range []time.Duration{-time.Second, 0, time.Second} {
					at := boundary.Add(delta)
					_, want := at.Zone()
					_, got := at.In(restored).Zone()
					if got != want {
						t.Fatalf("boundary %s: %d want %d", at, got, want)
					}
				}
				instant = boundary
			}
		})
	}
}
