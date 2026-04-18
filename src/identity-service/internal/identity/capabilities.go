package identity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Capability is a fixed, code-owned bundle of permissions an organization
// admin can grant to members through a single human-labeled toggle. The
// catalog is intentionally not customer-editable: adding or changing a
// capability is a code change. Per-member overrides are not modeled — every
// member of an organization sees the same enabled set.
type Capability struct {
	Key            string
	Label          string
	Description    string
	DefaultEnabled bool
	Permissions    []string
}

// baselineMemberPermissions are granted to every member regardless of which
// capability keys are enabled. They cover the read-only "see your own org"
// surface that exists at the moment a user is added to an organization.
var baselineMemberPermissions = []string{
	PermissionOrganizationRead,
	PermissionMemberRead,
	PermissionMemberCapabilitiesRead,
}

var defaultCapabilities = []Capability{
	{
		Key:            "deploy_executions",
		Label:          "Deploy executions",
		Description:    "Submit sandbox executions, read their status, and stream their logs.",
		DefaultEnabled: true,
		Permissions: []string{
			PermissionSandboxExecutionSubmit,
			PermissionSandboxExecutionRead,
			PermissionSandboxLogsRead,
		},
	},
	{
		Key:            "view_volumes",
		Label:          "View volumes",
		Description:    "List durable volumes and read their current state.",
		DefaultEnabled: true,
		Permissions: []string{
			PermissionSandboxVolumeRead,
		},
	},
	{
		// Members can extend an invite, but only admins/owners can change
		// what role another member holds. PermissionMemberRolesWrite is
		// intentionally not member-eligible (catalog.go) and the init()
		// invariant below enforces it cannot leak into a capability bundle.
		Key:            "invite_members",
		Label:          "Invite members",
		Description:    "Invite new users to the organization. Changing an existing member's role stays admin-only.",
		DefaultEnabled: true,
		Permissions: []string{
			PermissionMemberInvite,
		},
	},
	{
		Key:            "view_billing",
		Label:          "View billing",
		Description:    "Read the organization's billing entitlements, statements, grants, and contracts.",
		DefaultEnabled: false,
		Permissions: []string{
			PermissionBillingRead,
		},
	},
}

// init validates the capability catalog at process start. Any drift between
// defaultCapabilities, baselineMemberPermissions, and the member-eligible
// operations declared in catalog.go is a developer-time bug — fail loud rather
// than serve a confused authorization model.
func init() {
	known := KnownPermissions()
	eligible := memberEligiblePermissions()
	covered := map[string]struct{}{}
	seenKeys := map[string]struct{}{}

	check := func(source, permission string) {
		if _, ok := known[permission]; !ok {
			panic(fmt.Sprintf("identity: %s references unknown permission %q", source, permission))
		}
		if _, ok := eligible[permission]; !ok {
			panic(fmt.Sprintf("identity: %s references non-member-eligible permission %q", source, permission))
		}
		covered[permission] = struct{}{}
	}

	for _, permission := range baselineMemberPermissions {
		check("baselineMemberPermissions", permission)
	}
	for _, capability := range defaultCapabilities {
		key := strings.TrimSpace(capability.Key)
		if key == "" {
			panic("identity: capability key is required")
		}
		if _, duplicate := seenKeys[key]; duplicate {
			panic(fmt.Sprintf("identity: duplicate capability key %q", key))
		}
		seenKeys[key] = struct{}{}
		if strings.TrimSpace(capability.Label) == "" {
			panic(fmt.Sprintf("identity: capability %q missing label", key))
		}
		if len(capability.Permissions) == 0 {
			panic(fmt.Sprintf("identity: capability %q must bundle at least one permission", key))
		}
		for _, permission := range capability.Permissions {
			check(fmt.Sprintf("capability %q", key), permission)
		}
	}

	for permission := range eligible {
		if _, ok := covered[permission]; !ok {
			panic(fmt.Sprintf("identity: member-eligible permission %q is not granted by any capability or baseline", permission))
		}
	}
}

// DefaultCapabilities returns a copy of the static capability catalog.
func DefaultCapabilities() []Capability {
	out := make([]Capability, len(defaultCapabilities))
	for i, capability := range defaultCapabilities {
		capability.Permissions = append([]string(nil), capability.Permissions...)
		out[i] = capability
	}
	return out
}

// CapabilityForKey returns the capability with the matching key.
func CapabilityForKey(key string) (Capability, bool) {
	for _, capability := range defaultCapabilities {
		if capability.Key == key {
			capability.Permissions = append([]string(nil), capability.Permissions...)
			return capability, true
		}
	}
	return Capability{}, false
}

// DefaultCapabilityKeys returns the keys of capabilities flagged as enabled
// by default. The order matches the catalog declaration so that
// MemberCapabilitiesDocument's enabled_keys remains stable for new orgs.
func DefaultCapabilityKeys() []string {
	out := []string{}
	for _, capability := range defaultCapabilities {
		if capability.DefaultEnabled {
			out = append(out, capability.Key)
		}
	}
	return out
}

// DefaultMemberCapabilitiesDocument returns the document a brand-new
// organization receives before any admin opens the editor.
func DefaultMemberCapabilitiesDocument(orgID, actor string, now time.Time) MemberCapabilitiesDocument {
	return MemberCapabilitiesDocument{
		OrgID:       orgID,
		Version:     0,
		EnabledKeys: DefaultCapabilityKeys(),
		UpdatedAt:   now,
		UpdatedBy:   actor,
	}
}

// ResolveMemberPermissions returns the permission set granted to a member
// caller given the organization's enabled capability keys. Unknown keys are
// ignored — validation runs at write time, not at read.
func ResolveMemberPermissions(enabledKeys []string) []string {
	granted := map[string]struct{}{}
	for _, permission := range baselineMemberPermissions {
		granted[permission] = struct{}{}
	}
	enabled := map[string]struct{}{}
	for _, key := range enabledKeys {
		enabled[key] = struct{}{}
	}
	for _, capability := range defaultCapabilities {
		if _, ok := enabled[capability.Key]; !ok {
			continue
		}
		for _, permission := range capability.Permissions {
			granted[permission] = struct{}{}
		}
	}
	out := make([]string, 0, len(granted))
	for permission := range granted {
		out = append(out, permission)
	}
	sort.Strings(out)
	return out
}
