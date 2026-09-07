package ical

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RRule represents an RFC5545 section 3.3.10 recurrence rule.
type RRule struct {
	Freq       string
	Until      *time.Time
	Count      *int
	Interval   int
	BySecond   []int
	ByMinute   []int
	ByHour     []int
	ByDay      []string
	ByMonthDay []int
	ByYearDay  []int
	ByWeekNo   []int
	ByMonth    []int
	BySetPos   []int
	Wkst       string

	org string
}

func (r *RRule) Org() string { return r.org }

// ParseRRule parses and validates an RRULE value, without the RRULE: prefix.
func ParseRRule(s string) (*RRule, error) {
	r := &RRule{Interval: 1, org: s}
	seen := map[string]bool{}
	for part := range strings.SplitSeq(s, ";") {
		key, val, ok := strings.Cut(part, "=")
		key = strings.ToUpper(key)
		if !ok || val == "" || seen[key] {
			return nil, fmt.Errorf("invalid or duplicate RRULE part: %q", part)
		}
		seen[key] = true
		var list *[]int
		switch key {
		case "FREQ":
			r.Freq = strings.ToUpper(val)
		case "UNTIL":
			t, err := parseTime(val)
			if err != nil {
				return nil, fmt.Errorf("invalid UNTIL: %w", err)
			}
			r.Until = &t
		case "COUNT", "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 || n > 2147483647 {
				return nil, fmt.Errorf("invalid %s: %q", key, val)
			}
			if key == "COUNT" {
				r.Count = &n
			} else {
				r.Interval = n
			}
		case "BYSECOND":
			list = &r.BySecond
		case "BYMINUTE":
			list = &r.ByMinute
		case "BYHOUR":
			list = &r.ByHour
		case "BYMONTHDAY":
			list = &r.ByMonthDay
		case "BYYEARDAY":
			list = &r.ByYearDay
		case "BYWEEKNO":
			list = &r.ByWeekNo
		case "BYMONTH":
			list = &r.ByMonth
		case "BYSETPOS":
			list = &r.BySetPos
		case "BYDAY":
			r.ByDay = strings.Split(strings.ToUpper(val), ",")
		case "WKST":
			r.Wkst = strings.ToUpper(val)
		default:
			return nil, fmt.Errorf("unknown RRULE key: %q", key)
		}
		if list != nil {
			for item := range strings.SplitSeq(val, ",") {
				n, err := strconv.Atoi(item)
				if err != nil {
					return nil, fmt.Errorf("invalid %s value: %q", key, item)
				}
				*list = append(*list, n)
			}
		}
	}
	if err := validateRRule(r); err != nil {
		return nil, err
	}
	return r, nil
}

// validateRRule also protects callers constructing rules directly. Interval zero
// is the default for struct literals, but is rejected when explicitly parsed.
func validateRRule(r *RRule) error {
	if r == nil || !slices.Contains([]string{"SECONDLY", "MINUTELY", "HOURLY", "DAILY", "WEEKLY", "MONTHLY", "YEARLY"}, r.Freq) {
		return fmt.Errorf("invalid RRULE frequency")
	}
	if r.Interval < 0 || r.Interval > 2147483647 || r.Count != nil && (*r.Count < 1 || *r.Count > 2147483647) {
		return fmt.Errorf("invalid RRULE interval or count")
	}
	if r.Count != nil && r.Until != nil {
		return fmt.Errorf("RRULE COUNT and UNTIL are mutually exclusive")
	}
	for _, field := range []struct {
		name     string
		values   []int
		min, max int
		nonzero  bool
	}{
		{"BYSECOND", r.BySecond, 0, 60, false},
		{"BYMINUTE", r.ByMinute, 0, 59, false},
		{"BYHOUR", r.ByHour, 0, 23, false},
		{"BYMONTHDAY", r.ByMonthDay, -31, 31, true},
		{"BYYEARDAY", r.ByYearDay, -366, 366, true},
		{"BYWEEKNO", r.ByWeekNo, -53, 53, true},
		{"BYMONTH", r.ByMonth, 1, 12, false},
		{"BYSETPOS", r.BySetPos, -366, 366, true},
	} {
		for _, n := range field.values {
			if n < field.min || n > field.max || field.nonzero && n == 0 {
				return fmt.Errorf("invalid %s value: %d", field.name, n)
			}
		}
	}
	if r.Wkst != "" && !validWeekday(r.Wkst) {
		return fmt.Errorf("invalid WKST: %q", r.Wkst)
	}
	for _, day := range r.ByDay {
		if len(day) < 2 || !validWeekday(day[len(day)-2:]) {
			return fmt.Errorf("invalid BYDAY: %q", day)
		}
		if len(day) > 2 {
			ordinal := day[:len(day)-2]
			digits := strings.TrimPrefix(strings.TrimPrefix(ordinal, "+"), "-")
			n, err := strconv.Atoi(ordinal)
			if err != nil || len(digits) > 2 || n == 0 || n < -53 || n > 53 {
				return fmt.Errorf("invalid BYDAY ordinal: %q", day)
			}
			if r.Freq != "MONTHLY" && r.Freq != "YEARLY" || r.Freq == "YEARLY" && len(r.ByWeekNo) != 0 {
				return fmt.Errorf("ordinal BYDAY requires MONTHLY or YEARLY without BYWEEKNO")
			}
		}
	}
	if len(r.ByWeekNo) != 0 && r.Freq != "YEARLY" {
		return fmt.Errorf("BYWEEKNO requires YEARLY")
	}
	if len(r.ByYearDay) != 0 && slices.Contains([]string{"DAILY", "WEEKLY", "MONTHLY"}, r.Freq) {
		return fmt.Errorf("BYYEARDAY is not valid with %s", r.Freq)
	}
	if len(r.ByMonthDay) != 0 && r.Freq == "WEEKLY" {
		return fmt.Errorf("BYMONTHDAY is not valid with WEEKLY")
	}
	if len(r.BySetPos) != 0 && len(r.BySecond)+len(r.ByMinute)+len(r.ByHour)+len(r.ByDay)+len(r.ByMonthDay)+len(r.ByYearDay)+len(r.ByWeekNo)+len(r.ByMonth) == 0 {
		return fmt.Errorf("BYSETPOS requires another BY rule part")
	}
	return nil
}

func validWeekday(s string) bool {
	return slices.Contains([]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}, strings.ToUpper(s))
}

// parseTime parses RFC5545 UTC/local DATE-TIME or DATE values. Zone-less
// values retain the UTC interpretation used by the former StrToROption path;
// they are not reinterpreted in DTSTART's location during iteration.
func parseTime(s string) (time.Time, error) {
	if len(s) == 16 && strings.HasSuffix(s, "Z") {
		return time.Parse("20060102T150405Z", s)
	}
	if len(s) == 8 {
		return time.Parse("20060102", s)
	}
	if len(s) == 15 {
		return time.Parse("20060102T150405", s)
	}
	return time.Time{}, fmt.Errorf("invalid RFC5545 date/time: %q", s)
}

// MatchRRuleAt finds an occurrence containing search in [start, end).
// Invalid rules and iterator errors are reported as no match by this legacy API.
func MatchRRuleAt(r *RRule, dtstart, dtend, search time.Time) (a, b time.Time, found bool) {
	duration := dtend.Sub(dtstart)
	if dtend.IsZero() || duration <= 0 {
		return
	}
	err := walkRRule(context.Background(), r, dtstart, search, func(candidate time.Time) bool {
		end := candidate.Add(duration)
		if !search.Before(candidate) && search.Before(end) {
			a, b, found = candidate, end, true
		}
		return !found
	})
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return
}

// MatchRRuleBetween finds the first occurrence overlapping the inclusive range.
// Invalid rules and iterator errors are reported as no match by this legacy API.
func MatchRRuleBetween(r *RRule, dtstart, dtend, dateFrom, dateTo time.Time) (a, b time.Time, found bool) {
	if dateFrom.After(dateTo) {
		return
	}
	duration := time.Duration(0)
	if !dtend.IsZero() && dtend.After(dtstart) {
		duration = dtend.Sub(dtstart)
	}
	err := walkRRule(context.Background(), r, dtstart, dateTo, func(candidate time.Time) bool {
		end := candidate.Add(duration)
		if !end.Before(dateFrom) {
			a, b, found = candidate, end, true
		}
		return !found
	})
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return
}
