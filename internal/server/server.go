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
	"github.com/rakunlabs/calendar/internal/adapter/mcp"
	"github.com/rakunlabs/calendar/internal/config"
	"github.com/rakunlabs/calendar/internal/core/port"
	_ "github.com/rakunlabs/calendar/internal/server/docs"
)

// Option adjusts optional server surfaces that the core API does not require.
type Option func(*options)

type options struct {
	mcp config.MCP
}

// WithMCP mounts the Model Context Protocol endpoint at <base_path>/mcp.
func WithMCP(cfg config.MCP) Option {
	return func(o *options) { o.mcp = cfg }
}

// @title calendar API
// @BasePath /calendar/v1
func NewServer(_ context.Context, svc port.CalendarService, basePath string, opts ...Option) (*ada.Server, error) {
	settings := options{}
	for _, opt := range opts {
		opt(&settings)
	}
	// An unset base path serves at the root; there is no implicit mount point.
	prefix := strings.Trim(strings.TrimSpace(basePath), "/")
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
		(&mcors.Cors{
			AllowHeaders:  []string{"Content-Type", "Authorization", "X-User", "X-Request-Id", "Accept", "Last-Event-ID", "Mcp-Session-Id", "Mcp-Protocol-Version"},
			ExposeHeaders: []string{"Mcp-Session-Id", "Mcp-Protocol-Version"},
		}).Middleware(),
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
	if settings.mcp.Enabled {
		calendar.Handle("/mcp", mcp.New(svc, settings.mcp.ReadOnly).HTTPHandler())
	}
	if err := registerUI(calendar, prefix); err != nil {
		return nil, err
	}

	return s, nil
}
