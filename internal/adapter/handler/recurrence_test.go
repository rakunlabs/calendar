package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/internal/core/port"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
)

type eventErrorService struct {
	port.CalendarService
	err error
}

func (s eventErrorService) UpdateEvent(context.Context, string, *models.Event) error { return s.err }
func (s eventErrorService) AddEvents(context.Context, []models.Event) error          { return s.err }

func TestEventValidationAndConflictStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{port.ErrInvalidEvent, http.StatusBadRequest},
		{port.ErrConflict, http.StatusConflict},
		{fmt.Errorf("database unavailable"), http.StatusInternalServerError},
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			h, err := NewHTTP(eventErrorService{err: fmt.Errorf("write event: %w", tc.err)})
			require.NoError(t, err)
			mux := ada.NewMux()
			h.RegisterRoutes(mux)
			path, body := "/events", "[{}]"
			if method == http.MethodPut {
				path, body = "/events/master", "{}"
			}
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
		}
	}
}
