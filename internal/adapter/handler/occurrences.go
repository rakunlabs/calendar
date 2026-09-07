package handler

import (
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
)

// @Summary Get event occurrences in a date range
// @Tags Events
// @Param from query string true "Inclusive start (RFC3339)"
// @Param to query string true "Exclusive end (RFC3339), at most 400 days after from"
// @Param entity query string false "Literal entity equality (also accepts entity[eq]); AND with event_group"
// @Param event_group query string false "Literal event group equality; pagination is not supported"
// @Success 200 {object} Response[[]models.Event]
// @Failure 400 {object} ResponseMessage
// @Router /occurrences [get]
func (h *HTTP) Occurrences(c *ada.Context) error {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(values["from"]) != 1 || len(values["to"]) != 1 {
		return ada.NewHTTPError(http.StatusBadRequest, "expected one from and to timestamp and valid query encoding")
	}
	from, fromErr := time.Parse(time.RFC3339, values.Get("from"))
	to, toErr := time.Parse(time.RFC3339, values.Get("to"))
	if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		return ada.NewHTTPError(http.StatusBadRequest, "from and to must be RFC3339 timestamps forming a range of at most 400 days")
	}
	values.Del("from")
	values.Del("to")
	q, err := literalQuery(values, "entity", "event_group")
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	events, err := h.Service.GetEvents(c.Request.Context(), q)
	if err != nil {
		return err
	}
	result := []models.Event{}
	for _, event := range events {
		occurrences, err := ical.Occurrences(c.Request.Context(), event, from, to)
		if err != nil {
			return err
		}
		result = append(result, occurrences...)
		if len(result) > 20000 {
			return ada.NewHTTPError(http.StatusUnprocessableEntity, "Too many occurrences. Select a smaller date range.")
		}
	}
	slices.SortFunc(result, func(a, b models.Event) int { return a.DateFrom.Compare(b.DateFrom.Time) })
	return c.SendJSON(Response[[]models.Event]{Payload: result})
}
