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
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/verself/cue-renderer/schema"
)

// Loaded is the in-memory snapshot every renderer receives. The raw
// Topology field carries every CUE value the graph projects; typed
// projections (Evidence, ...) are filled in alongside so renderers don't
// need to know how to walk a cue.Value.
//
// Today most of Topology is map[string]any because schema.cue still uses
// open records (`...`). As the schema tightens, gengotypes will produce
// real structs and this field becomes typed end-to-end without any
// renderer changing.
type Loaded struct {
	Topology schema.Topology
	Evidence []schema.Evidence

	// GraphSHA256 is the hex SHA-256 of the canonical-JSON encoding of
	// the topology value. Spans use it as a stable identifier for "the
	// shape of the world this run rendered against."
	GraphSHA256 string
}

// Topology compiles the CUE instance at `instance` (e.g.
// "./instances/local:topology") rooted at `dir` and decodes it. One
// cue.Value.Decode call hydrates the whole graph; one targeted Decode
// hydrates each typed projection on Loaded. Both share the same
// underlying CUE evaluation, so this remains a single graph walk.
func Topology(dir, instance string) (Loaded, error) {
	val, err := buildValue(dir, instance, "topology")
	if err != nil {
		return Loaded{}, err
	}

	var loaded Loaded
	if err := val.Decode(&loaded.Topology); err != nil {
		return Loaded{}, fmt.Errorf("decode topology: %w", err)
	}
	if err := val.LookupPath(cue.ParsePath("evidence")).Decode(&loaded.Evidence); err != nil {
		return Loaded{}, fmt.Errorf("decode topology.evidence: %w", err)
	}

	digest, err := canonicalDigest(val)
	if err != nil {
		return Loaded{}, err
	}
	loaded.GraphSHA256 = digest
	return loaded, nil
}

func buildValue(dir, instance, fieldExpr string) (cue.Value, error) {
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

// canonicalDigest serialises the value as deterministic JSON and hashes
// it. JSON gives us stable object-key ordering and matches the digest
// topology.py emits today, so dashboards keyed off topology.graph_sha256
// don't have to learn about the migration.
func canonicalDigest(val cue.Value) (string, error) {
	var v any
	if err := val.Decode(&v); err != nil {
		return "", fmt.Errorf("decode for digest: %w", err)
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal for digest: %w", err)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}
