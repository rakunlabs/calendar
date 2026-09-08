package domain

import (
	"database/sql/driver"

	"github.com/worldline-go/types"
)

// CalendarDate preserves civil values that cannot be reconstructed from an instant.
// Type is DATE, DATE-TIME, or empty (DATE-TIME); Value uses the ICS date syntax.
type CalendarDate struct {
	Value string `json:"value"`
	TZID  string `json:"tzid,omitempty"`
	Type  string `json:"type,omitempty"`
}

type RecurrencePeriod struct {
	Start    CalendarDate  `json:"start"`
	End      *CalendarDate `json:"end,omitempty"`
	Duration string        `json:"duration,omitempty"`
}

type OccurrenceOverride struct {
	RecurrenceID CalendarDate `json:"recurrence_id"`
	Cancelled    bool         `json:"cancelled,omitempty"`
	Event        *Event       `json:"event,omitempty"`
}

// Recurrence is stored atomically with its master, never as competing rows sharing a UID.
type Recurrence struct {
	Start     *CalendarDate        `json:"start,omitempty"`
	End       *CalendarDate        `json:"end,omitempty"`
	Duration  string               `json:"duration,omitempty"`
	ExDates   []CalendarDate       `json:"exdates,omitempty"`
	RDates    []RecurrencePeriod   `json:"rdates,omitempty"`
	Overrides []OccurrenceOverride `json:"overrides,omitempty"`
	Timezones []string             `json:"timezones,omitempty"`
}

func (r Recurrence) Value() (driver.Value, error) {
	return types.NewJSON(r).Value()
}

func (r *Recurrence) Scan(value any) error {
	var data types.JSON[Recurrence]
	if err := data.Scan(value); err != nil {
		return err
	}
	*r = data.V
	return nil
}
