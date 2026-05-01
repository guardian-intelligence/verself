// Package ops projects the explicit Ansible-vars surface declared in
// loaded.Config.AnsibleVars into group_vars/all/generated/ops.yml, plus
// the per-component electric instance projection that has to be computed
// from topology rather than authored as static config.
package ops

import (
	"context"
	"fmt"
	"maps"
	"sort"

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
	if artifacts, ok := loaded.ConfigMap["artifacts"]; ok {
		payload["topology_artifacts"] = artifacts
	}
	electric, err := electricInstances(loaded)
	if err != nil {
		return err
	}
	payload["topology_electric_instances"] = electric

	// Cert-id allowlist for the detect-recent-intrusions query. Every
	// cert OpenBao mints carries a KeyID `verself-<principal>-<suffix>`
	// where suffix is either an operator device name or
	// `slot-<index>` for a workload-pool slot. Anything outside this
	// set arriving in verself.host_auth_events is, by construction, a
	// machine that escaped the trust set — exactly the Blacksmith-CI
	// class of incident.
	suffixes, err := knownCertIDSuffixes(loaded)
	if err != nil {
		return err
	}
	payload["known_cert_id_suffixes"] = suffixes

	return projection.WriteYAML(out, "ops", payload)
}

// knownCertIDSuffixes flattens operators.<op>.devices.<device>.name plus
// `slot-0`..`slot-{count-1}` into a sorted []string. Sorting keeps the
// rendered ops.yml byte-stable across re-runs (CUE map iteration order
// is unspecified).
func knownCertIDSuffixes(loaded load.Loaded) ([]string, error) {
	out := []string{}
	for _, op := range loaded.Config.Operators {
		for _, dev := range op.Devices {
			if dev.Name == "" {
				return nil, fmt.Errorf("operator device name is empty")
			}
			out = append(out, dev.Name)
		}
	}
	for i := int64(0); i < loaded.Config.Workloads.Pool.SlotCount; i++ {
		out = append(out, fmt.Sprintf("slot-%d", i))
	}
	sort.Strings(out)
	return out, nil
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
