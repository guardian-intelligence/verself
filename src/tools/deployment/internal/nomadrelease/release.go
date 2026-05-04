// Package nomadrelease is the typed deploy contract between Bazel-built
// Nomad artifacts and the consume-only deploy path.
package nomadrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion = 2
	FileName      = "release.json"
)

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Release struct {
	SchemaVersion    int              `json:"schema_version"`
	Site             string           `json:"site"`
	SHA              string           `json:"sha"`
	PublishedAt      string           `json:"published_at,omitempty"`
	ComponentsQuery  string           `json:"components_query"`
	ArtifactDelivery ArtifactDelivery `json:"artifact_delivery"`
	Artifacts        []Artifact       `json:"artifacts"`
	Jobs             []Job            `json:"jobs"`
	SubmitOrder      []string         `json:"submit_order"`
}

type ArtifactDelivery struct {
	Bucket               string            `json:"bucket"`
	GetterSourcePrefix   string            `json:"getter_source_prefix"`
	GetterOptions        map[string]string `json:"getter_options"`
	PublisherCredentials Credentials       `json:"publisher_credentials"`
	Origin               Origin            `json:"origin"`
}

type Origin struct {
	Scheme       string `json:"scheme"`
	Hostname     string `json:"hostname"`
	Port         int    `json:"port"`
	CABundlePath string `json:"ca_bundle_path"`
}

type Credentials struct {
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
}

type Artifact struct {
	Output        string            `json:"output"`
	LocalPath     string            `json:"local_path,omitempty"`
	SHA256        string            `json:"sha256"`
	Bucket        string            `json:"bucket"`
	Key           string            `json:"key"`
	GetterSource  string            `json:"getter_source"`
	GetterOptions map[string]string `json:"getter_options,omitempty"`
}

type Job struct {
	JobID          string          `json:"job_id"`
	SpecSHA256     string          `json:"spec_sha256"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	Spec           json.RawMessage `json:"spec"`
}

func Load(path string) (*Release, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read nomad release: %w", err)
	}
	return Decode(body)
}

func Decode(body []byte) (*Release, error) {
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse nomad release: %w", err)
	}
	if err := release.Validate(""); err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *Release) Validate(expectedSHA string) error {
	if r == nil {
		return fmt.Errorf("nomad release is nil")
	}
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("nomad release schema_version=%d, want %d", r.SchemaVersion, SchemaVersion)
	}
	if r.Site == "" {
		return fmt.Errorf("nomad release site is required")
	}
	if r.SHA != "" && !sha40.MatchString(r.SHA) {
		return fmt.Errorf("nomad release sha must be 40 lowercase hex characters: %q", r.SHA)
	}
	if expectedSHA != "" && r.SHA != expectedSHA {
		return fmt.Errorf("nomad release sha=%s, want %s", r.SHA, expectedSHA)
	}
	if r.ComponentsQuery == "" {
		return fmt.Errorf("nomad release components_query is required")
	}
	if r.ArtifactDelivery.Bucket == "" || r.ArtifactDelivery.GetterSourcePrefix == "" {
		return fmt.Errorf("nomad release artifact_delivery is incomplete")
	}
	artifacts := map[string]bool{}
	for _, artifact := range r.Artifacts {
		if artifact.Output == "" || artifact.SHA256 == "" || artifact.Bucket == "" || artifact.Key == "" || artifact.GetterSource == "" {
			return fmt.Errorf("nomad release artifact is incomplete: %q", artifact.Output)
		}
		if artifacts[artifact.Output] {
			return fmt.Errorf("nomad release duplicate artifact output %q", artifact.Output)
		}
		artifacts[artifact.Output] = true
	}
	jobs := map[string]bool{}
	for _, job := range r.Jobs {
		if job.JobID == "" || job.SpecSHA256 == "" || job.ArtifactSHA256 == "" || len(job.Spec) == 0 {
			return fmt.Errorf("nomad release job is incomplete: %q", job.JobID)
		}
		if jobs[job.JobID] {
			return fmt.Errorf("nomad release duplicate job_id %q", job.JobID)
		}
		jobs[job.JobID] = true
	}
	if len(r.SubmitOrder) != len(r.Jobs) {
		return fmt.Errorf("nomad release submit_order has %d entries, jobs has %d", len(r.SubmitOrder), len(r.Jobs))
	}
	for _, jobID := range r.SubmitOrder {
		if !jobs[jobID] {
			return fmt.Errorf("nomad release submit_order references unknown job %q", jobID)
		}
	}
	return nil
}

func (r *Release) StampForPublish(sha string, publishedAt time.Time) error {
	if !sha40.MatchString(sha) {
		return fmt.Errorf("nomad release publish sha must be 40 lowercase hex characters: %q", sha)
	}
	r.SHA = sha
	r.PublishedAt = publishedAt.UTC().Format(time.RFC3339Nano)
	for i := range r.Artifacts {
		r.Artifacts[i].LocalPath = ""
	}
	return r.Validate(sha)
}

func (r *Release) Encode() ([]byte, error) {
	if err := r.Validate(r.SHA); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode nomad release: %w", err)
	}
	body = append(body, '\n')
	return body, nil
}

func (r *Release) JobByID(jobID string) (Job, bool) {
	for _, job := range r.Jobs {
		if job.JobID == jobID {
			return job, true
		}
	}
	return Job{}, false
}

func (a Artifact) ResolveLocalPath(repoRoot string) string {
	if filepath.IsAbs(a.LocalPath) {
		return a.LocalPath
	}
	return filepath.Join(repoRoot, a.LocalPath)
}

func ReleaseKey(site, sha string) string {
	return "releases/" + site + "/" + sha + "/" + FileName
}

func ReleaseArtifact(delivery ArtifactDelivery, site, sha string) Artifact {
	key := ReleaseKey(site, sha)
	return Artifact{
		Output:       "nomad-release",
		Bucket:       delivery.Bucket,
		Key:          key,
		GetterSource: strings.TrimRight(delivery.GetterSourcePrefix, "/") + "/" + key,
	}
}

func SHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
