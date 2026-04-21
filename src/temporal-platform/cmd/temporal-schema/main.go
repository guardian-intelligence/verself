package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	fmotel "github.com/forge-metal/otel"
	"github.com/forge-metal/temporal-platform/internal/pgsocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/server/common/config"
	_ "go.temporal.io/server/common/persistence/sql/sqlplugin/postgresql"
	tlog "go.temporal.io/server/common/log"
	toolsql "go.temporal.io/server/tools/sql"
	"go.temporal.io/server/tools/common/schema"
)

var (
	tracer  = otel.Tracer("github.com/forge-metal/temporal-platform/cmd/temporal-schema")
	version = "dev"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: temporal-schema <setup|update> --store <datastore> --schema-name <schema> [--version <version>] [--config <path>]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "temporal-schema",
		ServiceVersion: version,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		_ = otelShutdown(context.Background())
	}()
	slog.SetDefault(logger)

	command := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", strings.TrimSpace(os.Getenv("FM_TEMPORAL_CONFIG_PATH")), "path to Temporal config file")
	storeName := flags.String("store", "", "Temporal datastore name")
	schemaName := flags.String("schema-name", "", "embedded Temporal schema name")
	initialVersion := flags.String("version", "0.0", "initial schema version for setup")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config or FM_TEMPORAL_CONFIG_PATH is required")
	}
	if strings.TrimSpace(*storeName) == "" {
		return errors.New("--store is required")
	}
	cfg, err := config.Load(config.WithConfigFile(*configPath))
	if err != nil {
		return fmt.Errorf("load temporal config %s: %w", *configPath, err)
	}
	if err := pgsocket.ConfigureTemporalDatastores(cfg); err != nil {
		return fmt.Errorf("configure temporal datastores: %w", err)
	}

	sqlCfg, err := datastoreSQL(cfg, *storeName)
	if err != nil {
		return err
	}

	_, span := tracer.Start(ctx, "temporal.schema."+command)
	defer span.End()
	span.SetAttributes(
		attribute.String("temporal.datastore", strings.TrimSpace(*storeName)),
		attribute.String("db.name", strings.TrimSpace(sqlCfg.DatabaseName)),
	)
	if strings.TrimSpace(*schemaName) != "" {
		span.SetAttributes(attribute.String("temporal.schema_name", strings.TrimSpace(*schemaName)))
	}

	tlogger := tlog.NewCLILogger()
	conn, err := toolsql.NewConnection(sqlCfg, tlogger)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("open temporal datastore %s: %w", *storeName, err)
	}
	defer conn.Close()

	switch command {
	case "setup":
		if err := schema.NewSetupSchemaTask(conn, &schema.SetupConfig{
			SchemaName:     strings.TrimSpace(*schemaName),
			InitialVersion: strings.TrimSpace(*initialVersion),
		}, tlogger).Run(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("setup temporal datastore %s schema version tables: %w", *storeName, err)
		}
	case "update":
		if strings.TrimSpace(*schemaName) == "" {
			return errors.New("--schema-name is required for update")
		}
		if err := schema.NewUpdateSchemaTask(conn, &schema.UpdateConfig{
			DBName:     strings.TrimSpace(sqlCfg.DatabaseName),
			SchemaName: strings.TrimSpace(*schemaName),
		}, tlogger).Run(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return fmt.Errorf("update temporal schema %s on datastore %s: %w", *schemaName, *storeName, err)
		}
	default:
		return fmt.Errorf("unknown temporal-schema command %q", command)
	}

	return nil
}

func datastoreSQL(cfg *config.Config, storeName string) (*config.SQL, error) {
	storeName = strings.TrimSpace(storeName)
	store, ok := cfg.Persistence.DataStores[storeName]
	if !ok {
		return nil, fmt.Errorf("temporal datastore %q not found in config", storeName)
	}
	if store.SQL == nil {
		return nil, fmt.Errorf("temporal datastore %q is not configured as SQL", storeName)
	}
	return store.SQL, nil
}
