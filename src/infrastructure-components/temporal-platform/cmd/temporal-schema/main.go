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

	verselfotel "github.com/verself/observability/otel"
	"github.com/verself/temporal-platform/internal/pgsocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.temporal.io/server/common/config"
	tlog "go.temporal.io/server/common/log"
	_ "go.temporal.io/server/common/persistence/sql/sqlplugin/postgresql"
	"go.temporal.io/server/tools/common/schema"
	toolsql "go.temporal.io/server/tools/sql"
)

var (
	tracer  = otel.Tracer("github.com/verself/temporal-platform/cmd/temporal-schema")
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
		return errors.New("usage: temporal-schema <migrate|setup|update> --config <path> [--store <datastore>] [--schema-name <schema>] [--version <version>]")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
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
	configPath := flags.String("config", "", "path to Temporal config file")
	storeName := flags.String("store", "", "Temporal datastore name")
	schemaName := flags.String("schema-name", "", "embedded Temporal schema name")
	initialVersion := flags.String("version", "0.0", "initial schema version for setup")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	if command != "migrate" && strings.TrimSpace(*storeName) == "" {
		return errors.New("--store is required")
	}
	cfg, err := config.Load(config.WithConfigFile(*configPath))
	if err != nil {
		return fmt.Errorf("load temporal config %s: %w", *configPath, err)
	}
	if err := pgsocket.ConfigureTemporalDatastores(cfg); err != nil {
		return fmt.Errorf("configure temporal datastores: %w", err)
	}

	_, span := tracer.Start(ctx, "temporal.schema."+command)
	defer span.End()

	switch command {
	case "migrate":
		tasks := []schemaTask{
			{Command: "setup", Store: "postgres-default", Version: "0.0"},
			{Command: "update", Store: "postgres-default", SchemaName: "postgresql/v12/temporal"},
			{Command: "setup", Store: "postgres-visibility", Version: "0.0"},
			{Command: "update", Store: "postgres-visibility", SchemaName: "postgresql/v12/visibility"},
		}
		for _, task := range tasks {
			if err := runSchemaTask(cfg, task); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}
		}
		return nil
	case "setup":
		if err := runSchemaTask(cfg, schemaTask{
			Command:    "setup",
			Store:      strings.TrimSpace(*storeName),
			SchemaName: strings.TrimSpace(*schemaName),
			Version:    strings.TrimSpace(*initialVersion),
		}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	case "update":
		if strings.TrimSpace(*schemaName) == "" {
			return errors.New("--schema-name is required for update")
		}
		if err := runSchemaTask(cfg, schemaTask{
			Command:    "update",
			Store:      strings.TrimSpace(*storeName),
			SchemaName: strings.TrimSpace(*schemaName),
		}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	default:
		return fmt.Errorf("unknown temporal-schema command %q", command)
	}

	return nil
}

type schemaTask struct {
	Command    string
	Store      string
	SchemaName string
	Version    string
}

func runSchemaTask(cfg *config.Config, task schemaTask) error {
	sqlCfg, err := datastoreSQL(cfg, task.Store)
	if err != nil {
		return err
	}
	tlogger := tlog.NewCLILogger()
	conn, err := toolsql.NewConnection(sqlCfg, tlogger)
	if err != nil {
		return fmt.Errorf("open temporal datastore %s: %w", task.Store, err)
	}
	defer conn.Close()
	switch task.Command {
	case "setup":
		if err := schema.NewSetupSchemaTask(conn, &schema.SetupConfig{
			SchemaName:     strings.TrimSpace(task.SchemaName),
			InitialVersion: strings.TrimSpace(task.Version),
		}, tlogger).Run(); err != nil {
			if schemaAlreadySetup(err) {
				return nil
			}
			return fmt.Errorf("setup temporal datastore %s schema version tables: %w", task.Store, err)
		}
	case "update":
		if err := schema.NewUpdateSchemaTask(conn, &schema.UpdateConfig{
			DBName:     strings.TrimSpace(sqlCfg.DatabaseName),
			SchemaName: strings.TrimSpace(task.SchemaName),
		}, tlogger).Run(); err != nil {
			return fmt.Errorf("update temporal schema %s on datastore %s: %w", task.SchemaName, task.Store, err)
		}
	default:
		return fmt.Errorf("unknown temporal schema task command %q", task.Command)
	}
	return nil
}

func schemaAlreadySetup(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate key")
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
