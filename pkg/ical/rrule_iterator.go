package ical

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// walkRRule yields ordered, unique starts through to (inclusive). COUNT is
// measured from DTSTART, not the caller's query window. Work includes empty
// periods and rejected candidates, so impossible rules cannot loop forever.
func walkRRule(ctx context.Context, rule *RRule, start, to time.Time, yield func(time.Time) bool) error {
	return walkRRuleFrom(ctx, rule, start, start, to, yield)
}

// walkRRuleFrom yields starts in [from, to], retaining DTSTART-based COUNT and
// full-period BYSETPOS semantics. Callers must include any overlap lookback in from.
// Complex COUNT rules scan from DTSTART and return an error if work is exhausted.
func walkRRuleFrom(ctx context.Context, rule *RRule, start, from, to time.Time, yield func(time.Time) bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRRule(rule); err != nil {
		return err
	}
	if rule.Until != nil {
		until := *rule.Until
		switch rule.UntilType {
		case "DATE":
			// DATE is inclusive of the entire civil date, including for callers
			// that supply a non-midnight DTSTART.
			until = time.Date(until.Year(), until.Month(), until.Day()+1, 0, 0, 0, 0, start.Location()).Add(-time.Nanosecond)
		case "FLOATING":
			until = firstRRuleLocalTime(time.Date(until.Year(), until.Month(), until.Day(), until.Hour(), until.Minute(), until.Second(), until.Nanosecond(), start.Location()))
		}
		if until.Before(to) {
			to = until
		}
	}
	if to.Before(start) || to.Before(from) {
		return nil
	}
	interval := max(rule.Interval, 1)
	wkst := parseWkst(rule.Wkst)
	// Iterate civil dates in UTC, then construct wall-clock candidates in DTSTART's
	// location. DST must not change period boundaries or normalize missing dates.
	period := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	var unit int64
	switch rule.Freq {
	case "HOURLY":
		unit = 3600
		period = period.Add(time.Duration(start.Hour()) * time.Hour)
	case "MINUTELY":
		unit = 60
		period = period.Add(time.Duration(start.Hour()*60+start.Minute()) * time.Minute)
	case "SECONDLY":
		unit = 1
		period = period.Add(time.Duration(start.Hour()*3600+start.Minute()*60+start.Second()) * time.Second)
	case "WEEKLY":
		period = startOfWeek(period, wkst)
	case "MONTHLY":
		period = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "YEARLY":
		period = time.Date(start.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		if len(rule.ByWeekNo) != 0 {
			period = rruleWeekOne(start.Year(), wkst)
		}
	}
	localTo := to.In(start.Location())
	lastDate := time.Date(localTo.Year(), localTo.Month(), localTo.Day(), 0, 0, 0, 0, time.UTC)
	if unit != 0 {
		lastDate = lastDate.Add(time.Duration(localTo.Hour()*3600+localTo.Minute()*60+localTo.Second()) * time.Second)
	}
	hours, minutes, seconds := rule.ByHour, rule.ByMinute, rule.BySecond
	if len(hours) == 0 {
		hours = []int{start.Hour()}
	}
	if len(minutes) == 0 {
		minutes = []int{start.Minute()}
	}
	if len(seconds) == 0 {
		seconds = []int{start.Second()}
	}
	budget := 100000
	spend := func(work int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		budget -= work
		if budget < 0 {
			return fmt.Errorf("RRULE exceeds expansion/work limit (100000)")
		}
		return nil
	}
	// One date-filtering unit covers up to 32 BY-value comparisons. Charging
	// every comparison as a whole date exhausts ordinary century-old holidays.
	// Larger lists still pay proportionally, plus their initial scan cost.
	filterWork := len(rule.ByMonth) + 2*len(rule.ByMonthDay) + 2*len(rule.ByYearDay) + 2*len(rule.ByWeekNo) + len(rule.ByDay)
	timeWork := len(rule.ByHour) + len(rule.ByMinute) + len(rule.BySecond) + len(rule.BySetPos)
	if err := spend(filterWork + timeWork); err != nil {
		return err
	}
	dateWork := max(1, (filterWork+31)/32)
	count := 0
	// Civil arithmetic avoids replaying decades of seconds. COUNT can only be
	// skipped when every period contributes exactly one start, with no DST gaps.
	zoneStart, zoneEnd := start.ZoneBounds()
	simpleCount := filterWork+timeWork == 0 && zoneStart.IsZero() && zoneEnd.IsZero()
	if unit != 0 && from.After(start) && (rule.Count == nil || simpleCount) {
		localFrom := from.In(start.Location())
		civilFrom := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), localFrom.Hour(), localFrom.Minute(), localFrom.Second(), 0, time.UTC)
		step := unit * int64(interval)
		skipped := (civilFrom.Unix() - period.Unix()) / step
		if skipped > 0 {
			if rule.Count != nil {
				if skipped >= int64(*rule.Count) {
					return ctx.Err()
				}
				count = int(skipped)
			}
			period = time.Unix(period.Unix()+skipped*step, 0).UTC()
		}
	}
	for !period.After(lastDate) {
		if err := spend(1); err != nil {
			return err
		}
		var end, next time.Time
		switch rule.Freq {
		case "HOURLY", "MINUTELY", "SECONDLY":
			end = period.Add(time.Second)
			next = time.Unix(period.Unix()+unit*int64(interval), 0).UTC()
		case "DAILY":
			end, next = period.AddDate(0, 0, 1), period.AddDate(0, 0, interval)
		case "WEEKLY":
			end, next = period.AddDate(0, 0, 7), period.AddDate(0, 0, 7*interval)
		case "MONTHLY":
			end, next = period.AddDate(0, 1, 0), period.AddDate(0, interval, 0)
		case "YEARLY":
			end, next = period.AddDate(1, 0, 0), period.AddDate(interval, 0, 0)
			if len(rule.ByWeekNo) != 0 {
				// The fourth day always belongs to the period's week-year.
				year := period.AddDate(0, 0, 3).Year()
				end, next = rruleWeekOne(year+1, wkst), rruleWeekOne(year+interval, wkst)
			}
		}
		var candidates []time.Time
		periodHours, periodMinutes, periodSeconds := hours, minutes, seconds
		if unit != 0 {
			periodHours = []int{period.Hour()}
			if len(rule.ByHour) != 0 && !slices.Contains(rule.ByHour, period.Hour()) {
				periodHours = nil
			}
			if unit <= 60 {
				periodMinutes = []int{period.Minute()}
				if len(rule.ByMinute) != 0 && !slices.Contains(rule.ByMinute, period.Minute()) {
					periodMinutes = nil
				}
			}
			if unit == 1 {
				periodSeconds = []int{period.Second()}
				if len(rule.BySecond) != 0 && !slices.Contains(rule.BySecond, period.Second()) && !(period.Second() == 59 && slices.Contains(rule.BySecond, 60)) {
					periodSeconds = nil
				}
			}
			if err := spend(max(1, (timeWork+31)/32)); err != nil {
				return err
			}
		}
		for day := period; day.Before(end); day = day.AddDate(0, 0, 1) {
			if err := spend(dateWork); err != nil {
				return err
			}
			if !matchRRuleDate(rule, day, start, wkst) {
				continue
			}
			for _, hour := range periodHours {
				for _, minute := range periodMinutes {
					for _, second := range periodSeconds {
						if err := spend(1); err != nil {
							return err
						}
						// Go is not leap-second aware; RFC5545 maps second 60 to 59.
						second = min(second, 59)
						candidate := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, second, start.Nanosecond(), start.Location())
						// RFC5545 excludes nonexistent local times (and they do not count).
						if candidate.Year() != day.Year() || candidate.Month() != day.Month() || candidate.Day() != day.Day() || candidate.Hour() != hour || candidate.Minute() != minute || candidate.Second() != second {
							continue
						}
						candidate = firstRRuleLocalTime(candidate)
						candidates = append(candidates, candidate)
					}
				}
			}
		}
		slices.SortFunc(candidates, func(a, b time.Time) int { return a.Compare(b) })
		candidates = slices.CompactFunc(candidates, time.Time.Equal)
		if len(rule.BySetPos) != 0 {
			selected := make([]time.Time, 0, min(len(rule.BySetPos), len(candidates)))
			for _, pos := range rule.BySetPos {
				if err := spend(1); err != nil {
					return err
				}
				i := pos - 1
				if pos < 0 {
					i = len(candidates) + pos
				}
				if i >= 0 && i < len(candidates) {
					selected = append(selected, candidates[i])
				}
			}
			slices.SortFunc(selected, func(a, b time.Time) int { return a.Compare(b) })
			candidates = slices.CompactFunc(selected, time.Time.Equal)
		}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return err
			}
			if candidate.Before(start) {
				continue
			}
			if candidate.After(to) {
				return nil
			}
			count++
			if !candidate.Before(from) && !yield(candidate) {
				return nil
			}
			if rule.Count != nil && count >= *rule.Count {
				return ctx.Err()
			}
		}
		if !next.After(period) {
			return fmt.Errorf("RRULE period overflow")
		}
		period = next
	}
	return ctx.Err()
}

// Go may choose either side of a fold; RFC5545 requires the first. Inspect
// only the preceding zone interval, keeping this work constant per candidate.
func firstRRuleLocalTime(t time.Time) time.Time {
	boundary, _ := t.ZoneBounds()
	if boundary.IsZero() {
		return t
	}
	_, offset := t.Zone()
	_, previous := boundary.Add(-time.Nanosecond).Zone()
	if previous > offset {
		earlier := t.Add(-time.Duration(previous-offset) * time.Second)
		if earlier.Year() == t.Year() && earlier.YearDay() == t.YearDay() && earlier.Hour() == t.Hour() && earlier.Minute() == t.Minute() && earlier.Second() == t.Second() {
			return earlier
		}
	}
	return t
}

func matchRRuleDate(r *RRule, day, start time.Time, wkst time.Weekday) bool {
	if len(r.ByMonth) != 0 && !slices.Contains(r.ByMonth, int(day.Month())) {
		return false
	}
	monthDays := daysInMonth(day.Year(), day.Month())
	yearDays := time.Date(day.Year(), 12, 31, 0, 0, 0, 0, time.UTC).YearDay()
	if !matchesRRuleIndex(r.ByMonthDay, day.Day(), monthDays) || !matchesRRuleIndex(r.ByYearDay, day.YearDay(), yearDays) {
		return false
	}
	if len(r.ByWeekNo) != 0 {
		weekYear := day.Year()
		first := rruleWeekOne(weekYear, wkst)
		if day.Before(first) {
			weekYear--
			first = rruleWeekOne(weekYear, wkst)
		} else if !day.Before(rruleWeekOne(weekYear+1, wkst)) {
			weekYear++
			first = rruleWeekOne(weekYear, wkst)
		}
		week := int(day.Sub(first).Hours()/24)/7 + 1
		weeks := int(rruleWeekOne(weekYear+1, wkst).Sub(first).Hours()/24) / 7
		if !matchesRRuleIndex(r.ByWeekNo, week, weeks) {
			return false
		}
	}
	if len(r.ByDay) != 0 {
		matched := false
		for _, value := range r.ByDay {
			if parseWkst(value[len(value)-2:]) != day.Weekday() {
				continue
			}
			if len(value) == 2 {
				matched = true
				break
			}
			n, _ := strconv.Atoi(value[:len(value)-2])
			index, total := day.Day(), monthDays
			if r.Freq == "YEARLY" && len(r.ByMonth) == 0 {
				index, total = day.YearDay(), yearDays
			}
			if n > 0 && (index-1)/7+1 == n || n < 0 && -((total-index)/7+1) == n {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	// Missing expanding date fields inherit DTSTART, not every day in the period.
	switch r.Freq {
	case "WEEKLY":
		if len(r.ByDay) == 0 && day.Weekday() != start.Weekday() {
			return false
		}
	case "MONTHLY":
		if len(r.ByMonthDay) == 0 && len(r.ByDay) == 0 && day.Day() != start.Day() {
			return false
		}
	case "YEARLY":
		if len(r.ByYearDay) == 0 && len(r.ByWeekNo) == 0 && len(r.ByMonthDay) == 0 && len(r.ByDay) == 0 {
			if day.Day() != start.Day() || len(r.ByMonth) == 0 && day.Month() != start.Month() {
				return false
			}
		}
	}
	return true
}

func matchesRRuleIndex(values []int, index, total int) bool {
	return len(values) == 0 || slices.Contains(values, index) || slices.Contains(values, index-total-1)
}

func parseWkst(s string) time.Weekday {
	if s == "" {
		return time.Monday
	}
	return time.Weekday(slices.Index([]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}, strings.ToUpper(s)))
}

func startOfWeek(t time.Time, wkst time.Weekday) time.Time {
	return t.AddDate(0, 0, -(int(t.Weekday())-int(wkst)+7)%7)
}

// The first week has at least four days in its year, for any WKST.
func rruleWeekOne(year int, wkst time.Weekday) time.Time {
	return startOfWeek(time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC), wkst)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
