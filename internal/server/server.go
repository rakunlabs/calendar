package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/ada/handler/swagger"
	mcors "github.com/rakunlabs/ada/middleware/cors"
	mlog "github.com/rakunlabs/ada/middleware/log"
	mrecover "github.com/rakunlabs/ada/middleware/recover"
	mrequestid "github.com/rakunlabs/ada/middleware/requestid"
	mserver "github.com/rakunlabs/ada/middleware/server"
	mtelemetry "github.com/rakunlabs/ada/middleware/telemetry"

	"github.com/rakunlabs/calendar/internal/adapter/handler"
	"github.com/rakunlabs/calendar/internal/config"
	"github.com/rakunlabs/calendar/internal/core/port"
	_ "github.com/rakunlabs/calendar/internal/server/docs"
)

// @title calendar API
// @BasePath /calendar/v1
func NewServer(_ context.Context, svc port.CalendarService, basePath string) (*ada.Server, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		basePath = "/calendar"
	}
	prefix := strings.Trim(basePath, "/")
	if prefix != "" {
		prefix = "/" + prefix
	}
	handleHTTP, err := handler.NewHTTP(svc)
	if err != nil {
		return nil, fmt.Errorf("failed to create http handler: %w", err)
	}

	s := ada.New()
	s.Use(
		mrecover.Middleware(mrecover.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, _ error) {
			w.Header().Del("Content-Length")
			handler.HTTPErrorHandler(ada.NewContext(w, r), ada.NewHTTPError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
		})),
		mserver.Middleware(config.ServiceName),
		(&mcors.Cors{AllowHeaders: []string{"Content-Type", "Authorization", "X-User", "X-Request-Id"}}).Middleware(),
		mrequestid.Middleware(),
		mlog.Middleware(),
		mtelemetry.Middleware(),
	)
	s.ErrorHandler(handler.HTTPErrorHandler)

	calendar := s.Group(prefix)
	calendar.MethodNotAllowed(s.Wrap(func(c *ada.Context) error {
		return ada.NewHTTPError(http.StatusMethodNotAllowed, "Method Not Allowed")
	}))

	calendar.HandleFunc("/swagger/*", swagger.Handler(
		swagger.WithTitle(config.ServiceName),
		swagger.WithVersion(config.ServiceVersion),
		swagger.WithBasePath(prefix+"/v1"),
	))

	api := calendar.Group("/v1")
	api.NotFound(s.Wrap(func(c *ada.Context) error {
		return ada.NewHTTPError(http.StatusNotFound, "Not Found")
	}))

	handleHTTP.RegisterRoutes(api)
	if err := registerUI(calendar, prefix); err != nil {
		return nil, err
	}

	return s, nil
}
