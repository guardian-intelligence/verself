package postgres

import (
	"context"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "postgres" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	postgresConfig, err := projection.ConfigMap(loaded, "postgres")
	if err != nil {
		return err
	}
	limits, err := projection.PostgresRoleConnectionLimits(loaded)
	if err != nil {
		return err
	}
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	var databases []map[string]any
	for _, component := range components {
		postgres, err := projection.Map(component.Value, component.Name, "postgres")
		if err != nil {
			return err
		}
		database, err := projection.String(postgres, component.Name+".postgres", "database")
		if err != nil {
			return err
		}
		if database == "" {
			continue
		}
		databases = append(databases, map[string]any{
			"component": component.Name,
			"database":  database,
			"owner":     postgres["owner"],
		})
	}
	return projection.WriteYAML(out, "postgres", map[string]any{
		"postgresql_max_connections":                postgresConfig["max_connections"],
		"postgresql_superuser_reserved_connections": postgresConfig["superuser_reserved_connections"],
		"postgresql_role_connection_limits":         limits,
		"topology_postgres":                         map[string]any{"databases": databases},
	})
}
