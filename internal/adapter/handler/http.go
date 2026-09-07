package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/ada/utils/bind"

	"github.com/rakunlabs/query"
	"github.com/worldline-go/types"

	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
)

type HTTP struct {
	Service port.CalendarService

	Validator QueryValidator
}

type QueryValidator struct {
	GetEvents    *query.Validator
	DeleteEvents *query.Validator

	GetRelations    *query.Validator
	DeleteRelations *query.Validator

	GetEventsDate *query.Validator
	GetICS        *query.Validator
}

var DefaultLimit uint64 = 25

func NewHTTP(svc port.CalendarService) (*HTTP, error) {
	validatorGetEvents, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithSort(query.WithIn("id", "entity", "event_group", "name", "description", "disabled", "date_from", "date_to", "updated_at", "updated_by")),
		query.WithValues(query.WithIn("id", "entity", "event_group", "name", "description", "disabled", "updated_by")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for GetEvents: %w", err)
	}

	validatorDeleteEvents, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithValues(query.WithIn("id")),
		query.WithValue("id", query.WithOperator(query.OperatorEq, query.OperatorIn), query.WithNotEmpty()),
		query.WithLimit(query.WithNotAllowed()),
		query.WithOffset(query.WithNotAllowed()),
		query.WithSort(query.WithNotAllowed()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for DeleteEvents: %w", err)
	}

	validatorGetRelations, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithSort(query.WithIn("event_id", "event_group", "entity")),
		query.WithValues(query.WithIn("entity", "event_id", "event_group")),
		query.WithValue("entity", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("event_id", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("event_group", query.WithOperator(query.OperatorEq, query.OperatorIn)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for GetRelations: %w", err)
	}

	validatorDeleteRelations, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithValues(query.WithIn("entity", "event_id", "event_group")),
		query.WithValue("entity", query.WithOperator(query.OperatorEq, query.OperatorIn), query.WithNotEmpty()),
		query.WithValue("event_id", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("event_group", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithLimit(query.WithNotAllowed()),
		query.WithOffset(query.WithNotAllowed()),
		query.WithSort(query.WithNotAllowed()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for DeleteRelations: %w", err)
	}

	validatorGetEventsDate, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithValues(query.WithIn("entity", "event_group", "date")),
		query.WithValue("entity", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("event_group", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("date", query.WithOperator(query.OperatorEq), query.WithNotEmpty()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for GetEventsDate: %w", err)
	}

	validatorGetICS, err := query.NewValidator(
		query.WithField(query.WithNotAllowed()),
		query.WithValues(query.WithIn("entity", "event_group", "year")),
		query.WithValue("entity", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("event_group", query.WithOperator(query.OperatorEq, query.OperatorIn)),
		query.WithValue("year", query.WithOperator(query.OperatorEq, query.OperatorIn)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create validator for GetICS: %w", err)
	}

	return &HTTP{
		Service: svc,
		Validator: QueryValidator{
			GetEvents:       validatorGetEvents,
			DeleteEvents:    validatorDeleteEvents,
			DeleteRelations: validatorDeleteRelations,
			GetRelations:    validatorGetRelations,
			GetEventsDate:   validatorGetEventsDate,
			GetICS:          validatorGetICS,
		},
	}, nil
}

func (h *HTTP) RegisterRoutes(g *ada.Mux) {
	g.ErrorHandler(HTTPErrorHandler)
	g.GET("/events", h.GetEvents)
	g.GET("/occurrences", h.Occurrences)
	g.POST("/events", h.AddEvents)
	g.DELETE("/events", h.DeleteEvents)

	g.GET("/events/{id}", h.GetEvent)
	g.DELETE("/events/{id}", h.DeleteEvent)
	g.PUT("/events/{id}", h.PutEvent)

	g.GET("/relations", h.GetRelations)
	g.POST("/relations", h.AddRelations)
	g.DELETE("/relations", h.DeleteRelations)

	g.GET("/holidays", h.Holidays)
	g.POST("/ics", h.AddICS)
	g.GET("/ics", h.GetICS)
}

// @Summary GetEvents
// @Description GetEvents
// @Param id query string false "id"
// @Param name query string false "name"
// @Param description query string false "description"
// @Param event_group query string false "event_group"
// @Param entity query string false "entity for relation"
// @Param disabled query bool false "disabled"
// @Param limit query int false "limit" default(25)
// @Param offset query int false "offset"
// @Success 200 {object} Response[[]models.Event]
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events [get]
// @Tags Events
func (h *HTTP) GetEvents(c *ada.Context) error {
	q, err := parseQuery(
		c.Request.URL.RawQuery,
		h.Validator.GetEvents,
		query.WithDefaultLimit(DefaultLimit),
	)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	events, err := h.Service.GetEvents(c.Request.Context(), q)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}
	if len(events) == 0 {
		return ada.NewHTTPError(http.StatusNotFound, "no events found")
	}

	count, err := h.Service.GetEventsCount(c.Request.Context(), q)
	if err != nil {
		return &ada.HTTPError{Code: http.StatusInternalServerError, Message: "events count failed", Err: err}
	}

	return c.SendJSON(Response[[]models.Event]{
		Meta: &Meta{
			TotalItemCount: count,
			Limit:          q.GetLimit(),
			Offset:         q.GetOffset(),
		},
		Payload: events,
	})
}

// @Summary AddEvents
// @Description AddEvents
// @Accept json
// @Param body body []models.Event true "Event"
// @Success 200 {object} Response[[]string]
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events [post]
// @Tags Events
func (h *HTTP) AddEvents(c *ada.Context) error {
	v := []models.Event{}
	if err := c.Bind(&v, bind.WithJSONSingleAsSlice(true)); err != nil {
		return responseError(http.StatusBadRequest, err)
	}

	updatedBy := c.Request.Header.Get("X-User")
	for i := range v {
		v[i].UpdatedBy = updatedBy
	}

	if err := h.Service.AddEvents(c.Request.Context(), v); err != nil {
		return err
	}

	ids := make([]string, len(v))
	for i := range v {
		ids[i] = v[i].ID
	}

	return c.SendJSON(Response[[]string]{
		Message: &Message{
			Text: "Events added",
		},
		Payload: ids,
	})
}

// @Summary GetEvent
// @Description GetEvent
// @Param id path string true "Event ID"
// @Success 200 {object} Response[models.Event]
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events/{id} [get]
// @Tags Events
func (h *HTTP) GetEvent(c *ada.Context) error {
	id := c.Request.PathValue("id")
	if id == "" {
		return ada.NewHTTPError(http.StatusBadRequest, "missing event ID")
	}

	event, err := h.Service.GetEvent(c.Request.Context(), id)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}
	if event == nil {
		return ada.NewHTTPError(http.StatusNotFound, "event not found")
	}

	return c.SendJSON(Response[models.Event]{
		Payload: *event,
	})
}

// @Summary DeleteEvent
// @Description DeleteEvent
// @Param id path string true "Event ID"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events/{id} [delete]
// @Tags Events
func (h *HTTP) DeleteEvent(c *ada.Context) error {
	id := c.Request.PathValue("id")
	if id == "" {
		return ada.NewHTTPError(http.StatusBadRequest, "missing event ID")
	}

	if err := h.Service.RemoveEvent(c.Request.Context(), id); err != nil {
		return err
	}

	return nil
}

// @Summary DeleteEvents
// @Description DeleteEvents for multiple events
// @Param id query string true "Event ID"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events [delete]
// @Tags Events
func (h *HTTP) DeleteEvents(c *ada.Context) error {
	q, err := parseQuery(
		c.Request.URL.RawQuery,
		h.Validator.DeleteEvents,
	)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ids := q.GetValues("id")
	if len(ids) == 0 {
		return ada.NewHTTPError(http.StatusBadRequest, "missing event ID")
	}

	if err := h.Service.RemoveEvent(c.Request.Context(), q.GetValues("id")...); err != nil {
		return err
	}

	return nil
}

// @Summary PutEvent
// @Description PutEvent
// @Param id path string true "Event ID"
// @Param body body models.Event true "Event"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /events/{id} [put]
// @Tags Events
func (h *HTTP) PutEvent(c *ada.Context) error {
	id := c.Request.PathValue("id")
	if id == "" {
		return ada.NewHTTPError(http.StatusBadRequest, "missing event ID")
	}

	v := models.Event{}
	if err := json.NewDecoder(c.Request.Body).Decode(&v); err != nil {
		return responseError(http.StatusBadRequest, err)
	}

	updatedBy := c.Request.Header.Get("X-User")
	v.UpdatedBy = updatedBy

	if err := h.Service.UpdateEvent(c.Request.Context(), id, &v); err != nil {
		return err
	}

	return c.SendJSON(ResponseMessage{
		Message: &Message{
			Text: "Event updated",
		},
	})
}

// /////////////////////////////////////////////////////////////
// Relations
// /////////////////////////////////////////////////////////////

// @Summary AddRelations
// @Description Accepts one relation or a nonempty batch. Entity and supplied targets must be nonempty. At least one target is required; both targets retain group OR event matching. Duplicate assignments are ignored.
// @Accept json
// @Param body body []models.Relation true "Relation"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /relations [post]
// @Tags Relations
func (h *HTTP) AddRelations(c *ada.Context) error {
	v := []models.Relation{}
	if err := c.Bind(&v, bind.WithJSONSingleAsSlice(true)); err != nil {
		return responseError(http.StatusBadRequest, err)
	}
	if len(v) == 0 {
		return ada.NewHTTPError(http.StatusBadRequest, "empty relations batch")
	}

	updatedBy := c.Request.Header.Get("X-User")
	for i := range v {
		if strings.TrimSpace(v[i].Entity) == "" {
			return ada.NewHTTPError(http.StatusBadRequest, "missing entity")
		}
		if (v[i].EventGroup.Valid && strings.TrimSpace(v[i].EventGroup.V) == "") ||
			(v[i].EventID.Valid && strings.TrimSpace(v[i].EventID.V) == "") ||
			(!v[i].EventGroup.Valid && !v[i].EventID.Valid) {
			return ada.NewHTTPError(http.StatusBadRequest, "provide at least one target; supplied targets must be nonempty")
		}

		v[i].UpdatedBy = updatedBy
	}

	if err := h.Service.AddRelations(c.Request.Context(), v); err != nil {
		return err
	}

	return c.SendJSON(ResponseMessage{
		Message: &Message{
			Text: "Relations added",
		},
	})
}

// @Summary DeleteRelations
// @Description DeleteRelations for multiple relations
// @Description With _exact=true, entity and targets are literal equality values; omitted targets must be NULL. At least one target is required. Without _exact, legacy bulk query semantics apply.
// @Param _exact query bool false "Delete only the exact assignment, preserving combined rules"
// @Param entity query string true "entity"
// @Param event_id query string false "event_id"
// @Param event_group query string false "event_group"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /relations [delete]
// @Tags Relations
func (h *HTTP) DeleteRelations(c *ada.Context) error {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	var q *query.Query
	if exact, present := values["_exact"]; present {
		if len(exact) != 1 || exact[0] != "true" {
			return ada.NewHTTPError(http.StatusBadRequest, "_exact must be true")
		}
		values.Del("_exact")
		q, err = literalQuery(values, "entity", "event_group", "event_id")
		if err == nil {
			if !q.Has("entity") || !q.HasAny("event_group", "event_id") {
				return ada.NewHTTPError(http.StatusBadRequest, "exact deletion requires entity and at least one target")
			}
			for _, field := range []string{"event_group", "event_id"} {
				if !q.Has(field) {
					expr := query.NewExpressionCmp(query.OperatorIs, field, nil)
					q.Values[field] = []*query.ExpressionCmp{expr}
					q.Where = append(q.Where, expr)
				}
			}
		}
	} else {
		q, err = parseQuery(c.Request.URL.RawQuery, h.Validator.DeleteRelations)
	}
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := h.Service.RemoveRelation(c.Request.Context(), q); err != nil {
		return err
	}

	return c.SendJSON(ResponseMessage{
		Message: &Message{
			Text: "Relation removed",
		},
	})
}

// @Summary GetRelations
// @Description GetRelations
// @Param entity query string false "entity"
// @Param event_id query string false "event_id"
// @Param event_group query string false "event_group"
// @Param sort query string false "sort"
// @Param limit query int false "limit" default(25)
// @Param offset query int false "offset"
// @Success 200 {object} Response[[]models.Relation]
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /relations [get]
// @Tags Relations
func (h *HTTP) GetRelations(c *ada.Context) error {
	q, err := parseQuery(c.Request.URL.RawQuery, h.Validator.GetRelations, query.WithDefaultLimit(DefaultLimit))
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	relations, err := h.Service.GetRelations(c.Request.Context(), q)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}
	if len(relations) == 0 {
		return ada.NewHTTPError(http.StatusNotFound, "no relations found")
	}

	count, err := h.Service.GetRelationsCount(c.Request.Context(), q)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}

	return c.SendJSON(Response[[]models.Relation]{
		Meta: &Meta{
			TotalItemCount: count,
			Limit:          q.GetLimit(),
			Offset:         q.GetOffset(),
		},
		Payload: relations,
	})
}

// ////////////////////////////////////////////////////////////////

// @Summary Holidays
// @Description Holidays for specific date
// @Param entity query string false "entity for relation"
// @Param event_group query string false "country for relation"
// @Param date query string true "date specific event"
// @Success 200 {object} Response[[]models.Event]
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /holidays [get]
// @Tags Search
func (h *HTTP) Holidays(c *ada.Context) error {
	q, err := parseQuery(
		c.Request.URL.RawQuery,
		h.Validator.GetEventsDate,
		query.WithSkipExpressionCmp("date"),
	)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	events, err := h.Service.GetEvents(c.Request.Context(), q)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}
	if len(events) == 0 {
		return ada.NewHTTPError(http.StatusNotFound, "no events found")
	}

	return c.SendJSON(Response[[]models.Event]{
		Meta: &Meta{
			TotalItemCount: uint64(len(events)),
			Limit:          q.GetLimit(),
			Offset:         q.GetOffset(),
		},
		Payload: events,
	})
}

// @Summary AddICS
// @Description Upload an ICS file. The entire multipart request is limited to 10 MiB.
// @Accept multipart/form-data
// @Param file formData file true "ICS file"
// @Param event_group query string false "event_group for ics"
// @Param tz query string false "timezone like Europe/Amsterdam default UTC"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 413 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /ics [post]
// @Tags iCal
func (h *HTTP) AddICS(c *ada.Context) error {
	// Bound the whole multipart request, including headers and non-file fields.
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, 10<<20)
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	var eventGroupNull types.Null[string]
	if eventGroup := c.Request.URL.Query().Get("event_group"); eventGroup != "" {
		eventGroupNull = types.NewNull(eventGroup)
	}

	src, _, err := c.Request.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ada.NewHTTPError(http.StatusRequestEntityTooLarge, "ICS upload exceeds 10 MiB")
		}
		return ada.NewHTTPError(http.StatusBadRequest, "failed to get file: "+err.Error())
	}

	defer src.Close()

	tz := strings.TrimSpace(c.Request.URL.Query().Get("tz"))
	defaultTZ := time.UTC
	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return ada.NewHTTPError(http.StatusBadRequest, "invalid timezone: "+tz+" "+err.Error())
		}

		defaultTZ = loc
	}

	if err := h.Service.AddIcal(c.Request.Context(), src, defaultTZ, eventGroupNull, c.Request.Header.Get("X-User")); err != nil {
		return ada.NewHTTPError(http.StatusInternalServerError, "failed to add ICS: "+err.Error())
	}

	return c.SendJSON(ResponseMessage{
		Message: &Message{
			Text: "ICS added",
		},
	})
}

// @Summary GetICS
// @Description GetICS
// @Param entity query string false "entity for relation"
// @Param event_group query string false "country"
// @Param year query string false "specific year events"
// @Success 200 {object} ResponseMessage
// @Failure 400 {object} ResponseMessage
// @Failure 500 {object} ResponseMessage
// @Router /ics [get]
// @Tags iCal
func (h *HTTP) GetICS(c *ada.Context) error {
	// Extract explicit scope equality before the generic parser interprets
	// parentheses as query syntax. Leave legacy expressions encoded as received.
	literal := url.Values{}
	remaining := []string{}
	depth := 0
	for _, part := range strings.Split(c.Request.URL.RawQuery, "&") {
		key, value, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(key)
		if err != nil {
			return ada.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if depth == 0 && (key == "entity[eq]" || key == "event_group[eq]") {
			value, err = url.QueryUnescape(value)
			if err != nil {
				return ada.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			literal.Add(key, value)
			continue
		}
		remaining = append(remaining, part)
		// Match the generic parser's grouping rules for legacy expressions.
		grouping := strings.ReplaceAll(strings.ReplaceAll(part, "%28", "("), "%29", ")")
		depth += strings.Count(grouping, "(") - strings.Count(grouping, ")")
	}
	scopes, err := literalQuery(literal, "entity", "event_group")
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	q, err := parseQuery(
		strings.Join(remaining, "&"),
		h.Validator.GetICS,
		query.WithSkipExpressionCmp("year"),
	)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if q.Values == nil {
		q.Values = make(map[string][]*query.ExpressionCmp)
	}
	for field, expressions := range scopes.Values {
		q.Values[field] = append(q.Values[field], expressions...)
	}
	q.Where = append(q.Where, scopes.Where...)

	events, err := h.Service.GetEventsICS(c.Request.Context(), q)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}

	// convert ics format
	category := strings.Join(q.GetValues("entity"), ",")
	fileName := strings.ToLower(strings.ReplaceAll(category, ",", "_"))
	if fileName == "" {
		fileName = "events"
	}
	if category == "" {
		category = "Holidays"
	}

	str, err := ical.GenerateICS(events, category)
	if err != nil {
		return responseError(http.StatusInternalServerError, err)
	}

	// send ics file
	return c.SetHeader("Content-Type", "text/calendar", "Content-Disposition", "attachment; filename="+fileName+".ics").SendString(str)
}
