// Package ops projects the explicit Ansible-vars surface declared in
// loaded.Config.AnsibleVars into group_vars/all/generated/ops.yml, plus
// the per-component electric instance projection that has to be computed
// from topology rather than authored as static config.
package ops

import (
	"context"
	"maps"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "ops" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	payload := maps.Clone(loaded.Config.AnsibleVars)
	if payload == nil {
		payload = map[string]any{}
	}
	electric, err := electricInstances(loaded)
	if err != nil {
		return err
	}
	payload["topology_electric_instances"] = electric
	return projection.WriteYAML(out, "ops", payload)
}

func electricInstances(loaded load.Loaded) (map[string]any, error) {
	components, err := projection.ElectricComponents(loaded)
	if err != nil {
		return nil, err
	}
	rendered := map[string]any{}
	for _, component := range components {
		sync := component.Sync
		instance, err := projection.String(sync, component.ComponentName+".electric", "instance")
		if err != nil {
			return nil, err
		}
		rendered[instance] = map[string]any{
			"electric_instance":              instance,
			"electric_service_name":          component.ServiceName,
			"electric_service_port":          component.Port,
			"electric_pg_role":               sync["pg_role"],
			"electric_pg_conn_limit":         sync["pg_conn_limit"],
			"electric_db":                    sync["source_database"],
			"electric_writer_role":           sync["writer_role"],
			"electric_publication_name":      sync["publication_name"],
			"electric_publication_tables":    sync["publication_tables"],
			"electric_storage_dir":           sync["storage_dir"],
			"electric_credstore_dir":         sync["credstore_dir"],
			"electric_nftables_table":        sync["nftables_table"],
			"electric_nftables_file":         sync["nftables_file"],
			"electric_db_pool_size":          sync["db_pool_size"],
			"electric_replication_stream_id": sync["replication_stream_id"],
		}
	}
	return rendered, nil
}
