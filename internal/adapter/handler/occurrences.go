package handler

import (
	"net/http"
	"slices"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/rakunlabs/query"
)

// @Summary Get event occurrences in a date range
// @Tags Events
// @Param from query string true "Inclusive start (RFC3339)"
// @Param to query string true "Exclusive end (RFC3339), at most 400 days after from"
// @Success 200 {object} Response[[]models.Event]
// @Failure 400 {object} ResponseMessage
// @Router /occurrences [get]
func (h *HTTP) Occurrences(c *ada.Context) error {
	from, fromErr := time.Parse(time.RFC3339, c.Request.URL.Query().Get("from"))
	to, toErr := time.Parse(time.RFC3339, c.Request.URL.Query().Get("to"))
	if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		return ada.NewHTTPError(http.StatusBadRequest, "from and to must be RFC3339 timestamps forming a range of at most 400 days")
	}
	events, err := h.Service.GetEvents(c.Request.Context(), query.New())
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
