package systemd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/projection"
	"github.com/verself/cue-renderer/internal/render/serviceenv"
)

type Renderer struct{}

func (Renderer) Name() string { return "systemd" }

func (Renderer) Render(_ context.Context, loaded load.Loaded, out render.WritableFS) error {
	components, err := projection.Components(loaded)
	if err != nil {
		return err
	}
	unitNames := map[string]struct{}{}
	for _, component := range components {
		// Skip nomad-supervised components: the Nomad renderer writes
		// the job spec, and the converge_component.yml supervisor branch
		// never touches /etc/systemd/system for them.
		if componentSupervisor(component) == "nomad" {
			continue
		}
		converge, ok := component.Value["converge"].(map[string]any)
		if !ok {
			continue
		}
		enabled, _ := converge["enabled"].(bool)
		if !enabled {
			continue
		}
		units, err := projection.Slice(converge, component.Name+".converge", "units")
		if err != nil {
			return err
		}
		for i, raw := range units {
			unit, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("%s.converge.units[%d]: expected map, got %T", component.Name, i, raw)
			}
			name, err := projection.String(unit, component.Name+".converge.units", "name")
			if err != nil {
				return err
			}
			unitNames[name] = struct{}{}
			body, err := renderUnit(component, unit)
			if err != nil {
				return fmt.Errorf("render %s: %w", name, err)
			}
			if err := out.WriteFile(unitPath(name), body); err != nil {
				return err
			}
		}
	}
	if err := out.WriteFile(handlerPath(), renderHandlers(unitNames)); err != nil {
		return err
	}
	return nil
}

func componentSupervisor(component projection.NamedMap) string {
	deployment, _ := component.Value["deployment"].(map[string]any)
	supervisor, _ := deployment["supervisor"].(string)
	if supervisor == "" {
		return "systemd"
	}
	return supervisor
}

func unitPath(name string) string {
	return projection.RenderedPath("/etc/systemd/system/" + name + ".service")
}

func handlerPath() string {
	return "handlers/component-systemd.yml"
}

func renderHandlers(unitNames map[string]struct{}) []byte {
	names := make([]string, 0, len(unitNames))
	for name := range unitNames {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("---\n")
	for i, name := range names {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- name: Restart %s\n", name)
		fmt.Fprintf(&b, "  listen: restart %s\n", name)
		b.WriteString("  ansible.builtin.systemd:\n")
		b.WriteString("    daemon_reload: true\n")
		fmt.Fprintf(&b, "    name: %s\n", name)
		b.WriteString("    state: restarted\n")
	}
	return []byte(b.String())
}

func renderUnit(component projection.NamedMap, unit map[string]any) ([]byte, error) {
	name := mustString(unit, "name")
	hardening := mustMap(unit, "hardening")
	environment, err := serviceenv.Unit(component, unit)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(projection.Header)
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", mustString(unit, "description"))
	writeJoined(&b, "After", mustStringSlice(unit, "after"))
	writeJoined(&b, "Requires", mustStringSlice(unit, "requires"))
	writeJoined(&b, "Wants", mustStringSlice(unit, "wants"))

	b.WriteString("\n[Service]\n")
	fmt.Fprintf(&b, "Type=%s\n", mustString(unit, "type"))
	fmt.Fprintf(&b, "User=%s\n", mustString(unit, "user"))
	fmt.Fprintf(&b, "Group=%s\n", mustString(unit, "group"))
	writeJoined(&b, "SupplementaryGroups", mustStringSlice(unit, "supplementary_groups"))
	for _, value := range mustStringSlice(unit, "bind_read_only_paths") {
		fmt.Fprintf(&b, "BindReadOnlyPaths=%s\n", value)
	}
	for _, credential := range mustMapSlice(unit, "load_credentials") {
		fmt.Fprintf(&b, "LoadCredential=%s:%s\n", mustString(credential, "name"), mustString(credential, "path"))
	}
	for _, key := range sortedStringKeys(environment) {
		fmt.Fprintf(&b, "Environment=%s=%s\n", key, environment[key])
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", mustString(unit, "exec"))
	fmt.Fprintf(&b, "Restart=%s\n", mustString(unit, "restart"))
	fmt.Fprintf(&b, "RestartSec=%d\n", mustInt(unit, "restart_sec"))
	fmt.Fprintf(&b, "CapabilityBoundingSet=%s\n", mustString(hardening, "capability_bounding_set"))
	writeBool(&b, "ProtectHome", hardening, "protect_home")
	fmt.Fprintf(&b, "ProtectSystem=%s\n", mustString(hardening, "protect_system"))
	for _, value := range mustStringSlice(hardening, "read_write_paths") {
		fmt.Fprintf(&b, "ReadWritePaths=%s\n", value)
	}
	writeBool(&b, "PrivateDevices", hardening, "private_devices")
	writeBool(&b, "PrivateTmp", hardening, "private_tmp")
	writeBool(&b, "ProtectClock", hardening, "protect_clock")
	writeBool(&b, "ProtectControlGroups", hardening, "protect_control_groups")
	writeBool(&b, "ProtectKernelLogs", hardening, "protect_kernel_logs")
	writeBool(&b, "ProtectKernelModules", hardening, "protect_kernel_modules")
	writeBool(&b, "ProtectKernelTunables", hardening, "protect_kernel_tunables")
	writeBool(&b, "LockPersonality", hardening, "lock_personality")
	writeBool(&b, "NoNewPrivileges", hardening, "no_new_privileges")
	writeJoined(&b, "RestrictAddressFamilies", mustStringSlice(hardening, "restrict_address_families"))
	if _, ok := hardening["restrict_namespaces"]; ok {
		writeBool(&b, "RestrictNamespaces", hardening, "restrict_namespaces")
	}
	writeBool(&b, "RestrictRealtime", hardening, "restrict_realtime")
	writeBool(&b, "RestrictSUIDSGID", hardening, "restrict_suid_sgid")
	fmt.Fprintf(&b, "SystemCallArchitectures=%s\n", mustString(hardening, "system_call_architectures"))
	fmt.Fprintf(&b, "UMask=%s\n", mustString(hardening, "umask"))

	b.WriteString("\n[Install]\n")
	writeJoined(&b, "WantedBy", mustStringSlice(unit, "wanted_by"))
	if name == "" {
		return nil, fmt.Errorf("unit name is empty")
	}
	return []byte(b.String()), nil
}

func writeJoined(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s=%s\n", key, strings.Join(values, " "))
}

func writeBool(b *strings.Builder, systemdKey string, values map[string]any, key string) {
	fmt.Fprintf(b, "%s=%s\n", systemdKey, boolString(mustBool(values, key)))
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func mustMap(values map[string]any, key string) map[string]any {
	out, _ := values[key].(map[string]any)
	return out
}

func mustMapSlice(values map[string]any, key string) []map[string]any {
	raw, _ := values[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}

func mustString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mustStringSlice(values map[string]any, key string) []string {
	raw, _ := values[key].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func mustInt(values map[string]any, key string) int64 {
	value, _ := projection.Int(values, "systemd", key)
	return value
}

func mustBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
