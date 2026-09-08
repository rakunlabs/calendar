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
		{"week year interval excludes next year", "FREQ=YEARLY;INTERVAL=2;BYWEEKNO=1;BYDAY=MO", "2024-01-01T09:00:00Z", "2026-01-01T09:00:00Z", []string{"2024-01-01T09:00:00Z", "2025-12-29T09:00:00Z"}},
		{"week year setpos includes preceding December", "FREQ=YEARLY;BYWEEKNO=1;BYSETPOS=1", "2024-01-01T09:00:00Z", "2024-12-30T09:00:00Z", []string{"2024-01-01T09:00:00Z", "2024-12-30T09:00:00Z"}},
		{"last week interval includes following January", "FREQ=YEARLY;INTERVAL=2;BYWEEKNO=-1;BYSETPOS=-1", "2020-01-01T09:00:00Z", "2023-01-02T09:00:00Z", []string{"2021-01-03T09:00:00Z", "2023-01-01T09:00:00Z"}},
		{"week 53 interval and count", "FREQ=YEARLY;INTERVAL=5;BYWEEKNO=53;BYDAY=SU;COUNT=2", "2015-01-01T09:00:00Z", "2026-01-01T09:00:00Z", []string{"2016-01-03T09:00:00Z", "2021-01-03T09:00:00Z"}},
		{"week year sunday interval", "FREQ=YEARLY;INTERVAL=2;BYWEEKNO=1;BYDAY=SU;WKST=SU;BYSETPOS=1", "2023-01-01T09:00:00Z", "2025-01-01T09:00:00Z", []string{"2023-01-01T09:00:00Z", "2024-12-29T09:00:00Z"}},
		{"week year month filter on adjacent date", "FREQ=YEARLY;INTERVAL=2;BYWEEKNO=-1;BYMONTH=1;BYSETPOS=-1", "2020-01-01T09:00:00Z", "2023-01-02T09:00:00Z", []string{"2021-01-03T09:00:00Z", "2023-01-01T09:00:00Z"}},
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
		{"hour expansion and filter", "FREQ=HOURLY;BYHOUR=9,11;BYMINUTE=30,0;BYSECOND=20,10;COUNT=5", "2024-01-01T09:00:15Z", "2024-01-01T12:00:00Z", []string{"2024-01-01T09:00:20Z", "2024-01-01T09:30:10Z", "2024-01-01T09:30:20Z", "2024-01-01T11:00:10Z", "2024-01-01T11:00:20Z"}},
		{"hour setpos full period", "FREQ=HOURLY;BYMINUTE=0,30;BYSETPOS=1;COUNT=2", "2024-01-01T09:15:00Z", "2024-01-01T11:15:00Z", []string{"2024-01-01T10:00:00Z", "2024-01-01T11:00:00Z"}},
		{"minute expansion and filter", "FREQ=MINUTELY;BYHOUR=9;BYMINUTE=0,2;BYSECOND=10,20;COUNT=3", "2024-01-01T09:00:15Z", "2024-01-01T09:03:00Z", []string{"2024-01-01T09:00:20Z", "2024-01-01T09:02:10Z", "2024-01-01T09:02:20Z"}},
		{"minute setpos", "FREQ=MINUTELY;INTERVAL=2;BYSECOND=10,20;BYSETPOS=-1;COUNT=2", "2024-01-01T09:00:00Z", "2024-01-01T09:05:00Z", []string{"2024-01-01T09:00:20Z", "2024-01-01T09:02:20Z"}},
		{"second filters and interval", "FREQ=SECONDLY;INTERVAL=2;BYHOUR=9;BYMINUTE=0;BYSECOND=1,2,4;BYSETPOS=-1,1;COUNT=2", "2024-01-01T09:00:00Z", "2024-01-01T09:01:00Z", []string{"2024-01-01T09:00:02Z", "2024-01-01T09:00:04Z"}},
		{"second setpos not across seconds", "FREQ=SECONDLY;BYSECOND=0,1,2;BYSETPOS=2", "2024-01-01T09:00:00Z", "2024-01-01T09:00:02Z", nil},
		{"second until leap fallback", "FREQ=SECONDLY;BYSECOND=59,60;UNTIL=20240101T090160Z", "2024-01-01T09:00:58Z", "2024-01-01T09:03:00Z", []string{"2024-01-01T09:00:59Z", "2024-01-01T09:01:59Z"}},
		{"hour date filters", "FREQ=HOURLY;BYMONTH=1;BYMONTHDAY=2;BYYEARDAY=2;BYDAY=TU;COUNT=2", "2024-01-01T23:00:00Z", "2024-01-03T00:00:00Z", []string{"2024-01-02T00:00:00Z", "2024-01-02T01:00:00Z"}},
		{"minute date filters", "FREQ=MINUTELY;BYMONTH=1;BYMONTHDAY=2;BYYEARDAY=2;BYDAY=TU;COUNT=2", "2024-01-01T23:59:00Z", "2024-01-02T00:02:00Z", []string{"2024-01-02T00:00:00Z", "2024-01-02T00:01:00Z"}},
		{"second date filters", "FREQ=SECONDLY;BYMONTH=1;BYMONTHDAY=2;BYYEARDAY=2;BYDAY=TU;COUNT=2", "2024-01-01T23:59:59Z", "2024-01-02T00:00:02Z", []string{"2024-01-02T00:00:00Z", "2024-01-02T00:00:01Z"}},
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

func TestWalkRRuleLeapSecondDedup(t *testing.T) {
	for _, freq := range []string{"SECONDLY", "MINUTELY", "HOURLY", "DAILY", "WEEKLY", "MONTHLY", "YEARLY"} {
		t.Run(freq, func(t *testing.T) {
			for _, suffix := range []string{"", ";BYSETPOS=-1,1"} {
				r, err := ParseRRule("FREQ=" + freq + ";BYSECOND=60,59,60;COUNT=2" + suffix)
				if err != nil {
					t.Fatal(err)
				}
				start := rruleTestTime(t, "2024-01-01T09:00:59Z")
				var got []time.Time
				err = walkRRule(context.Background(), r, start, start.AddDate(3, 0, 0), func(v time.Time) bool { got = append(got, v); return true })
				if err != nil || len(got) != 2 || !got[0].Equal(start) || !got[1].After(got[0]) || got[1].Second() != 59 {
					t.Fatalf("got %v error=%v", got, err)
				}
				if !reflect.DeepEqual(r.BySecond, []int{60, 59, 60}) {
					t.Fatal("walker mutated parsed rule")
				}
			}
		})
	}
}

func TestWalkRRuleFromSubday(t *testing.T) {
	start := rruleTestTime(t, "1900-01-01T00:00:00Z")
	from := rruleTestTime(t, "2026-01-01T00:00:00Z")
	for _, definition := range []string{"FREQ=SECONDLY", "FREQ=MINUTELY", "FREQ=HOURLY", "FREQ=SECONDLY;BYSECOND=0,1;BYSETPOS=1", "FREQ=SECONDLY;COUNT=2147483647"} {
		r, err := ParseRRule(definition)
		if err != nil {
			t.Fatal(err)
		}
		var got []time.Time
		err = walkRRuleFrom(context.Background(), r, start, from, from.Add(time.Second), func(v time.Time) bool { got = append(got, v); return true })
		want := 1
		if r.Freq == "SECONDLY" {
			want = 2
		}
		if r.Count != nil {
			want = 0
		}
		if err != nil || len(got) != want || len(got) > 0 && !got[0].Equal(from) {
			t.Fatalf("%s: got %v error=%v", definition, got, err)
		}
	}
	// COUNT must include starts skipped by the lower bound, not restart there.
	r, _ := ParseRRule("FREQ=SECONDLY;INTERVAL=2;COUNT=5")
	var got []time.Time
	err := walkRRuleFrom(context.Background(), r, start, start.Add(5*time.Second), start.Add(20*time.Second), func(v time.Time) bool { got = append(got, v); return true })
	if err != nil || len(got) != 2 || !got[0].Equal(start.Add(6*time.Second)) || !got[1].Equal(start.Add(8*time.Second)) {
		t.Fatalf("COUNT seek: %v error=%v", got, err)
	}
	r, _ = ParseRRule("FREQ=SECONDLY;BYSECOND=0;COUNT=2147483647")
	err = walkRRuleFrom(context.Background(), r, start, from, from, func(time.Time) bool { t.Fatal("unexpected yield"); return true })
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("complex COUNT must report exhausted work: %v", err)
	}
	// Seeking must retain the beginning of the period for BYSETPOS selection.
	r, _ = ParseRRule("FREQ=HOURLY;BYMINUTE=0,30;BYSETPOS=1")
	err = walkRRuleFrom(context.Background(), r, start, from.Add(15*time.Minute), from.Add(45*time.Minute), func(time.Time) bool { t.Fatal("partial-period selection"); return true })
	if err != nil {
		t.Fatal(err)
	}
}

func TestWalkRRuleSubdayDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		freq, start string
		want        []string
	}{
		{"HOURLY", "2024-03-10T01:30:00-05:00", []string{"2024-03-10T01:30:00-05:00", "2024-03-10T03:30:00-04:00", "2024-03-10T04:30:00-04:00"}},
		{"MINUTELY", "2024-03-10T01:59:30-05:00", []string{"2024-03-10T01:59:30-05:00", "2024-03-10T03:00:30-04:00", "2024-03-10T03:01:30-04:00"}},
		{"SECONDLY", "2024-03-10T01:59:59-05:00", []string{"2024-03-10T01:59:59-05:00", "2024-03-10T03:00:00-04:00", "2024-03-10T03:00:01-04:00"}},
		{"HOURLY", "2024-11-03T00:30:00-04:00", []string{"2024-11-03T00:30:00-04:00", "2024-11-03T01:30:00-04:00", "2024-11-03T02:30:00-05:00"}},
		{"MINUTELY", "2024-11-03T01:59:00-04:00", []string{"2024-11-03T01:59:00-04:00", "2024-11-03T02:00:00-05:00", "2024-11-03T02:01:00-05:00"}},
		{"SECONDLY", "2024-11-03T01:59:59-04:00", []string{"2024-11-03T01:59:59-04:00", "2024-11-03T02:00:00-05:00", "2024-11-03T02:00:01-05:00"}},
	} {
		t.Run(tt.freq+tt.start, func(t *testing.T) {
			start := rruleTestTime(t, tt.start).In(loc)
			r, _ := ParseRRule("FREQ=" + tt.freq + ";COUNT=3")
			var got []string
			err := walkRRule(context.Background(), r, start, start.Add(6*time.Hour), func(v time.Time) bool { got = append(got, v.Format(time.RFC3339)); return true })
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v error=%v", got, tt.want, err)
			}
		})
	}
}

func TestWalkRRuleSubdayUntilAndCancellation(t *testing.T) {
	start := rruleTestTime(t, "2024-01-01T09:00:00Z")
	for _, freq := range []string{"HOURLY", "MINUTELY", "SECONDLY"} {
		t.Run(freq, func(t *testing.T) {
			r, err := ParseRRule("FREQ=" + freq + ";UNTIL=20240101T100000Z")
			if err != nil {
				t.Fatal(err)
			}
			boundary := start.Add(time.Hour)
			var last time.Time
			err = walkRRule(context.Background(), r, start, boundary.Add(time.Hour), func(v time.Time) bool {
				if v.After(boundary) {
					t.Fatalf("past UNTIL: %v", v)
				}
				last = v
				return true
			})
			if err != nil || !last.Equal(boundary) {
				t.Fatalf("inclusive UNTIL last=%v error=%v", last, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err = walkRRuleFrom(ctx, r, start, start, boundary, func(time.Time) bool { cancel(); return true })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}

func TestWalkRRuleSeekMatchesFullWalk(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, anchor := range []string{"2024-03-10T01:58:00-05:00", "2024-11-03T00:58:00-04:00"} {
		start := rruleTestTime(t, anchor).In(loc)
		for _, definition := range []string{
			"FREQ=HOURLY;INTERVAL=2;BYMINUTE=0,30;BYSETPOS=-1",
			"FREQ=MINUTELY;INTERVAL=7;BYSECOND=10,60;BYSETPOS=-1",
			"FREQ=SECONDLY;INTERVAL=17;BYSECOND=0,17,34,51,59,60",
			"FREQ=MINUTELY;COUNT=100",
		} {
			r, err := ParseRRule(definition)
			if err != nil {
				t.Fatal(err)
			}
			from, to := start.Add(90*time.Minute), start.Add(3*time.Hour)
			var want, got []time.Time
			err = walkRRule(context.Background(), r, start, to, func(v time.Time) bool {
				if !v.Before(from) {
					want = append(want, v)
				}
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
			err = walkRRuleFrom(context.Background(), r, start, from, to, func(v time.Time) bool { got = append(got, v); return true })
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s %s: got %v want %v error=%v", anchor, definition, got, want, err)
			}
		}
	}
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

func TestWalkRRuleUntilTypes(t *testing.T) {
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
				boundary := time.Date(2024, 1, 2, 0, 0, 0, 0, loc)
				if r.UntilType == "UTC" {
					boundary = time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
				}
				start := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
				count := 0
				err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 5), func(v time.Time) bool {
					if v.After(boundary) {
						t.Fatalf("%s yielded %v past boundary", until, v)
					}
					count++
					return true
				})
				want := 2
				if r.UntilType == "UTC" && zone == "America/New_York" {
					want = 1
				}
				if err != nil || count != want {
					t.Fatalf("%s count=%d want=%d error=%v", until, count, want, err)
				}
				// A start at the effective boundary must still be included.
				count = 0
				err = walkRRule(context.Background(), r, boundary.In(loc), boundary.AddDate(0, 0, 1), func(time.Time) bool { count++; return true })
				if err != nil || count != 1 {
					t.Fatalf("%s inclusive boundary count=%d error=%v", until, count, err)
				}
			}
		})
	}
}

func TestWalkRRuleUntilCivilAndAbsolute(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 3, 9, 23, 30, 0, 0, loc)
	for _, tt := range []struct {
		until string
		want  int
	}{
		{"20240310", 2},
		{"20240310T233000", 2},
		{"20240310T232959", 1},
		{"20240310T233000Z", 1},
	} {
		r, err := ParseRRule("FREQ=DAILY;UNTIL=" + tt.until)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 4), func(time.Time) bool { count++; return true })
		if err != nil || count != tt.want {
			t.Fatalf("%s count=%d want=%d error=%v", tt.until, count, tt.want, err)
		}
	}
	until := time.Date(2024, 3, 10, 23, 30, 0, 0, time.UTC)
	r := &RRule{Freq: "DAILY", Until: &until}
	count := 0
	err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 4), func(time.Time) bool { count++; return true })
	if err != nil || count != 1 {
		t.Fatalf("manual absolute UNTIL count=%d error=%v", count, err)
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
	t.Run("empty week years bounded", func(t *testing.T) {
		err := walkRRule(context.Background(), &RRule{Freq: "YEARLY", ByWeekNo: []int{1}, ByMonth: []int{6}}, start, start.AddDate(1000, 0, 0), func(time.Time) bool { t.Fatal("unexpected yield"); return true })
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
	for _, r := range []*RRule{nil, {Freq: "BAD"}, {Freq: "DAILY", Interval: -1}} {
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

func TestWalkRRuleEuropeFallBack(t *testing.T) {
	for _, tt := range []struct {
		zone string
		hour int
		want string
	}{
		{"Europe/Berlin", 2, "2024-10-27T02:30:00+02:00"},
		{"Europe/London", 1, "2024-10-27T01:30:00+01:00"},
	} {
		t.Run(tt.zone, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Date(2024, 10, 26, tt.hour, 30, 0, 0, loc)
			for _, definition := range []string{"FREQ=DAILY;COUNT=3", "FREQ=DAILY;BYMINUTE=30;BYSETPOS=1;COUNT=3"} {
				r, err := ParseRRule(definition)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 4), func(v time.Time) bool { got = append(got, v.Format(time.RFC3339)); return true })
				want := []string{start.Format(time.RFC3339), tt.want, time.Date(2024, 10, 28, tt.hour, 30, 0, 0, loc).Format(time.RFC3339)}
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v want %v error=%v", got, want, err)
				}
			}
			// A floating UNTIL inside the fold uses the first instance too.
			fold := rruleTestTime(t, tt.want)
			r, err := ParseRRule("FREQ=DAILY;UNTIL=" + fold.Format("20060102T150405"))
			if err != nil {
				t.Fatal(err)
			}
			var got []time.Time
			err = walkRRule(context.Background(), r, start, start.AddDate(0, 0, 4), func(v time.Time) bool { got = append(got, v); return true })
			if err != nil || len(got) != 2 || !got[1].Equal(fold) {
				t.Fatalf("fold UNTIL got %v error=%v", got, err)
			}
		})
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
