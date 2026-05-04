package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/ansible"
	"github.com/verself/deployment-tools/internal/bazelbuild"
	"github.com/verself/deployment-tools/internal/deploydb"
	"github.com/verself/deployment-tools/internal/deploymodel"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/supplychain"
)

const deployUnitsQuery = `kind("deploy_unit rule", //src/host-configuration/...)`

type deployUnitDescriptor struct {
	SchemaVersion int                `json:"schema_version"`
	Label         string             `json:"label"`
	UnitID        string             `json:"unit_id"`
	Executor      string             `json:"executor"`
	PayloadKind   string             `json:"payload_kind"`
	Args          []string           `json:"args"`
	Payload       deployUnitPayload  `json:"payload"`
	Sources       []deployUnitSource `json:"sources"`
	Dependencies  []string           `json:"dependencies"`
	digest        string             `json:"-"`
}

type deployUnitPayload struct {
	Phase    string `json:"phase"`
	Playbook string `json:"playbook"`
}

type deployUnitSource struct {
	Path      string `json:"path"`
	ShortPath string `json:"short_path"`
}

func buildDeployUnitDescriptors(ctx context.Context, repoRoot string) ([]string, []string, error) {
	labels, err := discoverDeployUnits(ctx, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	build, err := bazelbuild.Build(ctx, repoRoot, labels, "--config=remote-writer")
	if err != nil {
		return nil, nil, err
	}
	descriptorPaths := make([]string, 0, len(labels))
	for _, label := range labels {
		outputs, err := build.Stream.ResolveOutputs(label, repoRoot)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve %s deploy unit descriptor output: %w", label, err)
		}
		if len(outputs) != 1 {
			return nil, nil, fmt.Errorf("%s must produce exactly one deploy unit descriptor output, got %d: %v", label, len(outputs), outputs)
		}
		descriptorPaths = append(descriptorPaths, outputs[0])
	}
	return labels, descriptorPaths, nil
}

func discoverDeployUnits(ctx context.Context, repoRoot string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "bazelisk", "query", deployUnitsQuery)
	cmd.Dir = repoRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bazelisk query %s: %w: %s", deployUnitsQuery, err, strings.TrimSpace(stderr.String()))
	}
	var labels []string
	for _, line := range strings.Split(string(body), "\n") {
		label := strings.TrimSpace(line)
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return nil, fmt.Errorf("bazel query %s returned no deploy units", deployUnitsQuery)
	}
	return labels, nil
}

func loadDeployUnitDescriptors(paths []string) ([]deployUnitDescriptor, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one deploy unit descriptor is required")
	}
	units := make([]deployUnitDescriptor, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var unit deployUnitDescriptor
		if err := json.Unmarshal(body, &unit); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if unit.SchemaVersion != 1 {
			return nil, fmt.Errorf("%s: unsupported deploy unit schema_version=%d", path, unit.SchemaVersion)
		}
		if unit.Label == "" || unit.UnitID == "" || unit.Executor == "" || unit.PayloadKind == "" {
			return nil, fmt.Errorf("%s: deploy unit descriptor must include label, unit_id, executor, and payload_kind", path)
		}
		key := unit.Executor + "/" + unit.UnitID
		if seen[key] {
			return nil, fmt.Errorf("duplicate deploy unit %s", key)
		}
		seen[key] = true
		for _, source := range unit.Sources {
			if source.Path == "" || source.ShortPath == "" {
				return nil, fmt.Errorf("%s: deploy unit sources must include path and short_path", path)
			}
		}
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool {
		if units[i].Executor != units[j].Executor {
			return units[i].Executor < units[j].Executor
		}
		return units[i].UnitID < units[j].UnitID
	})
	return units, nil
}

func findDeployUnit(units []deployUnitDescriptor, executor, unitID string) (deployUnitDescriptor, bool) {
	for _, unit := range units {
		if unit.Executor == executor && unit.UnitID == unitID {
			return unit, true
		}
	}
	return deployUnitDescriptor{}, false
}

func convergeHostConfigurationUnit(ctx context.Context, rt *runtime.Runtime, span trace.Span, site, repoRoot string, units []deployUnitDescriptor) (bool, error) {
	unit, ok := findDeployUnit(units, deploydb.DeployExecutorAnsible, hostConfigurationComponent)
	if !ok {
		return false, fmt.Errorf("missing deploy unit %s/%s", deploydb.DeployExecutorAnsible, hostConfigurationComponent)
	}
	unitDigest, err := digestDeployUnit(repoRoot, site, unit)
	if err != nil {
		return false, err
	}
	unit.digest = unitDigest
	span.SetAttributes(
		attribute.String("host_configuration.unit_digest", unitDigest),
		attribute.String("host_configuration.unit_label", unit.Label),
	)
	return convergeAnsibleDeployUnit(ctx, rt, span, site, repoRoot, unit, func(args []string) (*ansible.Result, error) {
		return runHostConfigurationSitePlaybook(ctx, rt, site, repoRoot, args)
	})
}

func convergeAnsibleDeployUnit(
	ctx context.Context,
	rt *runtime.Runtime,
	span trace.Span,
	site, repoRoot string,
	unit deployUnitDescriptor,
	run func([]string) (*ansible.Result, error),
) (bool, error) {
	eventsReady, err := rt.DeployDB.HasDeployUnitEvents(ctx)
	if err != nil {
		return false, err
	}
	var last deploydb.DeployUnitLastSucceeded
	var haveLast bool
	if eventsReady {
		last, haveLast, err = rt.DeployDB.LastSucceededDeployUnit(ctx, site, unit.Executor, unit.UnitID)
		if err != nil {
			return false, err
		}
	}

	noOp := eventsReady && haveLast && last.DesiredDigest == unit.digest
	span.SetAttributes(
		attribute.Bool("deploy_unit.events_ready", eventsReady),
		attribute.Bool("deploy_unit.noop", noOp),
		attribute.String("deploy_unit.executor", unit.Executor),
		attribute.String("deploy_unit.unit_id", unit.UnitID),
	)
	if eventsReady {
		if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventDecided, last.DesiredDigest, noOp, 0, "")); err != nil {
			return false, err
		}
	}
	if noOp {
		fmt.Fprintf(os.Stderr, "verself-deploy: %s/%s already at desired digest; skipping %s\n",
			unit.Executor, unit.UnitID, unit.Payload.Playbook)
		if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventSkipped, last.DesiredDigest, true, 0, "")); err != nil {
			return false, err
		}
		span.SetAttributes(attribute.Bool("host_configuration.skipped", true))
		return false, nil
	}

	if !eventsReady {
		fmt.Fprintf(os.Stderr, "verself-deploy: deploy unit evidence table missing; running %s so ClickHouse migrations can converge\n", unit.Payload.Playbook)
	} else if haveLast {
		fmt.Fprintf(os.Stderr, "verself-deploy: %s/%s digest changed since run %s; running %s\n",
			unit.Executor, unit.UnitID, last.RunKey, unit.Payload.Playbook)
	} else {
		fmt.Fprintf(os.Stderr, "verself-deploy: no previous successful %s/%s unit; running %s\n",
			unit.Executor, unit.UnitID, unit.Payload.Playbook)
	}

	startedAt := time.Now()
	if eventsReady {
		if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventApplied, last.DesiredDigest, false, 0, "")); err != nil {
			return false, err
		}
	}
	res, runErr := run(unit.Args)
	durationMs := durationMillis(time.Since(startedAt), "ansible deploy unit duration")
	if runErr != nil || res == nil || res.ExitCode != 0 {
		msg := ansibleFailureMessage(unit.Payload.Playbook, res, runErr)
		if eventsReady {
			if recordErr := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventFailed, last.DesiredDigest, false, durationMs, msg)); recordErr != nil {
				return false, recordErr
			}
		}
		return true, fmt.Errorf("%s/%s failed: %s", unit.Executor, unit.UnitID, msg)
	}

	if !eventsReady {
		var readyErr error
		eventsReady, readyErr = rt.DeployDB.HasDeployUnitEvents(ctx)
		if readyErr != nil {
			return true, readyErr
		}
		if eventsReady {
			// The schema was created by this same Ansible run; backfill the unit-level lifecycle for this run.
			for _, kind := range []string{deploydb.DeployUnitEventDecided, deploydb.DeployUnitEventApplied} {
				if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, kind, "", false, 0, "")); err != nil {
					return true, err
				}
			}
		}
	}
	if eventsReady {
		if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventSucceeded, unit.digest, false, durationMs, "")); err != nil {
			return true, err
		}
	}
	span.SetAttributes(
		attribute.Bool("host_configuration.skipped", false),
		attribute.Int("ansible.task_count", res.TaskCount),
		attribute.Int("ansible.changed_total", res.ChangedCount),
		attribute.Int("ansible.failed_count", res.FailedCount),
	)
	return true, nil
}

func recordSecurityPatchDeployUnit(ctx context.Context, rt *runtime.Runtime, site, repoRoot string, units []deployUnitDescriptor, res securityPatchResult) error {
	unit, ok := findDeployUnit(units, deploydb.DeployExecutorSecurityPatch, securityPatchComponent)
	if !ok {
		return fmt.Errorf("missing deploy unit %s/%s", deploydb.DeployExecutorSecurityPatch, securityPatchComponent)
	}
	digest, err := digestDeployUnit(repoRoot, site, unit)
	if err != nil {
		return err
	}
	unit.digest = digest
	duration := res.EndedAt.Sub(res.StartedAt)
	if res.EndedAt.IsZero() || res.StartedAt.IsZero() || duration < 0 {
		duration = 0
	}
	if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventApplied, "", false, 0, "")); err != nil {
		return err
	}
	return rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventSucceeded, digest, false, durationMillis(duration, "security patch duration"), ""))
}

func recordSupplyChainDeployUnit(ctx context.Context, rt *runtime.Runtime, site, repoRoot string) error {
	digest, err := digestWorkspaceFiles(repoRoot, []deployUnitSource{{
		Path:      filepath.Join(repoRoot, filepath.FromSlash(supplychain.DefaultPolicyPath)),
		ShortPath: supplychain.DefaultPolicyPath,
	}}, nil)
	if err != nil {
		return err
	}
	unit := deployUnitDescriptor{
		UnitID:      supplyChainComponent,
		Executor:    deploydb.DeployExecutorSupplyChain,
		PayloadKind: "supply_chain_policy",
		digest:      digest,
	}
	if err := rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventApplied, "", false, 0, "")); err != nil {
		return err
	}
	return rt.DeployDB.RecordDeployUnitEvent(ctx, unitEvent(rt, site, unit, deploydb.DeployUnitEventSucceeded, digest, false, 0, ""))
}

func unitEvent(rt *runtime.Runtime, site string, unit deployUnitDescriptor, kind, observedDigest string, noOp bool, durationMs uint32, errorMessage string) deploydb.DeployUnitEvent {
	return deploydb.DeployUnitEvent{
		RunKey:          rt.Identity.RunKey(),
		Site:            site,
		Executor:        unit.Executor,
		UnitID:          unit.UnitID,
		Kind:            kind,
		DesiredDigest:   unit.digest,
		ObservedDigest:  observedDigest,
		NoOp:            noOp,
		DependencyUnits: append([]string(nil), unit.Dependencies...),
		PayloadKind:     unit.PayloadKind,
		DurationMs:      durationMs,
		ErrorMessage:    truncateError(errorMessage),
	}
}

func digestDeployUnit(repoRoot, site string, unit deployUnitDescriptor) (string, error) {
	extra := []deployDigestInput{{
		ShortPath: "src/host-configuration/sites/" + site + "/inventory.ini",
		Path:      authoredInventoryPath(repoRoot, site),
	}}
	return digestWorkspaceFiles(repoRoot, unit.Sources, extra)
}

type deployDigestInput struct {
	ShortPath string
	Path      string
}

func digestWorkspaceFiles(repoRoot string, sources []deployUnitSource, extra []deployDigestInput) (string, error) {
	inputs := make([]deployDigestInput, 0, len(sources)+len(extra))
	for _, source := range sources {
		inputs = append(inputs, deployDigestInput{
			ShortPath: source.ShortPath,
			Path:      resolveWorkspacePath(repoRoot, source.Path),
		})
	}
	inputs = append(inputs, extra...)
	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].ShortPath < inputs[j].ShortPath
	})

	hash := sha256.New()
	for _, input := range inputs {
		if input.ShortPath == "" || input.Path == "" {
			return "", fmt.Errorf("deploy digest input is incomplete: %#v", input)
		}
		body, err := os.ReadFile(input.Path)
		if err != nil {
			return "", fmt.Errorf("read deploy digest input %s: %w", input.ShortPath, err)
		}
		if _, err := io.WriteString(hash, input.ShortPath); err != nil {
			return "", err
		}
		if _, err := hash.Write([]byte{0}); err != nil {
			return "", err
		}
		bodyDigest := deploymodel.SHA256(body)
		if _, err := io.WriteString(hash, bodyDigest); err != nil {
			return "", err
		}
		if _, err := hash.Write([]byte{'\n'}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
