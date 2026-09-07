package ical

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWalkRRule(t *testing.T) {
	for _, tt := range []struct {
		name, rule, start, to string
		want                  []string
	}{
		{"weekly defaults", "FREQ=WEEKLY;COUNT=3", "2024-01-30T09:00:00Z", "2024-02-20T09:00:00Z", []string{"2024-01-30T09:00:00Z", "2024-02-06T09:00:00Z", "2024-02-13T09:00:00Z"}},
		{"weekly crosses month", "FREQ=WEEKLY;BYDAY=TU,FR;COUNT=3", "2024-01-30T09:00:00Z", "2024-02-20T09:00:00Z", []string{"2024-01-30T09:00:00Z", "2024-02-02T09:00:00Z", "2024-02-06T09:00:00Z"}},
		{"month end skips invalid dates", "FREQ=MONTHLY;COUNT=3", "2024-01-31T09:00:00Z", "2024-06-01T00:00:00Z", []string{"2024-01-31T09:00:00Z", "2024-03-31T09:00:00Z", "2024-05-31T09:00:00Z"}},
		{"leap day", "FREQ=YEARLY;COUNT=2", "2024-02-29T09:00:00Z", "2029-01-01T00:00:00Z", []string{"2024-02-29T09:00:00Z", "2028-02-29T09:00:00Z"}},
		{"yearly month inherits day", "FREQ=YEARLY;BYMONTH=2,3;COUNT=3", "2024-01-31T09:00:00Z", "2027-01-01T00:00:00Z", []string{"2024-03-31T09:00:00Z", "2025-03-31T09:00:00Z", "2026-03-31T09:00:00Z"}},
		{"monthly month inherits day", "FREQ=MONTHLY;BYMONTH=2,3", "2024-01-31T09:00:00Z", "2024-04-01T00:00:00Z", []string{"2024-03-31T09:00:00Z"}},
		{"negative month day", "FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=3", "2024-01-30T09:00:00Z", "2024-04-01T00:00:00Z", []string{"2024-01-31T09:00:00Z", "2024-02-29T09:00:00Z", "2024-03-31T09:00:00Z"}},
		{"negative year day", "FREQ=YEARLY;BYYEARDAY=-1,-366", "2024-01-01T09:00:00Z", "2025-12-31T09:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-12-31T09:00:00Z", "2025-12-31T09:00:00Z"}},
		{"ordinal year weekday", "FREQ=YEARLY;BYDAY=2MO,-1SU", "2024-01-01T09:00:00Z", "2024-12-31T09:00:00Z", []string{"2024-01-08T09:00:00Z", "2024-12-29T09:00:00Z"}},
		{"ordinal month weekday", "FREQ=YEARLY;BYMONTH=2;BYDAY=2MO,-1SU", "2024-01-01T09:00:00Z", "2024-12-31T09:00:00Z", []string{"2024-02-12T09:00:00Z", "2024-02-25T09:00:00Z"}},
		{"negative week number", "FREQ=YEARLY;BYWEEKNO=-1;BYDAY=MO", "2024-01-01T09:00:00Z", "2025-01-01T00:00:00Z", []string{"2024-12-23T09:00:00Z"}},
		{"week year boundary", "FREQ=YEARLY;BYWEEKNO=1;BYDAY=MO", "2024-01-01T09:00:00Z", "2024-12-31T09:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-12-30T09:00:00Z"}},
		{"week start sunday", "FREQ=YEARLY;BYWEEKNO=1;BYDAY=SU;WKST=SU", "2020-01-01T09:00:00Z", "2021-01-04T09:00:00Z", []string{"2021-01-03T09:00:00Z"}},
		{"weekly interval monday", "FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,SU;WKST=MO;COUNT=4", "1997-08-05T09:00:00Z", "1997-09-01T00:00:00Z", []string{"1997-08-05T09:00:00Z", "1997-08-10T09:00:00Z", "1997-08-19T09:00:00Z", "1997-08-24T09:00:00Z"}},
		{"weekly interval sunday", "FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,SU;WKST=SU;COUNT=4", "1997-08-05T09:00:00Z", "1997-09-01T00:00:00Z", []string{"1997-08-05T09:00:00Z", "1997-08-17T09:00:00Z", "1997-08-19T09:00:00Z", "1997-08-31T09:00:00Z"}},
		{"setpos before start excluded", "FREQ=MONTHLY;BYDAY=MO;BYSETPOS=1;COUNT=2", "2024-01-15T09:00:00Z", "2024-04-01T00:00:00Z", []string{"2024-02-05T09:00:00Z", "2024-03-04T09:00:00Z"}},
		{"setpos ordered unique", "FREQ=MONTHLY;BYDAY=MO;BYSETPOS=-1,1,1;COUNT=3", "2024-01-01T09:00:00Z", "2024-03-01T00:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-01-29T09:00:00Z", "2024-02-05T09:00:00Z"}},
		{"time expansion sorted unique count", "FREQ=DAILY;BYHOUR=10,9,9;BYMINUTE=30,0;BYSECOND=20,10;COUNT=4", "2024-01-01T09:00:15Z", "2024-01-02T00:00:00Z", []string{"2024-01-01T09:00:20Z", "2024-01-01T09:30:10Z", "2024-01-01T09:30:20Z", "2024-01-01T10:00:10Z"}},
		{"setpos includes time", "FREQ=DAILY;BYHOUR=8,17;BYSETPOS=-1", "2024-01-01T09:00:00Z", "2024-01-02T17:00:00Z", []string{"2024-01-01T17:00:00Z", "2024-01-02T17:00:00Z"}},
		{"inclusive until", "FREQ=DAILY;UNTIL=20240102T090000Z", "2024-01-01T09:00:00Z", "2024-01-10T00:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-01-02T09:00:00Z"}},
		{"inclusive upper bound", "FREQ=DAILY", "2024-01-01T09:00:00Z", "2024-01-02T09:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-01-02T09:00:00Z"}},
		{"until inside period", "FREQ=MONTHLY;BYMONTHDAY=1,15;UNTIL=20240110T090000Z", "2024-01-01T09:00:00Z", "2024-03-01T00:00:00Z", []string{"2024-01-01T09:00:00Z"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := ParseRRule(tt.rule)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			err = walkRRule(context.Background(), r, rruleTestTime(t, tt.start), rruleTestTime(t, tt.to), func(v time.Time) bool {
				got = append(got, v.Format(time.RFC3339))
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func rruleTestTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWalkRRuleHistoricHolidays(t *testing.T) {
	for _, definition := range []string{
		"FREQ=YEARLY;BYMONTH=6;BYDAY=3SU",
		"FREQ=YEARLY;BYMONTH=11;BYMONTHDAY=2,3,4,5,6,7,8;BYDAY=TU",
		"FREQ=YEARLY;BYDAY=1MO",
		"FREQ=YEARLY;BYYEARDAY=-1",
	} {
		t.Run(definition, func(t *testing.T) {
			r, err := ParseRRule(definition)
			if err != nil {
				t.Fatal(err)
			}
			for _, year := range []int{1900, 1800} {
				start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
				to := time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC)
				count := 0
				var last time.Time
				err := walkRRule(context.Background(), r, start, to, func(v time.Time) bool {
					if v.Year() != year+count {
						t.Fatalf("expected one holiday in %d, got %v", year+count, v)
					}
					last = v
					count++
					return true
				})
				if err != nil || count != 2028-year {
					t.Fatalf("start=%d count=%d error=%v", year, count, err)
				}
				if definition == "FREQ=YEARLY;BYMONTH=6;BYDAY=3SU" && !last.Equal(time.Date(2027, 6, 20, 0, 0, 0, 0, time.UTC)) {
					t.Fatalf("wrong final Father's Day: %v", last)
				}
				if a, _, ok := MatchRRuleBetween(r, start, start.Add(24*time.Hour), last, last); !ok || !a.Equal(last) {
					t.Fatalf("historic matcher got %v, %v; want %v", a, ok, last)
				}
			}
		})
	}
}

func TestWalkRRuleUntilUTCCompatibility(t *testing.T) {
	// The former StrToROption call parsed local/date UNTIL in UTC before
	// DTSTART was assigned. Preserve that behavior for persisted definitions.
	for _, zone := range []string{"America/New_York", "Asia/Tokyo"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			for _, until := range []string{"20240102", "20240102T000000", "20240102T000000Z"} {
				r, err := ParseRRule("FREQ=DAILY;UNTIL=" + until)
				if err != nil {
					t.Fatal(err)
				}
				boundary := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
				if !r.Until.Equal(boundary) {
					t.Fatalf("%s parsed as %v, want %v", until, r.Until, boundary)
				}
				// A local-midnight start distinguishes UTC from local UNTIL.
				start := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
				count := 0
				err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 5), func(v time.Time) bool {
					if v.After(boundary) {
						t.Fatalf("%s yielded %v past UTC boundary", until, v)
					}
					count++
					return true
				})
				want := 1
				if zone == "Asia/Tokyo" {
					want = 2
				}
				if err != nil || count != want {
					t.Fatalf("%s count=%d want=%d error=%v", until, count, want, err)
				}
				// A start at the UTC boundary must still be included in either zone.
				count = 0
				err = walkRRule(context.Background(), r, boundary.In(loc), boundary.AddDate(0, 0, 1), func(time.Time) bool { count++; return true })
				if err != nil || count != 1 {
					t.Fatalf("%s inclusive boundary count=%d error=%v", until, count, err)
				}
			}
		})
	}
}

func TestWalkRRuleLimits(t *testing.T) {
	start := rruleTestTime(t, "2024-01-01T09:00:00Z")
	t.Run("stop immediately and default interval", func(t *testing.T) {
		n := 0
		err := walkRRule(context.Background(), &RRule{Freq: "DAILY"}, start, start.AddDate(1000, 0, 0), func(time.Time) bool { n++; return false })
		if err != nil || n != 1 {
			t.Fatalf("calls=%d error=%v", n, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := walkRRule(ctx, &RRule{Freq: "DAILY"}, start, start, func(time.Time) bool { t.Fatal("unexpected yield"); return true })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("cancel during iteration", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := walkRRule(ctx, &RRule{Freq: "DAILY"}, start, start.AddDate(1, 0, 0), func(time.Time) bool { cancel(); return true })
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("empty periods bounded", func(t *testing.T) {
		err := walkRRule(context.Background(), &RRule{Freq: "MONTHLY", ByMonth: []int{2}, ByMonthDay: []int{30}}, start, start.AddDate(1000, 0, 0), func(time.Time) bool { t.Fatal("unexpected yield"); return true })
		if err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatal(err)
		}
	})
	t.Run("expansion bounded", func(t *testing.T) {
		r := &RRule{Freq: "YEARLY"}
		for i := range 24 {
			r.ByHour = append(r.ByHour, i)
		}
		for i := range 60 {
			r.ByMinute = append(r.ByMinute, i)
			r.BySecond = append(r.BySecond, i)
		}
		r.ByDay = []string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}
		err := walkRRule(context.Background(), r, start, start.AddDate(1, 0, 0), func(time.Time) bool { return true })
		if err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatal(err)
		}
	})
	t.Run("filter work bounded", func(t *testing.T) {
		r := &RRule{Freq: "DAILY", ByMonth: make([]int, 100000)}
		for i := range r.ByMonth {
			r.ByMonth[i] = 2
		}
		err := walkRRule(context.Background(), r, start, start, func(time.Time) bool { t.Fatal("unexpected yield"); return true })
		if err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatal(err)
		}
	})
	for _, r := range []*RRule{nil, {Freq: "BAD"}, {Freq: "HOURLY"}, {Freq: "MINUTELY"}, {Freq: "SECONDLY"}, {Freq: "DAILY", BySecond: []int{60}}, {Freq: "DAILY", Interval: -1}} {
		if err := walkRRule(context.Background(), r, start, start, func(time.Time) bool { return true }); err == nil {
			t.Fatalf("expected error for %+v", r)
		}
	}
}

func TestWalkRRuleDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 3, 9, 2, 30, 0, 0, loc)
	r, _ := ParseRRule("FREQ=DAILY;COUNT=3")
	var got []string
	err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 5), func(v time.Time) bool { got = append(got, v.Format(time.RFC3339)); return true })
	want := []string{"2024-03-09T02:30:00-05:00", "2024-03-11T02:30:00-04:00", "2024-03-12T02:30:00-04:00"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v error %v", got, err)
	}
}

func TestRRuleMatcherBoundaries(t *testing.T) {
	start := rruleTestTime(t, "2024-01-01T09:00:00Z")
	end := start.Add(2 * time.Hour)
	r, _ := ParseRRule("FREQ=DAILY;COUNT=2")
	if _, _, ok := MatchRRuleAt(r, start, end, end); ok {
		t.Fatal("At must exclude occurrence end")
	}
	if a, _, ok := MatchRRuleBetween(r, start, end, end, end); !ok || !a.Equal(start) {
		t.Fatal("Between must include occurrence end")
	}
	if a, _, ok := MatchRRuleBetween(r, start, end, start.Add(time.Hour), end.Add(time.Hour)); !ok || !a.Equal(start) {
		t.Fatal("must find occurrence starting before window")
	}
	if _, _, ok := MatchRRuleBetween(r, start, end, start.AddDate(0, 0, 2), start.AddDate(0, 0, 5)); ok {
		t.Fatal("COUNT must not reset at query start")
	}
	if _, _, ok := MatchRRuleAt(r, start, end, start.AddDate(0, 0, 2)); ok {
		t.Fatal("At exceeded COUNT")
	}
	r, _ = ParseRRule("FREQ=MONTHLY;BYDAY=MO;COUNT=2")
	if _, _, ok := MatchRRuleBetween(r, start, end, start.AddDate(0, 0, 10), start.AddDate(0, 2, 0)); ok {
		t.Fatal("COUNT must count candidates, not periods")
	}
}

func TestParseRRuleValidation(t *testing.T) {
	for _, s := range []string{
		"FREQ=DAILY;UNTIL=20240101T090000.1Z", "FREQ=DAILY;UNTIL=20240101T090000,1", "FREQ=YEARLY;BYDAY=001MO",
		"", "COUNT=2", "FREQ=BOGUS", "FREQ=DAILY;", "FREQ=DAILY;;COUNT=1", "FREQ=DAILY;FREQ=WEEKLY", "FREQ=DAILY;X=1", "FREQ=DAILY;COUNT=0", "FREQ=DAILY;COUNT=-1", "FREQ=DAILY;INTERVAL=0", "FREQ=DAILY;INTERVAL=-1", "FREQ=DAILY;COUNT=2147483648", "FREQ=DAILY;BYHOUR=1,no", "FREQ=DAILY;BYHOUR=", "FREQ=DAILY;BYHOUR=1,", "FREQ=DAILY;BYHOUR=24", "FREQ=DAILY;BYMINUTE=60", "FREQ=DAILY;BYSECOND=61", "FREQ=DAILY;BYSECOND=-1", "FREQ=MONTHLY;BYMONTHDAY=0", "FREQ=MONTHLY;BYMONTHDAY=-32", "FREQ=YEARLY;BYYEARDAY=367", "FREQ=YEARLY;BYWEEKNO=0", "FREQ=YEARLY;BYWEEKNO=-54", "FREQ=YEARLY;BYMONTH=13", "FREQ=DAILY;BYDAY=XX", "FREQ=MONTHLY;BYDAY=0MO", "FREQ=YEARLY;BYDAY=54MO", "FREQ=DAILY;BYDAY=1MO", "FREQ=YEARLY;BYWEEKNO=1;BYDAY=1MO", "FREQ=MONTHLY;BYWEEKNO=1", "FREQ=DAILY;BYYEARDAY=1", "FREQ=WEEKLY;BYMONTHDAY=1", "FREQ=DAILY;WKST=1MO", "FREQ=DAILY;BYSETPOS=1", "FREQ=DAILY;BYHOUR=1;BYSETPOS=0", "FREQ=DAILY;COUNT=1;UNTIL=20240101", "FREQ=DAILY;UNTIL=20240230", "FREQ=DAILY;UNTIL=garbage",
	} {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseRRule(s); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
	for _, s := range []string{"freq=weekly;byday=mo,fr;wkst=su", "FREQ=YEARLY;BYDAY=+2MO,-53SU", "FREQ=DAILY;UNTIL=20240101", "FREQ=DAILY;UNTIL=20240101T090000", "FREQ=DAILY;UNTIL=20240101T090000Z", "FREQ=SECONDLY;BYSECOND=60", "FREQ=MONTHLY;BYMONTHDAY=-31,31;BYSETPOS=-366,366"} {
		r, err := ParseRRule(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if r.Org() != s {
			t.Fatal("Org changed")
		}
	}
}
