package ical

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/worldline-go/types"
)

// GenerateICS generates an iCalendar (ICS) file content from a list of events.
func GenerateICS(events []models.Event, category string) (string, error) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//rakunlabs//calendar//EN\r\n")

	if category == "" {
		category = "Holidays"
	}

	for _, e := range events {
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString(fmt.Sprintf("UID:%s\r\n", e.ID))
		b.WriteString(fmt.Sprintf("CATEGORIES:%s\r\n", category))
		b.WriteString("CLASS:PUBLIC\r\n")

		name := escapeICS(e.Name)
		if strings.HasPrefix(name, "LANGUAGE=") {
			b.WriteString(fmt.Sprintf("SUMMARY;%s\r\n", name))
		} else {
			b.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", name))
		}

		description := escapeICS(e.Description)
		if description != "" {
			b.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", description))
		}

		from := e.DateFrom.Time
		to := e.DateTo.Time
		if e.Tz != "" {
			loc, err := time.LoadLocation(e.Tz)
			if err != nil {
				return "", fmt.Errorf("event %s timezone: %w", e.ID, err)
			}
			from, to = from.In(loc), to.In(loc)
		}

		if e.AllDay {
			// All-day event: DTSTART/DTEND in DATE format (YYYYMMDD)
			b.WriteString(fmt.Sprintf("DTSTART;VALUE=DATE:%s\r\n", from.Format("20060102")))
			b.WriteString(fmt.Sprintf("DTEND;VALUE=DATE:%s\r\n", to.Format("20060102")))
		} else {
			// Timed event: include TZID if not UTC
			fromLoc, toLoc := from.Location(), to.Location()
			if fromLoc != time.UTC {
				b.WriteString(fmt.Sprintf("DTSTART;TZID=%s:%s\r\n", fromLoc.String(), from.Format("20060102T150405")))
			} else {
				b.WriteString(fmt.Sprintf("DTSTART:%s\r\n", from.UTC().Format("20060102T150405Z")))
			}
			if toLoc != time.UTC {
				b.WriteString(fmt.Sprintf("DTEND;TZID=%s:%s\r\n", toLoc.String(), to.Format("20060102T150405")))
			} else {
				b.WriteString(fmt.Sprintf("DTEND:%s\r\n", to.UTC().Format("20060102T150405Z")))
			}
		}

		if rule := strings.TrimSpace(e.RRule); rule != "" {
			rule = strings.TrimPrefix(rule, "RRULE:")
			if _, err := ParseRRule(rule); err != nil {
				return "", fmt.Errorf("event %s recurrence must be a single RRULE (materialize FUNC before export): %w", e.ID, err)
			}
			b.WriteString(fmt.Sprintf("RRULE:%s\r\n", rule))
		}
		b.WriteString("TRANSP:TRANSPARENT\r\n")
		b.WriteString("END:VEVENT\r\n")
	}

	b.WriteString("END:VCALENDAR\r\n")

	return b.String(), nil
}

// ParseICS parses ICS file data and returns a slice of models.Event.
func ParseICS(data io.Reader, tz *time.Location) ([]models.Event, error) {
	defaultTZ := time.UTC
	if tz != nil {
		defaultTZ = tz
	}

	// Unfold before parsing, including RRULE continuations and a final line without LF.
	scanner := bufio.NewScanner(data)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(lines) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			lines[len(lines)-1] += line[1:]
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read ics: %w", err)
	}
	var events []models.Event
	var e models.Event
	inEvent := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if line == "BEGIN:VEVENT" {
			inEvent = true
			e = models.Event{}

			continue
		}
		if line == "END:VEVENT" && inEvent {
			inEvent = false
			if e.DateFrom.IsZero() {
				return nil, fmt.Errorf("event %s has missing or invalid DTSTART", e.ID)
			}
			e.Tz = e.DateFrom.Location().String()
			if e.DateTo.Time.IsZero() {
				if !e.AllDay {
					return nil, fmt.Errorf("event %s: timed event requires DTEND", e.ID)
				}
				e.DateTo = types.Time{Time: e.DateFrom.AddDate(0, 0, 1)}
			}
			if !e.DateTo.After(e.DateFrom.Time) {
				return nil, fmt.Errorf("event %s: DTEND must be after DTSTART", e.ID)
			}

			events = append(events, e)

			continue
		}
		if !inEvent {
			continue
		}

		if strings.HasPrefix(line, "UID:") {
			e.ID = strings.TrimPrefix(line, "UID:")
		} else if strings.HasPrefix(line, "SUMMARY:") {
			e.Name = unescapeICS(strings.TrimPrefix(line, "SUMMARY:"))
		} else if strings.HasPrefix(line, "SUMMARY;") {
			e.Name = unescapeICS(strings.TrimPrefix(line, "SUMMARY;"))
		} else if strings.HasPrefix(line, "DESCRIPTION:") {
			e.Description = unescapeICS(strings.TrimPrefix(line, "DESCRIPTION:"))
		} else if strings.HasPrefix(line, "DTSTART") {
			date, allDay, err := parseICSDate(line, defaultTZ)
			if err != nil {
				return nil, fmt.Errorf("event %s DTSTART: %w", e.ID, err)
			}
			e.DateFrom.Time, e.AllDay = date, allDay
		} else if strings.HasPrefix(line, "DTEND") {
			date, _, err := parseICSDate(line, defaultTZ)
			if err != nil {
				return nil, fmt.Errorf("event %s DTEND: %w", e.ID, err)
			}
			e.DateTo.Time = date
		} else if strings.HasPrefix(line, "RRULE:") {
			if e.RRule != "" {
				return nil, fmt.Errorf("event %s has multiple RRULE properties", e.ID)
			}
			if _, err := ParseRRule(strings.TrimPrefix(line, "RRULE:")); err != nil {
				return nil, fmt.Errorf("event %s recurrence: %w", e.ID, err)
			}
			e.RRule = line
		} else {
			switch property := strings.SplitN(strings.SplitN(line, ":", 2)[0], ";", 2)[0]; property {
			case "DURATION":
				return nil, fmt.Errorf("event %s: unsupported DURATION property; use DTEND", e.ID)
			case "EXDATE", "RDATE", "RECURRENCE-ID":
				return nil, fmt.Errorf("event %s: unsupported recurrence property %s", e.ID, property)
			}
		}
	}

	return events, nil
}

func parseICSDate(line string, defaultTZ *time.Location) (time.Time, bool, error) {
	property, value, ok := strings.Cut(line, ":")
	if !ok {
		return time.Time{}, false, fmt.Errorf("missing date value")
	}
	loc := defaultTZ
	allDay := false
	for _, param := range strings.Split(property, ";")[1:] {
		key, val, _ := strings.Cut(param, "=")
		val = strings.Trim(val, "\"")
		switch key {
		case "VALUE":
			allDay = val == "DATE"
		case "TZID":
			var err error
			loc, err = time.LoadLocation(val)
			if err != nil {
				return time.Time{}, false, err
			}
		}
	}
	layout := "20060102T150405"
	if allDay {
		layout, loc = "20060102", defaultTZ
	} else if strings.HasSuffix(value, "Z") {
		layout, loc = "20060102T150405Z", time.UTC
	}
	date, err := time.ParseInLocation(layout, value, loc)
	return date, allDay, err
}

// escapeICS escapes special characters for ICS fields
func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")

	return s
}

// unescapeICS reverses escapeICS for ICS fields
func unescapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}
