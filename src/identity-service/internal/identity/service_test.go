package identity

import (
	"errors"
	"testing"
	"time"
)

func TestValidateMemberCapabilitiesRejectsUnknownKeys(t *testing.T) {
	doc := DefaultMemberCapabilitiesDocument("42", "user-1", time.Unix(1700000000, 0).UTC())
	doc.EnabledKeys = append(doc.EnabledKeys, "unknown_capability")

	if err := ValidateMemberCapabilities(doc); !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected ErrInvalidCapabilities, got %v", err)
	}
}

func TestValidateMemberCapabilitiesRejectsDuplicates(t *testing.T) {
	doc := DefaultMemberCapabilitiesDocument("42", "user-1", time.Unix(1700000000, 0).UTC())
	if len(doc.EnabledKeys) == 0 {
		t.Fatal("default capability set should not be empty")
	}
	doc.EnabledKeys = append(doc.EnabledKeys, doc.EnabledKeys[0])

	if err := ValidateMemberCapabilities(doc); !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
}

func TestOwnerAndAdminGrantAllKnownPermissions(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42"}
	knownCount := len(KnownPermissions())

	for _, role := range []string{RoleOwner, RoleAdmin} {
		permissions := PermissionsForRoles(doc, []string{role})
		if len(permissions) != knownCount {
			t.Fatalf("role %q should grant %d permissions, got %d", role, knownCount, len(permissions))
		}
	}
}

func TestMemberPermissionsAreCapabilityDerived(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42", EnabledKeys: []string{}}
	baseline := PermissionsForRoles(doc, []string{RoleMember})

	for _, expected := range baselineMemberPermissions {
		if !contains(baseline, expected) {
			t.Fatalf("baseline member permissions missing %q (got %v)", expected, baseline)
		}
	}
	if contains(baseline, PermissionSandboxExecutionSubmit) {
		t.Fatal("member without deploy_executions capability must not hold sandbox:execution:submit")
	}

	doc.EnabledKeys = []string{"deploy_executions"}
	enabled := PermissionsForRoles(doc, []string{RoleMember})
	if !contains(enabled, PermissionSandboxExecutionSubmit) {
		t.Fatalf("deploy_executions should grant sandbox:execution:submit, got %v", enabled)
	}
}

func TestMemberCannotMintNonMemberEligiblePermission(t *testing.T) {
	doc := MemberCapabilitiesDocument{OrgID: "42", EnabledKeys: DefaultCapabilityKeys()}
	principal := Principal{Subject: "u-1", OrgID: "42", Roles: []string{RoleMember}}

	// api_credentials:create is admin-only (not member-eligible).
	err := validateCredentialPermissions(doc, principal, []string{PermissionAPICredentialsCreate})
	if !errors.Is(err, ErrInvalidCapabilities) {
		t.Fatalf("expected ErrInvalidCapabilities for member minting api_credentials:create, got %v", err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
