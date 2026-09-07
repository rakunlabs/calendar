package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rakunlabs/chu"
	"github.com/rakunlabs/into"
	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/tell"

	"github.com/rakunlabs/calendar/internal/adapter/repository"
	"github.com/rakunlabs/calendar/internal/config"
	"github.com/rakunlabs/calendar/internal/core/service"
	"github.com/rakunlabs/calendar/internal/server"
)

var (
	version = "v0.0.0"
	commit  = ""
	date    = ""
)

func main() {
	config.ServiceVersion = version

	into.Init(
		run,
		into.WithLogger(logi.InitializeLog(logi.WithCaller(false))),
		into.WithMsgf("%s [%s] build %s %s", config.ServiceName, config.ServiceVersion, commit, date),
	)
}

func run(ctx context.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "loaded configuration", "config", chu.MarshalMap(cfg))

	// ///////////////////////////////////////////////////////
	// telemetry initialize
	collector, err := tell.New(ctx, cfg.Telemetry)
	if err != nil {
		return fmt.Errorf("failed to init telemetry; %w", err)
	}
	// flush metrics on failure
	defer collector.Shutdown()

	// ///////////////////////////////////////////////////////
	// database operations
	if err := repository.MigrateDB(ctx, cfg); err != nil {
		return fmt.Errorf("failed database migration: %w", err)
	}

	calendarPostgresAdapter, err := repository.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// ///////////////////////////////////////////////////////
	// service initialize
	svc, err := service.NewCalendarService(ctx, calendarPostgresAdapter)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// ///////////////////////////////////////////////////////
	// server initialize
	srv, err := server.NewServer(ctx, svc)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// ///////////////////////////////////////////////////////
	// start server
	return srv.StartWithContext(ctx, fmt.Sprintf(":%d", cfg.Port))
}
