// Package nomadclient is a typed wrapper around
// github.com/hashicorp/nomad/api: parse → plan → CAS-safe register →
// blocking-query monitor. Each step is a span.
//
// Resolved specs use `{"Job": {...}}` JSON envelopes, the same shape Nomad's
// HTTP API consumes for POST /v1/jobs. Authored source specs are owner-local
// HCL2 files parsed by the target Nomad server before artifact binding.
package nomadclient

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
)

// Spec is a resolved Nomad job spec plus the digests stamped into Job.Meta.
// The digests participate in the no-op decision:
// if the currently registered job already matches both, we skip the
// submit entirely rather than burn an evaluation on an identical spec.
type Spec struct {
	Job            *api.Job
	ArtifactDigest string
	SpecDigest     string
}

// LoadSpec reads a resolved Nomad JSON file and validates stamped metadata.
// Errors here surface the contract between resolver and submitter.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseSpec(data, path)
}

// ParseSpec validates a resolved Nomad job spec already held in memory.
func ParseSpec(data []byte, source string) (*Spec, error) {
	var env struct {
		Job *api.Job `json:"Job"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode %s: %w", source, err)
	}
	if env.Job == nil {
		return nil, fmt.Errorf("%s: missing top-level Job object", source)
	}
	if env.Job.ID == nil || *env.Job.ID == "" {
		return nil, fmt.Errorf("%s: Job.ID is required", source)
	}
	artifact := env.Job.Meta["artifact_sha256"]
	specDigest := env.Job.Meta["spec_sha256"]
	if artifact == "" || specDigest == "" {
		return nil, fmt.Errorf("%s: Job.Meta must include artifact_sha256 and spec_sha256", source)
	}
	return &Spec{
		Job:            env.Job,
		ArtifactDigest: artifact,
		SpecDigest:     specDigest,
	}, nil
}

// JobID returns the spec's job id. Provided as a method so callers
// don't need to defensively dereference *string from the api.Job.
func (s *Spec) JobID() string {
	if s == nil || s.Job == nil || s.Job.ID == nil {
		return ""
	}
	return *s.Job.ID
}

// shortDigest is the operator-friendly tail used in log lines and span
// attributes. Full digests stay in Job.Meta where they belong.
func shortDigest(d string) string {
	if len(d) < 12 {
		return d
	}
	return d[:12]
}
