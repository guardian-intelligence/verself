package edgecontract

import "sort"

func BuildManifest(inputs Inputs, outputs Outputs, plan Plan) Manifest {
	routes := make([]RouteManifest, 0, len(plan.Routes))
	for _, route := range plan.Routes {
		routes = append(routes, RouteManifest{
			FQDN:       route.FQDN,
			Backend:    string(route.Backend),
			GUID:       string(route.Backend),
			Kind:       string(route.Kind),
			Component:  route.Component,
			Interface:  route.Interface,
			Endpoint:   route.Endpoint,
			Upstream:   string(route.Upstream),
			NomadJobID: route.NomadJobID,
		})
	}
	frontends := append([]HAProxyObject{}, plan.Frontends...)
	backends := backendObjects(plan.Backends)
	servers := append([]HAProxyObject{}, plan.Servers...)
	sortObjects(frontends)
	sortObjects(backends)
	sortObjects(servers)
	upstreamKeys := make([]string, 0, len(plan.UpstreamKeys))
	for _, key := range plan.UpstreamKeys {
		upstreamKeys = append(upstreamKeys, string(key))
	}
	sort.Strings(upstreamKeys)
	summary := map[string]int{
		"frontends":       len(frontends),
		"backends":        len(backends),
		"servers":         len(servers),
		"public_routes":   len(routes),
		"nomad_upstreams": len(plan.NomadUpstreams),
		"upstream_keys":   len(upstreamKeys),
	}
	return Manifest{
		Site:           plan.Site,
		Inputs:         inputs,
		Outputs:        outputs,
		Frontends:      frontends,
		Backends:       backends,
		Servers:        servers,
		Routes:         routes,
		NomadUpstreams: append([]NomadService{}, plan.NomadUpstreams...),
		UpstreamKeys:   upstreamKeys,
		Summary:        summary,
	}
}

func sortObjects(in []HAProxyObject) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Kind == in[j].Kind {
			return in[i].Name < in[j].Name
		}
		return in[i].Kind < in[j].Kind
	})
}
