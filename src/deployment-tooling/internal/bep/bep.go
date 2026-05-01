// Package bep parses Bazel's Build Event Protocol from the JSON-line
// stream produced by `--build_event_json_file=<path>`.
//
// The proto schema is large; this consumer cares about exactly two
// event kinds: NamedSetOfFiles (the file-set deduplication primitive)
// and TargetComplete (the per-target build outcome with refs into
// those file sets). Everything else is skipped.
//
// The official BEP examples warn about quadratic traversal of
// NamedSetOfFiles graphs in large builds, so we index sets by id once
// at parse time and dereference in O(1) when resolving outputs.
//
// See: https://bazel.build/remote/bep
package bep

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Stream is the parsed result of one BEP file. Keep what the deploy
// flow needs; drop the rest.
type Stream struct {
	// NamedSets maps the BEP-assigned set id to its files + transitive
	// references. Walked recursively when resolving outputs.
	NamedSets map[string]NamedSetOfFiles

	// Targets is keyed by Bazel label (configuration is collapsed
	// onto the label since we only ever build one configuration per
	// invocation; a Phase 4 follow-up will widen this if needed).
	Targets map[string]TargetComplete
}

// NamedSetOfFiles captures the relevant fields of a `namedSetOfFiles`
// payload. FileSets are transitive references into other named sets.
type NamedSetOfFiles struct {
	Files    []File
	FileSets []string
}

// File is a single output file. URI is the canonical
// `file:///abs/path` shape; we expose AbsolutePath() to extract the
// filesystem path callers actually want.
type File struct {
	Name       string
	URI        string
	PathPrefix []string
	Digest     string
}

// AbsolutePath converts URI ("file:///abs/path") to the absolute
// filesystem path. With remote-cache configurations Bazel may emit
// `bytestream://` URIs instead — for those, reconstruct from the
// (pathPrefix + name) tuple anchored at workspaceRoot.
//
// Use WorkspacePath when the workspace anchor is known; AbsolutePath
// is a convenience for tests that don't have one.
func (f File) AbsolutePath() string {
	u, err := url.Parse(f.URI)
	if err == nil && u.Scheme == "file" {
		return u.Path
	}
	// Without a workspace anchor we have no good answer for
	// bytestream URIs; surface the URI to make the failure obvious.
	return f.URI
}

// WorkspacePath resolves the file under workspaceRoot using the
// pathPrefix + name tuple Bazel emits in BEP. Works for both file:
// and bytestream: URI variants.
func (f File) WorkspacePath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return f.AbsolutePath()
	}
	parts := append([]string{workspaceRoot}, f.PathPrefix...)
	parts = append(parts, f.Name)
	return strings.Join(parts, string(filepath.Separator))
}

// TargetComplete captures the relevant fields of a `completed`
// payload, indexed by label.
type TargetComplete struct {
	Label        string
	Success      bool
	OutputGroups []OutputGroup
}

// OutputGroup names a logical group of file sets — "default" is the
// one nomad-deploy and friends care about; aspect-defined groups
// (testlogs, debug_info, ...) live alongside it and we ignore them.
type OutputGroup struct {
	Name     string
	FileSets []string
}

// Parse reads the JSON-line BEP at path and returns an indexed
// Stream. Lines that don't carry a NamedSetOfFiles or TargetComplete
// payload are skipped silently.
func Parse(path string) (*Stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bep: %w", err)
	}
	defer f.Close()

	stream := &Stream{
		NamedSets: make(map[string]NamedSetOfFiles),
		Targets:   make(map[string]TargetComplete),
	}

	scanner := bufio.NewScanner(f)
	// BEP lines can be very large (full options blocks, structured
	// command lines, etc). Bump the buffer past bufio's default 64 KiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw rawEvent
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("decode bep line: %w", err)
		}
		switch {
		case raw.ID.NamedSet != nil && raw.NamedSetOfFiles != nil:
			stream.NamedSets[raw.ID.NamedSet.ID] = decodeNamedSet(raw.NamedSetOfFiles)
		case raw.ID.TargetCompleted != nil && raw.Completed != nil:
			tc := decodeTargetComplete(raw.ID.TargetCompleted.Label, raw.Completed)
			stream.Targets[tc.Label] = tc
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read bep: %w", err)
	}
	return stream, nil
}

// ResolveOutputs returns the workspace-anchored filesystem paths of
// the files in the "default" output group of the given target.
// Transitive NamedSetOfFiles references are walked once and cached;
// cycles (which the Bazel scheduler does not produce but a malformed
// stream could) are tolerated.
//
// workspaceRoot is the directory `bazelisk` was invoked from. It
// anchors `bazel-out`, the symlink the path-prefix tuple refers to.
// BEP files generated under a remote-cache configuration carry
// `bytestream://` URIs instead of `file:///`; this resolver still
// returns valid local paths for them by composing pathPrefix + name.
func (s *Stream) ResolveOutputs(label, workspaceRoot string) ([]string, error) {
	tc, ok := s.Targets[label]
	if !ok {
		return nil, fmt.Errorf("bep: no targetCompleted event for %s", label)
	}
	if !tc.Success {
		return nil, fmt.Errorf("bep: %s target_completed.success=false", label)
	}
	for _, og := range tc.OutputGroups {
		if og.Name != "default" {
			continue
		}
		seen := make(map[string]struct{})
		paths := make([]string, 0, 16)
		for _, setID := range og.FileSets {
			s.collectFiles(setID, workspaceRoot, seen, &paths)
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("bep: %s default output group is empty", label)
		}
		return paths, nil
	}
	return nil, fmt.Errorf("bep: %s has no default output group", label)
}

// FailedTargets returns labels with completed.success=false, suitable
// for surfacing as a span attribute.
func (s *Stream) FailedTargets() []string {
	var out []string
	for label, tc := range s.Targets {
		if !tc.Success {
			out = append(out, label)
		}
	}
	return out
}

// CountNamedSets returns the cardinality of the indexed file-set map;
// useful for span attributes (`bep.named_set_count`).
func (s *Stream) CountNamedSets() int { return len(s.NamedSets) }

// CountTargetCompletes returns the cardinality of indexed targets;
// useful for span attributes (`bep.target_complete_count`).
func (s *Stream) CountTargetCompletes() int { return len(s.Targets) }

// ErrEmpty is returned when the BEP file has no relevant events.
var ErrEmpty = errors.New("bep: no NamedSetOfFiles or TargetComplete events found")

func (s *Stream) collectFiles(setID, workspaceRoot string, seen map[string]struct{}, out *[]string) {
	if _, ok := seen[setID]; ok {
		return
	}
	seen[setID] = struct{}{}
	set, ok := s.NamedSets[setID]
	if !ok {
		return
	}
	for _, f := range set.Files {
		*out = append(*out, f.WorkspacePath(workspaceRoot))
	}
	for _, child := range set.FileSets {
		s.collectFiles(child, workspaceRoot, seen, out)
	}
}

// rawEvent is the line-level JSON shape BEP emits. Field tags follow
// Bazel's wire format (camelCase) which json.Unmarshal honours.
type rawEvent struct {
	ID        rawEventID         `json:"id"`
	Completed *rawCompleted      `json:"completed,omitempty"`
	// "namedSetOfFiles" is the canonical field; no abbreviation.
	NamedSetOfFiles *rawNamedSetOfFiles `json:"namedSetOfFiles,omitempty"`
}

type rawEventID struct {
	NamedSet        *rawNamedSetID        `json:"namedSet,omitempty"`
	TargetCompleted *rawTargetCompletedID `json:"targetCompleted,omitempty"`
}

type rawNamedSetID struct {
	ID string `json:"id"`
}

type rawTargetCompletedID struct {
	Label string `json:"label"`
}

type rawNamedSetOfFiles struct {
	Files    []rawFile         `json:"files"`
	FileSets []rawNamedSetID   `json:"fileSets"`
}

type rawFile struct {
	Name       string   `json:"name"`
	URI        string   `json:"uri"`
	PathPrefix []string `json:"pathPrefix"`
	Digest     string   `json:"digest"`
}

type rawCompleted struct {
	Success      bool             `json:"success"`
	OutputGroups []rawOutputGroup `json:"outputGroup"`
}

type rawOutputGroup struct {
	Name     string          `json:"name"`
	FileSets []rawNamedSetID `json:"fileSets"`
}

func decodeNamedSet(raw *rawNamedSetOfFiles) NamedSetOfFiles {
	out := NamedSetOfFiles{Files: make([]File, 0, len(raw.Files))}
	for _, f := range raw.Files {
		out.Files = append(out.Files, File{
			Name:       f.Name,
			URI:        f.URI,
			PathPrefix: f.PathPrefix,
			Digest:     f.Digest,
		})
	}
	if len(raw.FileSets) > 0 {
		out.FileSets = make([]string, 0, len(raw.FileSets))
		for _, fs := range raw.FileSets {
			out.FileSets = append(out.FileSets, fs.ID)
		}
	}
	return out
}

func decodeTargetComplete(label string, raw *rawCompleted) TargetComplete {
	out := TargetComplete{Label: label, Success: raw.Success}
	if len(raw.OutputGroups) == 0 {
		return out
	}
	out.OutputGroups = make([]OutputGroup, 0, len(raw.OutputGroups))
	for _, og := range raw.OutputGroups {
		og2 := OutputGroup{Name: og.Name}
		for _, fs := range og.FileSets {
			og2.FileSets = append(og2.FileSets, fs.ID)
		}
		out.OutputGroups = append(out.OutputGroups, og2)
	}
	return out
}
