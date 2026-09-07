package ical

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
	"github.com/worldline-go/types"
)

func TestGenerateICS(t *testing.T) {
	tzIstanbul, _ := time.LoadLocation("Europe/Istanbul")
	type args struct {
		events []models.Event
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "23 Nisan",
			args: args{
				events: []models.Event{
					{
						Name:        "23 Nisan Ulusal Egemenlik ve Çocuk Bayramı",
						Description: "23 Nisan Ulusal Egemenlik ve Çocuk Bayramı",
						DateFrom:    types.Time{Time: time.Date(2023, 4, 23, 0, 0, 0, 0, tzIstanbul)},
						DateTo:      types.Time{Time: time.Date(2023, 4, 24, 0, 0, 0, 0, tzIstanbul)},
						RRule:       "FREQ=YEARLY;BYMONTH=4;BYMONTHDAY=23",
						AllDay:      true,
						Disabled:    false,
						UpdatedAt:   types.Time{Time: time.Now()},
						UpdatedBy:   "system",
					},
				},
			},
			want: "BEGIN:VCALENDAR\r\n" +
				"VERSION:2.0\r\n" +
				"PRODID:-//worldline-go//calendar//EN\r\n" +
				"BEGIN:VEVENT\r\n" +
				"UID:\r\n" +
				"CATEGORIES:Holidays\r\n" +
				"CLASS:PUBLIC\r\n" +
				"SUMMARY:23 Nisan Ulusal Egemenlik ve Çocuk Bayramı\r\n" +
				"DESCRIPTION:23 Nisan Ulusal Egemenlik ve Çocuk Bayramı\r\n" +
				"DTSTART;VALUE=DATE:20230423\r\n" +
				"DTEND;VALUE=DATE:20230424\r\n" +
				"RRULE:FREQ=YEARLY;BYMONTH=4;BYMONTHDAY=23\r\n" +
				"TRANSP:TRANSPARENT\r\n" +
				"END:VEVENT\r\n" +
				"END:VCALENDAR\r\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateICS(tt.args.events, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateICS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateICS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestICSRoundTripRecurrenceAndTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, tt := range []struct {
		name       string
		allDay     bool
		start, end time.Time
	}{
		{"timed TZID", false, time.Date(2026, 3, 7, 9, 30, 0, 0, loc), time.Date(2026, 3, 7, 10, 30, 0, 0, loc)},
		{"all day DST", true, time.Date(2026, 3, 8, 0, 0, 0, 0, loc), time.Date(2026, 3, 9, 0, 0, 0, 0, loc)},
		{"multi day", true, time.Date(2026, 3, 7, 0, 0, 0, 0, loc), time.Date(2026, 3, 10, 0, 0, 0, 0, loc)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			event := models.Event{ID: "roundtrip", Name: "Meeting, notes; more", AllDay: tt.allDay, Tz: loc.String(),
				DateFrom: types.Time{Time: tt.start.UTC()}, DateTo: types.Time{Time: tt.end.UTC()},
				RRule: "RRULE:FREQ=DAILY;COUNT=3"}
			data, err := GenerateICS([]models.Event{event}, "")
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(data, "RRULE:"))
			if tt.allDay {
				require.Contains(t, data, "DTSTART;VALUE=DATE:")
			} else {
				require.Contains(t, data, "DTSTART;TZID=America/New_York:20260307T093000")
			}
			// DATE values are timezone-free, so their import timezone must be supplied.
			importTZ := time.UTC
			if tt.allDay {
				importTZ = loc
			}
			got, err := ParseICS(strings.NewReader(data), importTZ)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, event.Name, got[0].Name)
			require.Equal(t, event.ID, got[0].ID)
			require.Equal(t, event.RRule, got[0].RRule)
			require.Equal(t, event.AllDay, got[0].AllDay)
			require.Equal(t, event.Tz, got[0].Tz)
			require.True(t, tt.start.Equal(got[0].DateFrom.Time))
			require.True(t, tt.end.Equal(got[0].DateTo.Time))
			second, err := GenerateICS(got, "")
			require.NoError(t, err)
			require.Equal(t, data, second)
		})
	}
}

func TestParseICSTimeFormatsAndFoldedRecurrence(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Istanbul")
	require.NoError(t, err)
	for _, tt := range []struct {
		name, start, end, tz string
		allDay               bool
		hour                 int
	}{
		{"UTC ignores default timezone", "DTSTART:20260101T090000Z", "DTEND:20260101T100000Z", "UTC", false, 9},
		{"floating", "DTSTART:20260101T090000", "DTEND:20260101T100000", loc.String(), false, 9},
		{"DATE-TIME is not DATE", "DTSTART;VALUE=DATE-TIME:20260101T090000Z", "DTEND;VALUE=DATE-TIME:20260101T100000Z", "UTC", false, 9},
		{"TZID parameters", "DTSTART;TZID=America/New_York;VALUE=DATE-TIME:20260101T090000", "DTEND;TZID=America/New_York;VALUE=DATE-TIME:20260101T100000", "America/New_York", false, 9},
		{"DATE missing end", "DTSTART;VALUE=DATE:20260101", "", loc.String(), true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := "BEGIN:VEVENT\r\nUID:test\r\n" + tt.start + "\r\n" + tt.end + "\r\nRRULE:FREQ=DAILY;\r\n\tCOUNT=2\r\nEND:VEVENT"
			got, err := ParseICS(strings.NewReader(data), loc)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, tt.tz, got[0].Tz)
			require.Equal(t, tt.allDay, got[0].AllDay)
			require.Equal(t, tt.hour, got[0].DateFrom.Hour())
			require.Equal(t, "RRULE:FREQ=DAILY;COUNT=2", got[0].RRule)
			if tt.end == "" {
				want := got[0].DateFrom.Time
				if tt.allDay {
					want = want.AddDate(0, 0, 1)
				}
				require.Equal(t, want, got[0].DateTo.Time)
			}
		})
	}
}

func TestParseICSRejectsInvalidDuration(t *testing.T) {
	for _, tt := range []struct {
		name, dates, want string
	}{
		{"timed missing end", "DTSTART:20260101T090000Z", "timed event requires DTEND"},
		{"timed zero duration", "DTSTART:20260101T090000Z\r\nDTEND:20260101T090000Z", "DTEND must be after DTSTART"},
		{"timed negative duration", "DTSTART:20260101T090000Z\r\nDTEND:20260101T080000Z", "DTEND must be after DTSTART"},
		{"all day zero duration", "DTSTART;VALUE=DATE:20260101\r\nDTEND;VALUE=DATE:20260101", "DTEND must be after DTSTART"},
		{"all day negative duration", "DTSTART;VALUE=DATE:20260102\r\nDTEND;VALUE=DATE:20260101", "DTEND must be after DTSTART"},
		{"timed DURATION", "DTSTART:20260101T090000Z\r\nDURATION:PT1H", "unsupported DURATION"},
		{"all day DURATION", "DTSTART;VALUE=DATE:20260101\r\nDURATION:P2D", "unsupported DURATION"},
		{"DURATION with end", "DTSTART:20260101T090000Z\r\nDTEND:20260101T100000Z\r\nDURATION:PT1H", "unsupported DURATION"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := "BEGIN:VEVENT\r\nUID:invalid\r\n" + tt.dates + "\r\nEND:VEVENT"
			events, err := ParseICS(strings.NewReader(data), nil)
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, events)
		})
	}
}

func TestGenerateICSRejectsUnmaterializedRecurrence(t *testing.T) {
	for _, rule := range []string{"FUNC:GoodFriday", "RRULE:FREQ=YEARLY FUNC:GoodFriday", "RRULE:FREQ=DAILY RRULE:FREQ=YEARLY"} {
		_, err := GenerateICS([]models.Event{{RRule: rule}}, "")
		require.Error(t, err)
	}
}

func TestParseICSRejectsInvalidOrLossyRecurrence(t *testing.T) {
	for _, property := range []string{
		"DTEND:invalid",
		"DTEND;TZID=Invalid/Zone:20260101T100000",
		"RRULE:FREQ=DAILY;COUNT=0",
		"RRULE:FREQ=DAILY\r\nRRULE:FREQ=YEARLY",
		"EXDATE:20260102T090000Z",
		"RDATE;VALUE=DATE:20260103",
		"RECURRENCE-ID:20260101T090000Z",
	} {
		data := "BEGIN:VEVENT\r\nDTSTART:20260101T090000Z\r\n" + property + "\r\nEND:VEVENT"
		_, err := ParseICS(strings.NewReader(data), nil)
		require.Error(t, err, property)
	}
}

func TestParseICS(t *testing.T) {
	tzIstanbul, _ := time.LoadLocation("Europe/Istanbul")
	type args struct {
		data []byte
		tz   string
	}
	tests := []struct {
		name    string
		args    args
		want    []models.Event
		wantErr bool
	}{
		{
			name: "23 Nisan",
			args: args{
				data: []byte(`
BEGIN:VEVENT
SUMMARY:Atatürk'ü Anma\, Gençlik ve Spor Günü
DTSTART;VALUE=DATE:20240519
DTEND;VALUE=DATE:20240520
DTSTAMP:20241008T090751Z
UID:f6d4e8a07317c9779f0fa9ea3152f722-2024
CATEGORIES:Holidays
CLASS:public
DESCRIPTION:National holiday -  Türkiye'de pek çok kişi her yıl 19 May
 ıs'ta Atatürk Anma\, Gençlik ve Spor Günü'nü spor etkinliklerine kat
 ılarak ve bu gün 1919 yılında başlayan Kurtuluş Savaşı'nı hatırl
 ayarak kutlamaktadır.
LAST-MODIFIED:20241008T090751Z
TRANSP:transparent
END:VEVENT
`),
				tz: "Europe/Istanbul",
			},
			want: []models.Event{
				{
					ID:          "f6d4e8a07317c9779f0fa9ea3152f722-2024",
					Name:        "Atatürk'ü Anma, Gençlik ve Spor Günü",
					Description: "National holiday -  Türkiye'de pek çok kişi her yıl 19 Mayıs'ta Atatürk Anma, Gençlik ve Spor Günü'nü spor etkinliklerine katılarak ve bu gün 1919 yılında başlayan Kurtuluş Savaşı'nı hatırlayarak kutlamaktadır.",
					DateFrom:    types.Time{Time: time.Date(2024, 5, 19, 0, 0, 0, 0, tzIstanbul)},
					DateTo:      types.Time{Time: time.Date(2024, 5, 20, 0, 0, 0, 0, tzIstanbul)},
					Tz:          "Europe/Istanbul",
					AllDay:      true,
					RRule:       "",
					Disabled:    false,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := time.LoadLocation(tt.args.tz)
			if err != nil {
				t.Fatalf("time.LoadLocation() error = %v", err)
			}

			got, err := ParseICS(bytes.NewReader(tt.args.data), tz)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseICS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseICS() = \n%#v\n, want \n%#v\n", got, tt.want)
			}
		})
	}
}
