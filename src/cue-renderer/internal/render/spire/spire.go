package spire

import (
	"context"
	"fmt"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
)

type Renderer struct{}

func (Renderer) Name() string { return "spire" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	identities, err := projection.WorkloadIdentities(loaded)
	if err != nil {
		return err
	}
	edges, err := projection.TopologySlice(loaded, "edges")
	if err != nil {
		return err
	}

	cfg := loaded.Spire
	serverPort, err := endpointPort(loaded, cfg.Server_component, cfg.Server_endpoint)
	if err != nil {
		return err
	}
	bundlePort, err := endpointPort(loaded, cfg.Bundle_endpoint_component, cfg.Bundle_endpoint_endpoint)
	if err != nil {
		return err
	}
	bundleURL := fmt.Sprintf("%s://%s:%d", cfg.Bundle_endpoint_scheme, cfg.Bundle_endpoint_bind_address, bundlePort)
	payload := map[string]any{
		"spire_trust_domain":                 cfg.Trust_domain,
		"spire_server_bind_address":          cfg.Server_bind_address,
		"spire_server_bind_port":             serverPort,
		"spire_server_socket_path":           cfg.Server_socket_path,
		"spire_agent_socket_path":            cfg.Agent_socket_path,
		"spire_workload_group":               cfg.Workload_group,
		"spire_agent_id":                     cfg.Agent_id,
		"spire_bundle_endpoint_bind_address": cfg.Bundle_endpoint_bind_address,
		"spire_bundle_endpoint_bind_port":    bundlePort,
		"spire_jwt_bundle_endpoint_url":      bundleURL,
		"spire_jwt_issuer_url":               bundleURL,
	}

	spireIdentities := make([]map[string]any, 0, len(identities))
	for _, identity := range identities {
		spireIdentities = append(spireIdentities, map[string]any{
			"key":                   identity["key"],
			"component":             identity["component"],
			"entry_id":              identity["entry_id"],
			"spiffe_id":             identity["spiffe_id"],
			"user":                  identity["user"],
			"group":                 identity["group"],
			"uid_policy":            identity["uid_policy"],
			"selector":              identity["selector"],
			"x509_svid_ttl_seconds": identity["x509_svid_ttl_seconds"],
			"restart_units":         identity["restart_units"],
		})
		if ansibleVar, ok := identity["ansible_var"].(string); ok && ansibleVar != "" {
			payload[ansibleVar] = identity["spiffe_id"]
		}
	}

	spiffeEdges := make([]map[string]any, 0, len(edges))
	for _, item := range edges {
		edge, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("edge: expected map, got %T", item)
		}
		auth, err := projection.String(edge, "edge", "auth")
		if err != nil {
			return err
		}
		// The CUE binding pins edge.auth to a spiffe_auth_kinds key, so an
		// unknown value would have failed evaluation. The renderer just
		// reads the spiffe_bearing bit.
		kind, ok := loaded.SpiffeAuthKinds[auth]
		if !ok {
			return fmt.Errorf("edge auth %q is not in spiffe_auth_kinds (catalog out of sync with topology binding)", auth)
		}
		if !kind.Spiffe_bearing {
			continue
		}
		spiffeEdges = append(spiffeEdges, map[string]any{
			"from": edge["from"],
			"to":   edge["to"],
		})
	}

	payload["topology_spire"] = map[string]any{
		"identities": spireIdentities,
		"edges":      spiffeEdges,
	}
	return projection.WriteYAML(out, "spire", payload)
}

func endpointPort(loaded load.Loaded, componentName, endpointName string) (int64, error) {
	component, err := projection.TopologyMap(loaded, "components")
	if err != nil {
		return 0, err
	}
	componentMap, err := projection.Map(component, "topology.components", componentName)
	if err != nil {
		return 0, err
	}
	endpoints, err := projection.Map(componentMap, "topology.components."+componentName, "endpoints")
	if err != nil {
		return 0, err
	}
	endpoint, err := projection.Map(endpoints, "topology.components."+componentName+".endpoints", endpointName)
	if err != nil {
		return 0, err
	}
	return projection.Int(endpoint, "topology.components."+componentName+".endpoints."+endpointName, "port")
}
