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

// GenerateICS emits masters and detached instances with a shared UID.
func GenerateICS(events []models.Event, category string) (string, error) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//rakunlabs//calendar//EN\r\n")
	definitions, err := GenerateVTimezones(events)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		definition = strings.TrimSpace(definition)
		if !seen[definition] {
			seen[definition] = true
			b.WriteString(definition)
			b.WriteString("\r\n")
		}
	}
	if category == "" {
		category = "Holidays"
	}
	writeDate := func(name string, d models.CalendarDate) {
		b.WriteString(name)
		if d.Type != "" {
			b.WriteString(";VALUE=" + d.Type)
		}
		if d.TZID != "" {
			if strings.ContainsAny(d.TZID, ":;,") {
				b.WriteString(";TZID=\"" + d.TZID + "\"")
			} else {
				b.WriteString(";TZID=" + d.TZID)
			}
		}
		b.WriteString(":" + d.Value + "\r\n")
	}
	var writeEvent func(models.Event, string, *models.CalendarDate, bool) error
	writeEvent = func(e models.Event, uid string, id *models.CalendarDate, cancelled bool) error {
		b.WriteString("BEGIN:VEVENT\r\nUID:" + uid + "\r\n")
		if id != nil {
			writeDate("RECURRENCE-ID", *id)
		}
		if cancelled {
			b.WriteString("STATUS:CANCELLED\r\nEND:VEVENT\r\n")
			return nil
		}
		b.WriteString("CATEGORIES:" + escapeICS(category) + "\r\nCLASS:PUBLIC\r\nSUMMARY:" + escapeICS(e.Name) + "\r\n")
		if e.Description != "" {
			b.WriteString("DESCRIPTION:" + escapeICS(e.Description) + "\r\n")
		}
		a, z, _, err := eventTiming(e, definitions)
		if err != nil {
			return err
		}
		start, end := CalendarDateFromTime(a, e.AllDay), CalendarDateFromTime(z, e.AllDay)
		r := e.Recurrence
		if r != nil {
			if r.Start != nil {
				start = *r.Start
			}
			if r.End != nil {
				end = *r.End
			}
		}
		writeDate("DTSTART", start)
		if r != nil && r.Duration != "" {
			b.WriteString("DURATION:" + r.Duration + "\r\n")
		} else {
			writeDate("DTEND", end)
		}
		if rule := strings.TrimSpace(e.RRule); rule != "" {
			if property, value, ok := strings.Cut(rule, ":"); ok && strings.EqualFold(property, "RRULE") {
				rule = value
			}
			if _, err := ParseRRule(rule); err != nil {
				return fmt.Errorf("event %s recurrence must be a single RRULE (materialize FUNC before export): %w", e.ID, err)
			}
			b.WriteString("RRULE:" + rule + "\r\n")
		}
		if r != nil {
			for _, d := range r.ExDates {
				writeDate("EXDATE", d)
			}
			for _, p := range r.RDates {
				d := p.Start
				if p.End != nil {
					d.Type = "PERIOD"
					d.Value += "/" + p.End.Value
				} else if p.Duration != "" {
					d.Type = "PERIOD"
					d.Value += "/" + p.Duration
				}
				writeDate("RDATE", d)
			}
		}
		b.WriteString("TRANSP:TRANSPARENT\r\nEND:VEVENT\r\n")
		return nil
	}
	for _, e := range events {
		if err := ValidateEvent(e); err != nil {
			return "", err
		}
		if err := writeEvent(e, e.ID, nil, false); err != nil {
			return "", err
		}
		if e.Recurrence != nil {
			for _, o := range e.Recurrence.Overrides {
				detached := models.Event{}
				if o.Event != nil {
					detached = *o.Event
				}
				if err := writeEvent(detached, e.ID, &o.RecurrenceID, o.Cancelled); err != nil {
					return "", err
				}
			}
		}
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String(), nil
}

// ParseICS collects timezone definitions before resolving any event and groups
// detached components after parsing, so component order does not affect identity.
func ParseICS(data io.Reader, tz *time.Location) ([]models.Event, error) {
	if tz == nil {
		tz = time.UTC
	}
	scanner := bufio.NewScanner(data)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(lines) == 0 {
				return nil, fmt.Errorf("orphan folded line")
			}
			lines[len(lines)-1] += line[1:]
			if len(lines[len(lines)-1]) > 1024*1024 {
				return nil, fmt.Errorf("unfolded ICS line exceeds limit")
			}
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read ics: %w", err)
	}
	var definitions []string
	var components [][]string
	var stack []string
	var eventLines []string
	zoneStart := -1
	for i, line := range lines {
		if line == "" {
			continue
		}
		header, value, ok := cutICS(line, ':')
		if !ok {
			return nil, fmt.Errorf("malformed ICS content line")
		}
		name, _, _ := strings.Cut(header, ";")
		name = strings.ToUpper(name)
		if name == "BEGIN" {
			component := strings.ToUpper(value)
			if component == "VEVENT" {
				if len(stack) > 0 && stack[len(stack)-1] != "VCALENDAR" {
					return nil, fmt.Errorf("invalid nested VEVENT")
				}
				eventLines = nil
			}
			if component == "VTIMEZONE" {
				if zoneStart >= 0 {
					return nil, fmt.Errorf("nested VTIMEZONE")
				}
				zoneStart = i
			}
			stack = append(stack, component)
			continue
		}
		if name == "END" {
			if len(stack) == 0 || stack[len(stack)-1] != strings.ToUpper(value) {
				return nil, fmt.Errorf("unbalanced ICS component")
			}
			if strings.EqualFold(value, "VEVENT") {
				components = append(components, eventLines)
				eventLines = nil
			}
			if strings.EqualFold(value, "VTIMEZONE") {
				definitions = append(definitions, strings.Join(lines[zoneStart:i+1], "\r\n"))
				zoneStart = -1
			}
			stack = stack[:len(stack)-1]
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1] == "VEVENT" {
			eventLines = append(eventLines, line)
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unterminated ICS component")
	}
	type parsed struct {
		event     models.Event
		id        *models.CalendarDate
		cancelled bool
	}
	var parsedEvents []parsed
	for _, component := range components {
		p := parsed{event: models.Event{Recurrence: &models.Recurrence{Timezones: definitions}}}
		e := &p.event
		r := e.Recurrence
		seen := map[string]bool{}
		for _, line := range component {
			header, value, _ := cutICS(line, ':')
			name, _, _ := strings.Cut(header, ";")
			name = strings.ToUpper(name)
			switch name {
			case "DTSTART", "DTEND", "DURATION", "RRULE", "RECURRENCE-ID", "UID", "STATUS":
				if seen[name] {
					return nil, fmt.Errorf("event %s has multiple %s properties", e.ID, name)
				}
				seen[name] = true
			}
			switch name {
			case "UID":
				e.ID = value
			case "SUMMARY":
				e.Name = unescapeICS(value)
			case "DESCRIPTION":
				e.Description = unescapeICS(value)
			case "STATUS":
				p.cancelled = strings.EqualFold(value, "CANCELLED")
			case "RRULE":
				if _, err := ParseRRule(value); err != nil {
					return nil, err
				}
				e.RRule = "RRULE:" + value
			case "DURATION":
				r.Duration = value
			case "DTSTART", "DTEND", "RECURRENCE-ID", "EXDATE", "RDATE":
				d, err := calendarDateProperty(header, value)
				if err != nil {
					return nil, err
				}
				switch name {
				case "DTSTART":
					r.Start = &d
					e.AllDay = d.Type == "DATE"
				case "DTEND":
					r.End = &d
				case "RECURRENCE-ID":
					p.id = &d
				case "EXDATE":
					for _, v := range strings.Split(value, ",") {
						c := d
						c.Value = v
						r.ExDates = append(r.ExDates, c)
					}
				case "RDATE":
					for _, v := range strings.Split(value, ",") {
						c := d
						c.Value = v
						period := models.RecurrencePeriod{Start: c}
						if d.Type == "PERIOD" {
							a, b, ok := strings.Cut(v, "/")
							if !ok {
								return nil, fmt.Errorf("RDATE PERIOD requires start/end or start/duration")
							}
							period.Start.Type = ""
							period.Start.Value = a
							if strings.HasPrefix(b, "P") || strings.HasPrefix(b, "+") || strings.HasPrefix(b, "-") {
								period.Duration = b
							} else {
								end := period.Start
								end.Value = b
								period.End = &end
							}
						}
						r.RDates = append(r.RDates, period)
					}
				}
			}
		}
		if !p.cancelled || p.id == nil || r.Start != nil {
			if r.Start == nil {
				return nil, fmt.Errorf("event %s has missing or invalid DTSTART", e.ID)
			}
			a, err := ResolveCalendarDate(*r.Start, tz, definitions)
			if err != nil {
				return nil, err
			}
			e.Tz = a.Location().String()
			e.DateFrom = types.Time{Time: a}
			_, b, _, err := eventTiming(*e, definitions)
			if err != nil {
				return nil, err
			}
			e.DateTo = types.Time{Time: b}
		}
		parsedEvents = append(parsedEvents, p)
	}
	var events []models.Event
	masters := map[string]int{}
	for _, p := range parsedEvents {
		if p.id == nil {
			if _, ok := masters[p.event.ID]; ok && p.event.ID != "" {
				return nil, fmt.Errorf("duplicate master UID %s", p.event.ID)
			}
			masters[p.event.ID] = len(events)
			events = append(events, p.event)
		}
	}
	for _, p := range parsedEvents {
		if p.id == nil {
			continue
		}
		i, ok := masters[p.event.ID]
		if !ok || p.event.ID == "" {
			return nil, fmt.Errorf("RECURRENCE-ID has no master UID %s", p.event.ID)
		}
		o := models.OccurrenceOverride{RecurrenceID: *p.id, Cancelled: p.cancelled}
		if p.event.Recurrence.Start != nil {
			copy := p.event
			o.Event = &copy
		}
		events[i].Recurrence.Overrides = append(events[i].Recurrence.Overrides, o)
	}
	for _, e := range events {
		if err := ValidateEvent(e); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func calendarDateProperty(header, value string) (models.CalendarDate, error) {
	d := models.CalendarDate{Value: value}
	_, params, _ := strings.Cut(header, ";")
	for params != "" {
		var param string
		param, params, _ = cutICS(params, ';')
		key, val, ok := strings.Cut(param, "=")
		if !ok {
			return d, fmt.Errorf("invalid date parameter")
		}
		val = strings.Trim(val, "\"")
		switch strings.ToUpper(key) {
		case "VALUE":
			d.Type = strings.ToUpper(val)
		case "TZID":
			d.TZID = val
		case "RANGE":
			return d, fmt.Errorf("unsupported RECURRENCE-ID RANGE=%s; range overrides are not supported", val)
		}
	}
	return d, nil
}

// cutICS ignores delimiters inside quoted parameter values.
func cutICS(s string, delimiter byte) (string, string, bool) {
	quoted := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			quoted = !quoted
		} else if s[i] == delimiter && !quoted {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func parseICSDate(line string, defaultTZ *time.Location) (time.Time, bool, error) {
	header, value, ok := cutICS(line, ':')
	if !ok {
		return time.Time{}, false, fmt.Errorf("missing date value")
	}
	d, err := calendarDateProperty(header, value)
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := ResolveCalendarDate(d, defaultTZ, nil)
	return t, d.Type == "DATE", err
}

func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func unescapeICS(s string) string {
	return strings.NewReplacer("\\n", "\n", "\\N", "\n", "\\,", ",", "\\;", ";", "\\\\", "\\").Replace(s)
}
