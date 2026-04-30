// codegen-canary asserts that the OpenAPI codegen Bazel action graph
// behaves the way the deploy depends on it behaving:
//
//  1. Mutating an upstream Huma-route source file (in a way that
//     observably changes the rendered spec) propagates through every
//     downstream action — OpenAPISpec for each format, OAPICodegen
//     for each client. The canary asserts each expected spawn is
//     present in the post-mutation execution log with cacheHit=false.
//
//  2. Reverting the source restores cache hits on the same spawns —
//     i.e. the action key is purely a function of inputs, with no
//     incidental state leakage that would force unnecessary re-runs.
//
// Both halves matter. (1) catches a missing dep edge that would let a
// deploy ship without regenerating consumers; (2) catches a non-
// hermetic action that would re-run on every build.
//
// The mutation strategy is a string substitution in the source file,
// targeting a literal whose value flows into the rendered YAML (e.g.
// the Huma API version). A comment-only edit would not work: Bazel's
// content-addressed action cache correctly observes that the compiled
// .a file is byte-identical and shortcuts the whole downstream chain.
// We want the canary to fail loudly when the graph is broken, so the
// mutation must produce an end-to-end visible byte difference.
//
// The canary edits real source. It uses an in-memory backup, a
// deferred restore, and a final `git diff --exit-code` assertion to
// keep the working tree clean even on failure paths. Each invocation
// uses a unique nonce so action keys are guaranteed-novel — the
// "post-mutation was not a cache hit" assertion is meaningful
// regardless of whether a prior canary ran.
//
// Usage:
//
//	codegen-canary [--target=<name>|--target=all]
//	  --target=billing  # default
//	  --target=all      # run every manifest entry
//
// Exit code is 0 on success, 1 on any assertion failure.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// canaryEntry binds an upstream Huma-routes source file to the
// downstream Bazel actions that must rebuild when the rendered spec
// changes, and the (mnemonic, target) pairs the canary asserts.
type canaryEntry struct {
	Name string

	// SourceFile is workspace-relative. The canary substitutes
	// MutationReplace for every occurrence of MutationFind and
	// rebuilds.
	SourceFile      string
	MutationFind    string
	MutationReplace string // Contains a single %s for the nonce.

	// BuildTargets are the Bazel labels the canary asks bazelisk to
	// build — typically the leaf consumers of SourceFile (so the
	// build pulls every intermediate codegen action through the
	// action graph).
	BuildTargets []string

	// ExpectedSpawns is the set of (mnemonic, targetLabel) pairs that
	// MUST appear in the execution log on the post-mutation build
	// with cacheHit=false, and on the post-revert build with
	// cacheHit=true.
	ExpectedSpawns []expectedSpawn
}

type expectedSpawn struct {
	Mnemonic    string
	TargetLabel string
}

// manifest is the source of truth for which graph edges the canary
// guards. New service cutovers add an entry here in the same PR that
// flips the service's BUILD files to verself_openapi_yaml /
// verself_openapi_go_client.
//
// The MutationFind literal for billing is the API version embedded in
// `huma.DefaultConfig("Billing Service", version)` via
// `Config{Version: "2.0.0"}`. Changing the literal changes the
// `info.version` field of the rendered OpenAPI YAML, which propagates
// through OpenAPISpec → OAPICodegen end-to-end.
var manifest = []canaryEntry{
	{
		Name:            "billing",
		SourceFile:      "src/billing-service/internal/billingapi/api.go",
		MutationFind:    `Config{Version: "2.0.0"}`,
		MutationReplace: `Config{Version: "2.0.0+canary-%s"}`,
		BuildTargets: []string{
			"//src/billing-service/openapi:spec_3_0",
			"//src/billing-service/openapi:spec_3_1",
			"//src/billing-service/client:client",
		},
		ExpectedSpawns: []expectedSpawn{
			{Mnemonic: "OpenAPISpec", TargetLabel: "//src/billing-service/openapi:spec_3_0"},
			{Mnemonic: "OpenAPISpec", TargetLabel: "//src/billing-service/openapi:spec_3_1"},
			{Mnemonic: "OAPICodegen", TargetLabel: "//src/billing-service/client:client_gen"},
		},
	},
}

func main() {
	target := flag.String("target", "billing", "Manifest entry to run (or 'all').")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	if err != nil {
		fail("locate repo root: %v", err)
	}

	entries, err := selectEntries(*target)
	if err != nil {
		fail("%v", err)
	}

	failures := 0
	for _, entry := range entries {
		fmt.Fprintf(os.Stderr, "codegen-canary: %s — start\n", entry.Name)
		if err := runEntry(repoRoot, entry); err != nil {
			fmt.Fprintf(os.Stderr, "codegen-canary: %s — FAIL: %v\n", entry.Name, err)
			failures++
			continue
		}
		fmt.Fprintf(os.Stderr, "codegen-canary: %s — ok\n", entry.Name)
	}
	if failures > 0 {
		fail("%d canary entry(ies) failed", failures)
	}
}

func selectEntries(target string) ([]canaryEntry, error) {
	if target == "all" {
		return manifest, nil
	}
	for _, e := range manifest {
		if e.Name == target {
			return []canaryEntry{e}, nil
		}
	}
	names := make([]string, 0, len(manifest))
	for _, e := range manifest {
		names = append(names, e.Name)
	}
	return nil, fmt.Errorf("unknown --target=%q; known: %s, all", target, strings.Join(names, ", "))
}

// runEntry executes the warm/mutate/build/revert/build sequence for
// one manifest entry and asserts the cache-hit invariants on the two
// post-action builds.
func runEntry(repoRoot string, entry canaryEntry) (retErr error) {
	// Refuse to mutate a file with uncommitted changes — otherwise an
	// in-flight edit by a parallel actor could be dropped on the floor
	// when the in-memory backup is restored.
	if err := assertGitClean(repoRoot, entry.SourceFile); err != nil {
		return fmt.Errorf("source must be clean before canary runs: %w", err)
	}

	srcPath := filepath.Join(repoRoot, entry.SourceFile)
	original, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read source %s: %w", entry.SourceFile, err)
	}
	if !strings.Contains(string(original), entry.MutationFind) {
		return fmt.Errorf("mutation literal %q not present in %s; manifest is stale",
			entry.MutationFind, entry.SourceFile)
	}

	defer func() {
		// Restore the original bytes regardless of how we exit; assert
		// that git agrees the file is clean to catch any silent corruption.
		if writeErr := os.WriteFile(srcPath, original, 0o644); writeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("restore %s: %w", entry.SourceFile, writeErr))
			return
		}
		if diffErr := assertGitClean(repoRoot, entry.SourceFile); diffErr != nil {
			retErr = errors.Join(retErr, diffErr)
		}
	}()

	// Step 1 — warm cache for the build targets at the original source.
	// Without this, the post-revert build would be cold and the cache_hit
	// assertion would not distinguish "graph is clean" from "everything
	// happened to compile fast on cold cache".
	if _, err := bazelBuild(repoRoot, entry.BuildTargets); err != nil {
		return fmt.Errorf("warm build: %w", err)
	}

	// Step 2 — mutate with a unique nonce, build, assert no cache hits
	// on the expected spawns.
	nonce := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	mutation := []byte(strings.ReplaceAll(string(original), entry.MutationFind,
		fmt.Sprintf(entry.MutationReplace, nonce)))
	if err := os.WriteFile(srcPath, mutation, 0o644); err != nil {
		return fmt.Errorf("write mutation: %w", err)
	}
	mutLog, err := bazelBuild(repoRoot, entry.BuildTargets)
	if err != nil {
		return fmt.Errorf("post-mutation build: %w", err)
	}
	if err := assertSpawnState(mutLog, entry.ExpectedSpawns, false); err != nil {
		return fmt.Errorf("post-mutation: %w", err)
	}

	// Step 3 — restore source via the deferred path early, then build
	// again and assert every expected spawn is a cache hit.
	if err := os.WriteFile(srcPath, original, 0o644); err != nil {
		return fmt.Errorf("restore source for cache-hit build: %w", err)
	}
	revLog, err := bazelBuild(repoRoot, entry.BuildTargets)
	if err != nil {
		return fmt.Errorf("post-revert build: %w", err)
	}
	if err := assertSpawnState(revLog, entry.ExpectedSpawns, true); err != nil {
		return fmt.Errorf("post-revert: %w", err)
	}
	return nil
}

// bazelBuild runs `bazelisk build --execution_log_json_file=<tmp>` for
// the given labels and returns the parsed spawns. The canary
// deliberately omits --config=remote{,-writer} so its outputs neither
// read from nor pollute the shared bazel-remote cache.
func bazelBuild(repoRoot string, targets []string) ([]spawnExec, error) {
	logFile, err := os.CreateTemp("", "codegen-canary-execlog-*.json")
	if err != nil {
		return nil, fmt.Errorf("temp log: %w", err)
	}
	logFile.Close()
	defer os.Remove(logFile.Name())

	args := []string{
		"build",
		"--profile=/dev/null",
		"--execution_log_json_file=" + logFile.Name(),
	}
	args = append(args, targets...)
	cmd := exec.Command("bazelisk", args...)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("bazelisk %s: %w", strings.Join(args, " "), err)
	}
	return parseExecLog(logFile.Name())
}

// spawnExec mirrors the protojson rendering of bazel's SpawnExec
// (subset). Kept local rather than importing the bazel-execlog-to-otel
// package to avoid a cross-module dep just for these fields.
type spawnExec struct {
	Mnemonic    string `json:"mnemonic"`
	TargetLabel string `json:"targetLabel"`
	Runner      string `json:"runner"`
	CacheHit    bool   `json:"cacheHit"`
	ExitCode    int32  `json:"exitCode"`
}

func parseExecLog(path string) ([]spawnExec, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var out []spawnExec
	for {
		var sp spawnExec
		if err := dec.Decode(&sp); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode spawn %d: %w", len(out), err)
		}
		out = append(out, sp)
	}
	return out, nil
}

// assertSpawnState asserts every expected (mnemonic, target) pair is
// present in the execution log with the given cache-hit state.
func assertSpawnState(log []spawnExec, expected []expectedSpawn, wantCacheHit bool) error {
	for _, want := range expected {
		var found *spawnExec
		for i := range log {
			sp := &log[i]
			if sp.Mnemonic == want.Mnemonic && sp.TargetLabel == want.TargetLabel {
				found = sp
				break
			}
		}
		if found == nil {
			return fmt.Errorf("expected spawn (%s, %s) not found in execution log",
				want.Mnemonic, want.TargetLabel)
		}
		if found.CacheHit != wantCacheHit {
			return fmt.Errorf("spawn (%s, %s) cache_hit=%t, want %t (runner=%q)",
				want.Mnemonic, want.TargetLabel, found.CacheHit, wantCacheHit, found.Runner)
		}
	}
	return nil
}

func assertGitClean(repoRoot, relPath string) error {
	cmd := exec.Command("git", "diff", "--exit-code", "--", relPath)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git diff %s: working tree not clean (%w)", relPath, err)
	}
	return nil
}

// findRepoRoot resolves the workspace root. When invoked via
// `bazelisk run`, the binary's cwd is the runfiles tree under
// bazel-bin and the workspace lives at $BUILD_WORKSPACE_DIRECTORY
// (set by bazel run wrappers). When invoked directly, walk up from
// cwd looking for MODULE.bazel.
func findRepoRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("BUILD_WORKSPACE_DIRECTORY")); root != "" {
		return root, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "MODULE.bazel")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no MODULE.bazel found in any parent of cwd")
		}
		dir = parent
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "codegen-canary: "+format+"\n", args...)
	os.Exit(1)
}
