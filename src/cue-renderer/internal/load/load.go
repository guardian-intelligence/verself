// Package load wraps cuelang.org/go so callers don't need to know about
// cue.Context, cue.Value, or load.Instances. The output is a Loaded
// snapshot of the topology graph: the raw Topology value plus typed
// projections of the slices renderers actually consume.
//
// The whole graph is walked exactly once per call. Renderers must not
// re-decode CUE — they read off the Loaded struct. Adding a new typed
// projection is one field on Loaded plus one Decode line in Topology.
package load

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/verself/cue-renderer/schema"
)

// Loaded is the in-memory snapshot every renderer receives. The raw
// Topology field carries every CUE value the graph projects; typed
// projections (SmokeTests, ...) are filled in alongside so renderers don't
// need to know how to walk a cue.Value.
//
// Today most of Topology is map[string]any because schema.cue still uses
// open records (`...`). As the schema tightens, gengotypes will produce
// real structs and this field becomes typed end-to-end without any
// renderer changing.
type Loaded struct {
	Value       cue.Value
	ConfigValue cue.Value
	Topology    schema.Topology
	Config      schema.InstanceConfig
	ConfigMap   map[string]any
	Catalog     Catalog
	SmokeTests  SmokeTests
	Clusters    Clusters
	Nftables    Nftables

	// GraphSHA256 is the hex SHA-256 of the canonical-JSON encoding of
	// the topology, config, and catalog inputs. Spans use it as a stable
	// identifier for "the shape of the world this run rendered against."
	GraphSHA256    string
	TopologySHA256 string
	ConfigSHA256   string
	CatalogSHA256  string
}

type Clusters struct {
	Garage   schema.GarageCluster
	Temporal schema.TemporalCluster
}

type SmokeTests struct {
	Spans []schema.SmokeTestSpan
}

type Nftables struct {
	Rulesets map[string]map[string]any
}

type Catalog struct {
	Versions            map[string]any
	ServerTools         map[string]any
	ServerToolDownloads map[string]any
	ServerToolPackaging map[string]any
	DevTools            map[string]any
	GuestVersions       map[string]any

	// Raw is the loaded catalog package value. Renderers that need lossless
	// CUE projection should prefer this over the decoded maps.
	Raw cue.Value
}

// Topology compiles the named CUE instance (e.g. "local" resolves to
// "./instances/local:topology") rooted at `dir` and decodes it. One
// cue.Value.Decode call hydrates the whole graph; one targeted Decode
// hydrates each typed projection on Loaded. Both share the same
// underlying CUE evaluation, so this remains a single graph walk.
func Topology(dir, instance string) (Loaded, error) {
	instancePath, err := topologyInstancePath(instance)
	if err != nil {
		return Loaded{}, err
	}
	root, err := buildRoot(dir, instancePath)
	if err != nil {
		return Loaded{}, err
	}
	topologyVal, err := lookupConcrete(root, "topology")
	if err != nil {
		return Loaded{}, err
	}
	configVal, err := lookupConcrete(root, "config")
	if err != nil {
		return Loaded{}, err
	}
	catalogRoot, err := buildRoot(dir, "./catalog:catalog")
	if err != nil {
		return Loaded{}, err
	}

	var loaded Loaded
	if err := topologyVal.Decode(&loaded.Topology); err != nil {
		return Loaded{}, fmt.Errorf("decode topology: %w", err)
	}
	if err := configVal.Decode(&loaded.Config); err != nil {
		return Loaded{}, fmt.Errorf("decode config: %w", err)
	}
	if err := configVal.Decode(&loaded.ConfigMap); err != nil {
		return Loaded{}, fmt.Errorf("decode config map: %w", err)
	}
	loaded.Value = topologyVal
	loaded.ConfigValue = configVal
	loaded.Catalog.Raw = catalogRoot

	if err := topologyVal.LookupPath(cue.ParsePath("smoke_tests.spans")).Decode(&loaded.SmokeTests.Spans); err != nil {
		return Loaded{}, fmt.Errorf("decode topology.smoke_tests.spans: %w", err)
	}
	if err := topologyVal.LookupPath(cue.ParsePath("components.garage.garage")).Decode(&loaded.Clusters.Garage); err != nil {
		return Loaded{}, fmt.Errorf("decode topology.components.garage.garage: %w", err)
	}
	if err := topologyVal.LookupPath(cue.ParsePath("components.temporal.temporal")).Decode(&loaded.Clusters.Temporal); err != nil {
		return Loaded{}, fmt.Errorf("decode topology.components.temporal.temporal: %w", err)
	}
	if err := topologyVal.LookupPath(cue.ParsePath("nftables.rulesets")).Decode(&loaded.Nftables.Rulesets); err != nil {
		return Loaded{}, fmt.Errorf("decode topology.nftables.rulesets: %w", err)
	}

	if err := decodeCatalog(catalogRoot, &loaded.Catalog); err != nil {
		return Loaded{}, err
	}

	topologyDigest, topologyBytes, err := canonicalDigest(topologyVal)
	if err != nil {
		return Loaded{}, err
	}
	configDigest, configBytes, err := canonicalDigest(configVal)
	if err != nil {
		return Loaded{}, err
	}
	catalogDigest, catalogBytes, err := canonicalDigest(catalogRoot)
	if err != nil {
		return Loaded{}, err
	}
	loaded.TopologySHA256 = topologyDigest
	loaded.ConfigSHA256 = configDigest
	loaded.CatalogSHA256 = catalogDigest
	loaded.GraphSHA256 = digestParts(
		namedBytes{name: "topology", bytes: topologyBytes},
		namedBytes{name: "config", bytes: configBytes},
		namedBytes{name: "catalog", bytes: catalogBytes},
	)
	return loaded, nil
}

func topologyInstancePath(instance string) (string, error) {
	name := strings.TrimSpace(instance)
	if name == "" {
		return "", fmt.Errorf("topology instance name is required")
	}
	if strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("topology instance %q must be a name under instances/, not a CUE pathspec", instance)
	}
	return fmt.Sprintf("./instances/%s:topology", name), nil
}

func buildRoot(dir, instance string) (cue.Value, error) {
	cfg := &load.Config{Dir: dir}
	insts := load.Instances([]string{instance}, cfg)
	if len(insts) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instance found for %q", instance)
	}
	if err := insts[0].Err; err != nil {
		return cue.Value{}, fmt.Errorf("load %s: %w", instance, err)
	}
	ctx := cuecontext.New()
	root := ctx.BuildInstance(insts[0])
	if err := root.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("build %s: %w", instance, err)
	}
	return root, nil
}

func lookupConcrete(root cue.Value, fieldExpr string) (cue.Value, error) {
	val := root.LookupPath(cue.ParsePath(fieldExpr))
	if err := val.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("lookup %q: %w", fieldExpr, err)
	}
	if !val.Exists() {
		return cue.Value{}, fmt.Errorf("field %q does not exist", fieldExpr)
	}
	if err := val.Validate(cue.Concrete(true)); err != nil {
		return cue.Value{}, fmt.Errorf("validate %q: %w", fieldExpr, err)
	}
	return val, nil
}

func decodeCatalog(root cue.Value, out *Catalog) error {
	for _, item := range []struct {
		path string
		dst  *map[string]any
	}{
		{path: "versions", dst: &out.Versions},
		{path: "serverTools", dst: &out.ServerTools},
		{path: "serverToolDownloads", dst: &out.ServerToolDownloads},
		{path: "serverToolPackaging", dst: &out.ServerToolPackaging},
		{path: "devTools", dst: &out.DevTools},
		{path: "guestVersions", dst: &out.GuestVersions},
	} {
		value, err := lookupConcrete(root, item.path)
		if err != nil {
			return err
		}
		if err := value.Decode(item.dst); err != nil {
			return fmt.Errorf("decode catalog.%s: %w", item.path, err)
		}
	}
	return nil
}

// canonicalDigest serialises the value directly through CUE's JSON encoder
// and hashes it. This avoids a Decode -> any -> encoding/json round-trip,
// where Go map-key ordering becomes part of the graph identity contract.
func canonicalDigest(val cue.Value) (string, []byte, error) {
	buf, err := val.MarshalJSON()
	if err != nil {
		return "", nil, fmt.Errorf("marshal CUE value for digest: %w", err)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), buf, nil
}

type namedBytes struct {
	name  string
	bytes []byte
}

func digestParts(parts ...namedBytes) string {
	h := sha256.New()
	for _, part := range parts {
		writePart(h, part.name, part.bytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writePart(h hash.Hash, name string, bytes []byte) {
	_, _ = fmt.Fprintf(h, "%s\x00%d\x00", name, len(bytes))
	_, _ = h.Write(bytes)
}
