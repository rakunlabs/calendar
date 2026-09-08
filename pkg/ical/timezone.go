package ical

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
)

const timezoneLimit = 100000

const timezoneCacheLimit = 16

type timezoneCacheEntry struct {
	key      [sha256.Size]byte
	location *time.Location
}

type timezoneCache struct {
	sync.Mutex
	entries []timezoneCacheEntry // Least recently used first.
}

var compiledTimezones timezoneCache

func (c *timezoneCache) load(raw string) (*time.Location, error) {
	if len(raw) > 10*1024*1024 {
		return nil, fmt.Errorf("VTIMEZONE exceeds 10 MiB")
	}
	key := sha256.Sum256([]byte(raw))
	// Serialize misses as well as hits: concurrent resolutions must not all
	// expand the same rule. Only bounded compiled locations, not raw data or
	// expansion scratch space, survive in this cache. Errors are not cached.
	c.Lock()
	defer c.Unlock()
	for i, entry := range c.entries {
		if entry.key == key {
			copy(c.entries[i:], c.entries[i+1:])
			c.entries[len(c.entries)-1] = entry
			return entry.location, nil
		}
	}
	id, transitions, err := parseTimezoneDefinition(raw)
	if err != nil {
		return nil, err
	}
	location, err := timezoneLocation(strings.Clone(id), transitions)
	if err != nil {
		return nil, err
	}
	if len(c.entries) == timezoneCacheLimit {
		copy(c.entries, c.entries[1:])
		c.entries = c.entries[:len(c.entries)-1]
	}
	c.entries = append(c.entries, timezoneCacheEntry{key: key, location: location})
	return location, nil
}

type timezoneDefinition struct {
	raw      string
	location *time.Location
}

func resolveTimezoneDefinitions(definitions []string) ([]timezoneDefinition, error) {
	seenRaw := map[string]bool{}
	seenID := map[string]bool{}
	var result []timezoneDefinition
	for _, raw := range definitions {
		if seenRaw[raw] {
			continue
		}
		seenRaw[raw] = true
		location, err := compiledTimezones.load(raw)
		if err != nil {
			return nil, err
		}
		if seenID[location.String()] {
			return nil, fmt.Errorf("conflicting VTIMEZONE definitions for %q", location.String())
		}
		seenID[location.String()] = true
		result = append(result, timezoneDefinition{raw: raw, location: location})
	}
	return result, nil
}

// ValidateTimezoneDefinitions validates every supplied definition, including
// unreferenced ones, and rejects conflicting definitions of the same TZID.
// Identical raw definitions are deduplicated before compilation.
func ValidateTimezoneDefinitions(definitions []string) error {
	_, err := resolveTimezoneDefinitions(definitions)
	return err
}

type timezoneTransition struct {
	at       int64
	from, to int
	dst      bool
	name     string
}

type timezoneObservance struct {
	start      time.Time
	from, to   int
	dst        bool
	name, rule string
	dates      []time.Time
	seen       map[string]bool
}

// ResolveLocation prefers calendar-scoped VTIMEZONE data over the host database.
// Definitions are validated even when not selected. There is deliberately no
// name-only cache: two calendars may assign different rules to the same TZID.
// Successful compilations share a concurrency-safe, 16-entry content-keyed LRU;
// eviction only affects performance, never resolution semantics.
// Custom transitions cover civil years 1..9999; outside that range no guarantee
// is made. Unsupported observance rules fail rather than becoming fixed offsets.
// Each definition is limited to 10 MiB, 256 observances/types, 100000 transitions,
// 256 abbreviation bytes, and 20000000 date-filtering work units.
func ResolveLocation(name string, definitions []string) (*time.Location, error) {
	resolved, err := resolveTimezoneDefinitions(definitions)
	if err != nil {
		return nil, err
	}
	for _, definition := range resolved {
		if definition.location.String() == name {
			return definition.location, nil
		}
	}
	if name == "" {
		return time.UTC, nil
	}
	if name == "Local" {
		return nil, fmt.Errorf("timezone Local is not a portable TZID")
	}
	return time.LoadLocation(name)
}

func timezoneDate(value string) (time.Time, error) {
	t, err := time.Parse("20060102T150405", value)
	if err != nil || t.Year() < 1 || t.Year() > 9999 {
		return time.Time{}, fmt.Errorf("VTIMEZONE requires local DATE-TIME in years 1..9999: %q", value)
	}
	return t, nil
}

func timezoneOffset(value string) (int, error) {
	if (len(value) != 5 && len(value) != 7) || (value[0] != '+' && value[0] != '-') {
		return 0, fmt.Errorf("invalid UTC offset %q", value)
	}
	for _, c := range value[1:] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid UTC offset %q", value)
		}
	}
	h, _ := strconv.Atoi(value[1:3])
	m, _ := strconv.Atoi(value[3:5])
	s := 0
	if len(value) == 7 {
		s, _ = strconv.Atoi(value[5:])
	}
	if h > 23 || m > 59 || s > 59 || (value[0] == '-' && h+m+s == 0) {
		return 0, fmt.Errorf("invalid UTC offset %q", value)
	}
	n := h*3600 + m*60 + s
	if value[0] == '-' {
		n = -n
	}
	return n, nil
}

func parseTimezoneDefinition(raw string) (string, []timezoneTransition, error) {
	if len(raw) > 10*1024*1024 {
		return "", nil, fmt.Errorf("VTIMEZONE exceeds 10 MiB")
	}
	var lines []string
	// Unfold in linear time, including heavily folded untrusted definitions.
	unfolded := strings.NewReplacer("\n ", "", "\n\t", "").Replace(strings.ReplaceAll(raw, "\r\n", "\n"))
	for line := range strings.SplitSeq(unfolded, "\n") {
		if line != "" {
			lines = append(lines, line)
			if len(lines) > 200000 {
				return "", nil, fmt.Errorf("VTIMEZONE exceeds 200000 content lines")
			}
		}
	}
	fail := func() (string, []timezoneTransition, error) {
		return "", nil, fmt.Errorf("invalid or unsupported VTIMEZONE definition")
	}
	if len(lines) < 3 || !strings.EqualFold(lines[0], "BEGIN:VTIMEZONE") || !strings.EqualFold(lines[len(lines)-1], "END:VTIMEZONE") {
		return fail()
	}
	var id string
	var obs *timezoneObservance
	var transitions []timezoneTransition
	observances := 0
	work := 20000000
	for _, line := range lines[1 : len(lines)-1] {
		header, value, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(value, "\r\x00") {
			return fail()
		}
		key, param, _ := strings.Cut(header, ";")
		key = strings.ToUpper(key)
		if key == "BEGIN" {
			value = strings.ToUpper(value)
			if obs != nil || param != "" || (value != "STANDARD" && value != "DAYLIGHT") {
				return fail()
			}
			observances++
			if observances > 256 {
				return "", nil, fmt.Errorf("VTIMEZONE exceeds 256 observances")
			}
			obs = &timezoneObservance{dst: value == "DAYLIGHT", seen: map[string]bool{}}
			continue
		}
		if key == "END" {
			value = strings.ToUpper(value)
			if obs == nil || param != "" || (obs.dst && value != "DAYLIGHT") || (!obs.dst && value != "STANDARD") {
				return fail()
			}
			if !obs.seen["DTSTART"] || !obs.seen["TZOFFSETFROM"] || !obs.seen["TZOFFSETTO"] {
				return fail()
			}
			added, err := expandTimezoneObservance(*obs, &work)
			if err != nil {
				return "", nil, err
			}
			transitions = append(transitions, added...)
			if len(transitions) > timezoneLimit {
				return "", nil, fmt.Errorf("VTIMEZONE exceeds %d transitions", timezoneLimit)
			}
			obs = nil
			continue
		}
		if obs == nil {
			switch key {
			case "TZID":
				if id != "" || value == "" || param != "" {
					return fail()
				}
				id = value
			case "LAST-MODIFIED", "TZURL", "COMMENT":
			default:
				if !strings.HasPrefix(key, "X-") {
					return fail()
				}
			}
			continue
		}
		if key != "RDATE" && key != "COMMENT" && key != "TZNAME" && obs.seen[key] {
			return fail()
		}
		obs.seen[key] = true
		var err error
		switch key {
		case "DTSTART":
			if param != "" && !strings.EqualFold(param, "VALUE=DATE-TIME") {
				return fail()
			}
			obs.start, err = timezoneDate(value)
		case "TZOFFSETFROM", "TZOFFSETTO":
			if param != "" {
				return fail()
			}
			var offset int
			offset, err = timezoneOffset(value)
			if key == "TZOFFSETFROM" {
				obs.from = offset
			} else {
				obs.to = offset
			}
		case "TZNAME":
			obs.name = unescapeICS(value)
		case "RRULE":
			if param != "" {
				return fail()
			}
			obs.rule = value
		case "RDATE":
			if param != "" && !strings.EqualFold(param, "VALUE=DATE-TIME") {
				return fail()
			}
			for _, date := range strings.Split(value, ",") {
				t, e := timezoneDate(date)
				if e != nil {
					return "", nil, e
				}
				obs.dates = append(obs.dates, t)
				if len(obs.dates) > timezoneLimit {
					return "", nil, fmt.Errorf("too many VTIMEZONE RDATEs")
				}
			}
		case "COMMENT":
		default:
			if !strings.HasPrefix(key, "X-") {
				return fail()
			}
		}
		if err != nil {
			return "", nil, err
		}
	}
	if obs != nil || id == "" || len(transitions) == 0 {
		return fail()
	}
	slices.SortFunc(transitions, func(a, b timezoneTransition) int {
		if a.at < b.at {
			return -1
		}
		if a.at > b.at {
			return 1
		}
		return 0
	})
	unique := transitions[:0]
	for _, t := range transitions {
		if len(unique) > 0 && unique[len(unique)-1].at == t.at {
			p := unique[len(unique)-1]
			if p.from != t.from || p.to != t.to || p.dst != t.dst || p.name != t.name {
				return "", nil, fmt.Errorf("conflicting VTIMEZONE transitions")
			}
			continue
		}
		if len(unique) > 0 && unique[len(unique)-1].to != t.from {
			return "", nil, fmt.Errorf("VTIMEZONE TZOFFSETFROM disagrees with preceding transition")
		}
		unique = append(unique, t)
	}
	return id, unique, nil
}

func expandTimezoneObservance(o timezoneObservance, work *int) ([]timezoneTransition, error) {
	dates := append([]time.Time{o.start}, o.dates...)
	if o.rule != "" {
		r, err := ParseRRule(o.rule)
		if err != nil {
			return nil, err
		}
		if r.Freq != "YEARLY" || len(r.ByWeekNo)+len(r.ByYearDay)+len(r.ByHour)+len(r.ByMinute)+len(r.BySecond) != 0 {
			return nil, fmt.Errorf("unsupported VTIMEZONE RRULE: require YEARLY with BYMONTH/BYMONTHDAY/BYDAY/BYSETPOS date selectors")
		}
		if r.Until != nil && (r.UntilType != "UTC" || r.Until.Year() < 1 || r.Until.Year() > 9999) {
			return nil, fmt.Errorf("VTIMEZONE RRULE UNTIL must be UTC in years 1..9999")
		}
		count := 0
		for year := o.start.Year(); year <= 9999; year += max(1, r.Interval) {
			if r.Until != nil && time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(o.from)*time.Second).After(*r.Until) {
				break
			}
			var candidates []time.Time
			for day := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC); day.Year() == year; day = day.AddDate(0, 0, 1) {
				*work -= max(1, (len(r.ByMonth)+len(r.ByMonthDay)+len(r.ByDay)+len(r.BySetPos)+31)/32)
				if *work < 0 {
					return nil, fmt.Errorf("VTIMEZONE exceeds expansion work limit (20000000)")
				}
				if matchRRuleDate(r, day, o.start, parseWkst(r.Wkst)) {
					candidates = append(candidates, day.Add(time.Duration(o.start.Hour()*3600+o.start.Minute()*60+o.start.Second())*time.Second))
				}
			}
			if len(r.BySetPos) > 0 {
				var selected []time.Time
				for _, pos := range r.BySetPos {
					i := pos - 1
					if pos < 0 {
						i = len(candidates) + pos
					}
					if i >= 0 && i < len(candidates) {
						selected = append(selected, candidates[i])
					}
				}
				slices.SortFunc(selected, time.Time.Compare)
				candidates = slices.CompactFunc(selected, time.Time.Equal)
			}
			stop := false
			for _, t := range candidates {
				if t.Before(o.start) {
					continue
				}
				if r.Until != nil && t.Add(-time.Duration(o.from)*time.Second).After(*r.Until) {
					stop = true
					break
				}
				if r.Count != nil && count >= *r.Count {
					stop = true
					break
				}
				count++
				dates = append(dates, t)
				if len(dates) > timezoneLimit {
					return nil, fmt.Errorf("VTIMEZONE exceeds transition limit")
				}
			}
			if stop {
				break
			}
		}
	}
	result := make([]timezoneTransition, 0, len(dates))
	for _, t := range dates {
		result = append(result, timezoneTransition{at: t.Unix() - int64(o.from), from: o.from, to: o.to, dst: o.dst, name: o.name})
	}
	return result, nil
}

func timezoneLocation(id string, transitions []timezoneTransition) (*time.Location, error) {
	type zone struct {
		offset int
		dst    bool
		name   string
	}
	zones := []zone{{offset: transitions[0].from, name: "LMT"}}
	indices := make([]byte, len(transitions))
	for i, t := range transitions {
		z := zone{t.to, t.dst, t.name}
		index := slices.Index(zones, z)
		if index < 0 {
			index = len(zones)
			zones = append(zones, z)
		}
		if len(zones) > 256 {
			return nil, fmt.Errorf("VTIMEZONE exceeds 256 zone types")
		}
		indices[i] = byte(index)
	}
	var abbreviations []byte
	nameIndices := make([]byte, len(zones))
	abbreviationIndices := map[string]byte{}
	for i, z := range zones {
		if index, ok := abbreviationIndices[z.name]; ok {
			nameIndices[i] = index
			continue
		}
		if len(abbreviations)+len(z.name)+1 > 256 {
			return nil, fmt.Errorf("VTIMEZONE abbreviations exceed 256 bytes")
		}
		nameIndices[i] = byte(len(abbreviations))
		abbreviationIndices[z.name] = nameIndices[i]
		abbreviations = append(abbreviations, []byte(z.name)...)
		abbreviations = append(abbreviations, 0)
	}
	var b bytes.Buffer
	write := func(v any) { _ = binary.Write(&b, binary.BigEndian, v) }
	header := func(count int) {
		b.WriteString("TZif2")
		b.Write(make([]byte, 15))
		for _, n := range []int{0, 0, 0, count, len(zones), len(abbreviations)} {
			write(uint32(n))
		}
	}
	types := func() {
		for i, z := range zones {
			write(int32(z.offset))
			if z.dst {
				b.WriteByte(1)
			} else {
				b.WriteByte(0)
			}
			b.WriteByte(nameIndices[i])
		}
		b.Write(abbreviations)
	}
	header(0)
	types()
	header(len(transitions))
	for _, t := range transitions {
		write(t.at)
	}
	b.Write(indices)
	types()
	b.WriteString("\n\n")
	return time.LoadLocationFromTZData(id, b.Bytes())
}

// GenerateVTimezones preserves validated raw definitions verbatim and exports
// missing IANA zones as exact grouped RDATE transitions for years 1..9999.
// This deliberately favors bounded exactness over inferred future RRULEs. A DST
// zone is typically a few hundred KiB, independent of the event's query window.
// Overrides inherit their master's definitions. Mixing a custom definition with
// an event using the same TZID from IANA is rejected, not silently reinterpreted.
func GenerateVTimezones(events []models.Event) ([]string, error) {
	rawByID := map[string]string{}
	names := map[string]bool{}
	ianaNames := map[string]bool{}
	var result []string
	var visit func(models.Event, []string, int) error
	visit = func(e models.Event, inherited []string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("timezone override nesting exceeds 32")
		}
		if e.Recurrence == nil {
			e.Recurrence = &models.Recurrence{}
		}
		definitions := append(slices.Clone(inherited), e.Recurrence.Timezones...)
		resolved, err := resolveTimezoneDefinitions(definitions)
		if err != nil {
			return err
		}
		scope := map[string]bool{}
		for _, definition := range resolved {
			id, raw := definition.location.String(), definition.raw
			scope[id] = true
			if previous, ok := rawByID[id]; ok {
				if previous != raw {
					return fmt.Errorf("conflicting VTIMEZONE definitions for %q", id)
				}
			} else {
				rawByID[id] = raw
				result = append(result, raw)
			}
		}
		addName := func(name string) {
			if name == "" {
				return
			}
			names[name] = true
			if !scope[name] {
				ianaNames[name] = true
			}
		}
		add := func(d *models.CalendarDate) {
			if d != nil && d.TZID != "" {
				addName(d.TZID)
			}
		}
		if e.Tz != "" {
			addName(e.Tz)
		} else if !e.AllDay {
			addName(e.DateFrom.Time.Location().String())
			addName(e.DateTo.Time.Location().String())
		}
		add(e.Recurrence.Start)
		add(e.Recurrence.End)
		for _, d := range e.Recurrence.ExDates {
			add(&d)
		}
		for _, p := range e.Recurrence.RDates {
			add(&p.Start)
			add(p.End)
		}
		for _, o := range e.Recurrence.Overrides {
			add(&o.RecurrenceID)
			if o.Event != nil {
				if err := visit(*o.Event, definitions, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, e := range events {
		if err := visit(e, nil, 0); err != nil {
			return nil, err
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	slices.Sort(sorted)
	for _, name := range sorted {
		if _, ok := rawByID[name]; ok {
			if ianaNames[name] {
				return nil, fmt.Errorf("timezone scope collision for %q: custom VTIMEZONE would override another event's IANA timezone", name)
			}
			continue
		}
		if name == "UTC" || name == "" {
			continue
		}
		loc, err := ResolveLocation(name, nil)
		if err != nil {
			return nil, err
		}
		raw, err := exportTimezone(name, loc)
		if err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, nil
}

func exportTimezone(id string, loc *time.Location) (string, error) {
	if strings.ContainsAny(id, "\r\n\x00") {
		return "", fmt.Errorf("invalid TZID")
	}
	type group struct {
		from, to int
		dst      bool
		name     string
		dates    []string
	}
	var groups []group
	add := func(t time.Time, from, to int, dst bool) error {
		wall := t.UTC().Add(time.Duration(from) * time.Second)
		if wall.Year() < 1 || wall.Year() > 9999 {
			return fmt.Errorf("timezone transition outside civil years 1..9999")
		}
		name, _ := t.In(loc).Zone()
		i := slices.IndexFunc(groups, func(g group) bool { return g.from == from && g.to == to && g.dst == dst && g.name == name })
		if i < 0 {
			i = len(groups)
			groups = append(groups, group{from: from, to: to, dst: dst, name: name})
		}
		groups[i].dates = append(groups[i].dates, wall.Format("20060102T150405"))
		return nil
	}
	start := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	_, initial := start.In(loc).Zone()
	if err := add(start.Add(-time.Duration(initial)*time.Second), initial, initial, start.In(loc).IsDST()); err != nil {
		return "", err
	}
	end := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	cursor := start.In(loc)
	for work := 0; cursor.Before(end); work++ {
		if work > 1000000 {
			return "", fmt.Errorf("timezone export exceeds work limit")
		}
		_, next := cursor.ZoneBounds()
		if next.IsZero() {
			break
		}
		if !next.After(cursor) {
			// time.tzset returns January 1 + 365 days after the year's last
			// POSIX transition, including in leap years. No transitions
			// remain in this UTC year; resume at the next year's boundary.
			year := cursor.UTC().Year()
			if !next.Equal(time.Date(year, 1, 366, 0, 0, 0, 0, time.UTC)) {
				return "", fmt.Errorf("unsupported non-advancing timezone bounds for %q", id)
			}
			next = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC).In(loc)
		}
		if !next.Before(end) {
			break
		}
		fromName, from := next.Add(-time.Second).Zone()
		toName, to := next.Zone()
		if from != to || fromName != toName || next.Add(-time.Second).IsDST() != next.IsDST() {
			if err := add(next, from, to, next.IsDST()); err != nil {
				return "", err
			}
		}
		cursor = next
	}
	formatOffset := func(n int) string {
		sign := '+'
		if n < 0 {
			sign = '-'
			n = -n
		}
		if n%60 != 0 {
			return fmt.Sprintf("%c%02d%02d%02d", sign, n/3600, n/60%60, n%60)
		}
		return fmt.Sprintf("%c%02d%02d", sign, n/3600, n/60%60)
	}
	var b strings.Builder
	b.WriteString("BEGIN:VTIMEZONE\r\nTZID:" + id + "\r\n")
	for _, g := range groups {
		kind := "STANDARD"
		if g.dst {
			kind = "DAYLIGHT"
		}
		fmt.Fprintf(&b, "BEGIN:%s\r\nDTSTART:%s\r\nTZOFFSETFROM:%s\r\nTZOFFSETTO:%s\r\n", kind, g.dates[0], formatOffset(g.from), formatOffset(g.to))
		b.WriteString("TZNAME:" + escapeICS(g.name) + "\r\n")
		// Four dates per content line avoids both folding and oversized lines.
		for i := 1; i < len(g.dates); i += 4 {
			b.WriteString("RDATE:" + strings.Join(g.dates[i:min(i+4, len(g.dates))], ",") + "\r\n")
		}
		b.WriteString("END:" + kind + "\r\n")
	}
	b.WriteString("END:VTIMEZONE\r\n")
	return b.String(), nil
}
