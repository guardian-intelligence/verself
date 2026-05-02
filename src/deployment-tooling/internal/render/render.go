// Package render is the typed contract with the resolved per-site Nomad job
// directory. The resolver materialises three files the deploy flow depends on:
//
//	publish.json — every Garage artifact to upload, with sha256 + key
//	submit.tsv  — (job_id, spec_file) pairs, one per line
//	*.nomad.json — rendered Nomad job specs referenced by submit.tsv
//
// The structs here parse those files; they intentionally cover the
// fields verself-deploy reads and stop short of mirroring everything
// the renderer might emit.
package render

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is the parsed publish.json.
type Manifest struct {
	ArtifactDelivery ArtifactDelivery `json:"artifact_delivery"`
	Artifacts        []Artifact       `json:"artifacts"`
}

// ArtifactDelivery describes how the controller uploads to Garage.
type ArtifactDelivery struct {
	Bucket               string            `json:"bucket"`
	GetterSourcePrefix   string            `json:"getter_source_prefix"`
	GetterOptions        map[string]string `json:"getter_options"`
	PublisherCredentials Credentials       `json:"publisher_credentials"`
	Origin               Origin            `json:"origin"`
}

// Origin captures the artifact server's loopback-on-host shape.
type Origin struct {
	Scheme       string `json:"scheme"`
	Hostname     string `json:"hostname"`
	Port         int    `json:"port"`
	CABundlePath string `json:"ca_bundle_path"`
}

// Credentials points the publisher at a controller-side env file
// that exposes (access_key, secret_key) under the named env vars.
// The file is read remotely (sudo cat) by the SSH client.
type Credentials struct {
	EnvironmentFile    string `json:"environment_file"`
	AccessKeyIDEnv     string `json:"access_key_id_env"`
	SecretAccessKeyEnv string `json:"secret_access_key_env"`
}

// Artifact is one row of publish.json.
type Artifact struct {
	Output       string `json:"output"`
	LocalPath    string `json:"local_path"`
	SHA256       string `json:"sha256"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	GetterSource string `json:"getter_source"`
}

// SubmitEntry is one row of submit.tsv.
type SubmitEntry struct {
	JobID    string
	SpecFile string
}

// LoadManifest parses publish.json from the given path.
func LoadManifest(path string) (*Manifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// LoadSubmit parses submit.tsv and returns its rows in file order.
// Empty lines (the existing renderer can emit a trailing blank) are
// dropped quietly; malformed rows fail loud.
func LoadSubmit(path string) ([]SubmitEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open submit manifest: %w", err)
	}
	defer f.Close()

	var entries []SubmitEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			return nil, fmt.Errorf("submit manifest %s: malformed line %q (want job_id<TAB>spec_file)", path, line)
		}
		jobID, specFile := fields[0], fields[1]
		if jobID == "" || specFile == "" {
			return nil, fmt.Errorf("submit manifest %s: empty job_id or spec_file in %q", path, line)
		}
		entries = append(entries, SubmitEntry{JobID: jobID, SpecFile: specFile})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read submit manifest: %w", err)
	}
	return entries, nil
}

// ResolveLocalPath returns the absolute path to an artifact's source
// file on disk. LocalPath in the manifest is repo-relative; an absolute
// value is honoured as-is (the renderer occasionally produces those).
func (a Artifact) ResolveLocalPath(repoRoot string) string {
	if filepath.IsAbs(a.LocalPath) {
		return a.LocalPath
	}
	return filepath.Join(repoRoot, a.LocalPath)
}
