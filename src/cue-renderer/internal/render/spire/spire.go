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

	spireIdentities := make([]map[string]any, 0, len(identities))
	serverPort, err := endpointPort(loaded, "spire_server", "api")
	if err != nil {
		return err
	}
	bundlePort, err := endpointPort(loaded, "spire_bundle_endpoint", "bundle")
	if err != nil {
		return err
	}
	cfg := loaded.Config.Spire
	bundleURL := fmt.Sprintf("https://%s:%d", cfg.BundleEndpointBindAddress, bundlePort)
	payload := map[string]any{
		"spire_trust_domain":                 cfg.TrustDomain,
		"spire_server_bind_address":          cfg.ServerBindAddress,
		"spire_server_bind_port":             serverPort,
		"spire_server_socket_path":           cfg.ServerSocketPath,
		"spire_agent_socket_path":            cfg.AgentSocketPath,
		"spire_workload_group":               cfg.WorkloadGroup,
		"spire_agent_id":                     "spiffe://" + cfg.TrustDomain + cfg.AgentIDPath,
		"spire_bundle_endpoint_bind_address": cfg.BundleEndpointBindAddress,
		"spire_bundle_endpoint_bind_port":    bundlePort,
		"spire_jwt_bundle_endpoint_url":      bundleURL,
		"spire_jwt_issuer_url":               bundleURL,
	}

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
		if auth != "spiffe_mtls" {
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
