package ical

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type calendarDuration struct {
	days  int
	exact time.Duration
}

var durationPattern = regexp.MustCompile(`^\+?P(?:(\d+)W|(\d+)D(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?|T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)$`)

func parseDuration(value string) (calendarDuration, error) {
	var d calendarDuration
	m := durationPattern.FindStringSubmatch(value)
	if m == nil || strings.HasSuffix(value, "T") {
		return d, fmt.Errorf("invalid DURATION %q", value)
	}
	for i, s := range m[1:] {
		if s == "" {
			continue
		}
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return d, fmt.Errorf("DURATION exceeds limit")
		}
		switch i {
		case 0:
			d.days = int(n) * 7
		case 1:
			d.days = int(n)
		default:
			unit := []time.Duration{time.Hour, time.Minute, time.Second}[(i-2)%3]
			if n > uint64((1<<63-1-int64(d.exact))/int64(unit)) {
				return d, fmt.Errorf("DURATION exceeds limit")
			}
			d.exact += time.Duration(n) * unit
		}
	}
	if d.days > 3652059 || d.days == 0 && d.exact == 0 {
		return d, fmt.Errorf("DURATION must be positive and within calendar limits")
	}
	return d, nil
}

func (d calendarDuration) end(start time.Time) time.Time {
	return start.AddDate(0, 0, d.days).Add(d.exact)
}
