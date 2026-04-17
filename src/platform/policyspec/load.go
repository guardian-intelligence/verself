package policyspec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

// Load reads and validates every policy YAML file in sourceDir, returning a
// fully populated Spec. Any schema violation or cross-file inconsistency
// causes a single wrapped error — there is no "partial load" mode.
func Load(sourceDir string) (*Spec, error) {
	spec := &Spec{
		LoadedAt:  time.Now(),
		SourceDir: sourceDir,
	}

	if err := loadYAML(filepath.Join(sourceDir, "retention.yml"), &spec.Retention); err != nil {
		return nil, err
	}
	if err := loadYAML(filepath.Join(sourceDir, "subprocessors.yml"), &spec.Subprocessors); err != nil {
		return nil, err
	}
	if err := loadYAML(filepath.Join(sourceDir, "ropa.yml"), &spec.ROPA); err != nil {
		return nil, err
	}
	if err := loadYAML(filepath.Join(sourceDir, "contacts.yml"), &spec.Contacts); err != nil {
		return nil, err
	}
	if err := loadYAML(filepath.Join(sourceDir, "versions.yml"), &spec.Versions); err != nil {
		return nil, err
	}

	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func loadYAML(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("policyspec: read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("policyspec: parse %s: %w", path, err)
	}
	return nil
}

// validateSpec enforces the cross-file and cross-field invariants that the
// YAML decoder's shape check cannot express. Returns on the first failure so
// the caller gets a precise, line-addressable error.
func validateSpec(spec *Spec) error {
	stateKeys := map[string]struct{}{}
	for _, s := range spec.Retention.StateMachine {
		stateKeys[s.Key] = struct{}{}
	}
	required := []string{"active", "past_due", "suspended", "pending_deletion", "deleted"}
	for _, k := range required {
		if _, ok := stateKeys[k]; !ok {
			return fmt.Errorf("policyspec: retention.state_machine missing required key %q", k)
		}
	}

	for _, t := range spec.Retention.Transitions {
		if _, ok := stateKeys[t.From]; !ok {
			return fmt.Errorf("policyspec: transition %s→%s references unknown state %q", t.From, t.To, t.From)
		}
		if _, ok := stateKeys[t.To]; !ok {
			return fmt.Errorf("policyspec: transition %s→%s references unknown state %q", t.From, t.To, t.To)
		}
	}

	windowIDs := map[string]struct{}{}
	for _, w := range spec.Retention.Windows {
		windowIDs[w.ID] = struct{}{}
		for _, v := range []WindowValue{w.Active, w.PastDue, w.Suspended, w.PendingDeletion} {
			if err := validateWindowValue(w.ID, v); err != nil {
				return err
			}
		}
	}

	for _, a := range spec.ROPA.ProcessingActivities {
		if _, ok := windowIDs[a.RetentionRef]; !ok {
			return fmt.Errorf("policyspec: ropa.activity %s references unknown retention window %q", a.ID, a.RetentionRef)
		}
	}

	return nil
}

func validateWindowValue(windowID string, v WindowValue) error {
	switch v.Kind {
	case "preserved", "per_user_policy", "delete_with_parent", "not_provided":
		if v.Days != 0 || v.Years != 0 {
			return fmt.Errorf("policyspec: window %q value kind %q must not carry days/years", windowID, v.Kind)
		}
	case "delete_after", "ttl_days":
		if v.Days <= 0 {
			return fmt.Errorf("policyspec: window %q value kind %q requires positive days", windowID, v.Kind)
		}
		if v.Years != 0 {
			return fmt.Errorf("policyspec: window %q value kind %q must not carry years", windowID, v.Kind)
		}
	case "retain_years":
		if v.Years <= 0 {
			return fmt.Errorf("policyspec: window %q value kind %q requires positive years", windowID, v.Kind)
		}
		if v.Days != 0 {
			return fmt.Errorf("policyspec: window %q value kind %q must not carry days", windowID, v.Kind)
		}
	default:
		return fmt.Errorf("policyspec: window %q value has unknown kind %q", windowID, v.Kind)
	}
	return nil
}

// EmitBootSpan records a span summarizing what the policy source said at the
// moment of load. The span's attributes are the proof artifact: grepping
// ClickHouse for spans named "forge_metal.policy.check" shows every deploy's
// declared retention windows, alongside the deploy_id baggage attribute that
// the fmotel baggage processor attaches automatically.
func (s *Spec) EmitBootSpan(ctx context.Context) {
	tracer := otel.Tracer("github.com/forge-metal/platform-policyspec")
	attrs := []attribute.KeyValue{
		attribute.String("forge_metal.policy.source_dir", s.SourceDir),
		attribute.String("forge_metal.policy.retention.effective_at", s.Retention.EffectiveAt),
		attribute.Int("forge_metal.policy.retention.windows_count", len(s.Retention.Windows)),
		attribute.Int("forge_metal.policy.subprocessors_count", len(s.Subprocessors.Subprocessors)),
		attribute.Int("forge_metal.policy.ropa_activities_count", len(s.ROPA.ProcessingActivities)),
	}

	// Windows as structured attributes: one per window in "<id>.<state>=<kind>[:<n>]" shape.
	for _, w := range s.Retention.Windows {
		attrs = append(attrs,
			attribute.String("forge_metal.policy.window."+w.ID+".active", windowValueLabel(w.Active)),
			attribute.String("forge_metal.policy.window."+w.ID+".past_due", windowValueLabel(w.PastDue)),
			attribute.String("forge_metal.policy.window."+w.ID+".suspended", windowValueLabel(w.Suspended)),
			attribute.String("forge_metal.policy.window."+w.ID+".pending_deletion", windowValueLabel(w.PendingDeletion)),
		)
	}

	_, span := tracer.Start(ctx, "forge_metal.policy.check",
		trace.WithAttributes(attrs...),
	)
	span.End()
}

func windowValueLabel(v WindowValue) string {
	switch v.Kind {
	case "delete_after", "ttl_days":
		return fmt.Sprintf("%s:%dd", v.Kind, v.Days)
	case "retain_years":
		return fmt.Sprintf("%s:%dy", v.Kind, v.Years)
	default:
		return v.Kind
	}
}
