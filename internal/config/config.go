package config

import (
	"context"
	"fmt"

	"github.com/rakunlabs/chu"
	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/tell"

	_ "github.com/rakunlabs/chu/loader/external/loaderconsul"
	_ "github.com/rakunlabs/chu/loader/external/loadervault"
)

var (
	ServiceName    = "calendar"
	ServiceVersion = "v0.0.0"

	ServiceLog = ServiceName + "@" + ServiceVersion
)

// Config contains application configuration for this command.
type Config struct {
	LogLevel string `cfg:"log_level" default:"info"`
	Port     uint   `cfg:"port"      default:"8080"`

	DBType       string `cfg:"db_type"       default:"pgx"`
	DBDataSource string `cfg:"db_datasource" log:"false"`
	DBSchema     string `cfg:"db_schema"     default:"public"`

	Migrate Migrate `cfg:"migrate"`

	Telemetry tell.Config `cfg:"telemetry"`
}

// Migrate contains database connection to run the migrations.
type Migrate struct {
	DBDatasource string `cfg:"db_datasource" log:"false"`
	DBType       string `cfg:"db_type"       default:"pgx"`
	DBSchema     string `cfg:"db_schema"     default:"public"`
	DBTable      string `cfg:"db_table"      default:"calendar_migrations"`
}

func Load(ctx context.Context) (*Config, error) {
	cfg := &Config{}

	if err := chu.Load(ctx, ServiceName, cfg, chu.WithVersion(ServiceVersion)); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := logi.SetLogLevel(cfg.LogLevel); err != nil {
		return nil, fmt.Errorf("parse log level %s: %w", cfg.LogLevel, err)
	}

	return cfg, nil
}
