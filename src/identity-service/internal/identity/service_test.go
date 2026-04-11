package identity

import "testing"

func TestValidatePolicyRejectsUnknownRoleKeysUntilProjectRoleManagementExists(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	policy.Roles = append(policy.Roles, PolicyRole{
		RoleKey:     "custom_role",
		DisplayName: "Custom Role",
		Permissions: []string{
			PermissionOrganizationRead,
		},
	})

	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("expected unknown role key to be rejected")
	}
}

func TestValidatePolicyRejectsUnknownPermissions(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	policy.Roles[0].Permissions = append(policy.Roles[0].Permissions, "sandbox:vm:reboot")

	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("expected unknown permission to be rejected")
	}
}

func TestValidatePolicyAllowsEditableOrgAdminToBeConstrained(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	for index, role := range policy.Roles {
		if role.RoleKey == RoleOrgAdmin {
			policy.Roles[index].Permissions = []string{PermissionOrganizationRead}
		}
	}

	if err := ValidatePolicy(policy); err != nil {
		t.Fatalf("editable org admin role should be constrainable: %v", err)
	}
}

func TestValidatePolicyRejectsReservedForgeOrgOwnerRole(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	policy.Roles = append(policy.Roles, PolicyRole{
		RoleKey:     RoleForgeOrgOwner,
		DisplayName: "Forge Org Owner",
		Permissions: []string{
			PermissionOrganizationRead,
		},
	})

	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("expected reserved forge org owner role to be rejected from editable policy")
	}
}

func TestReservedForgeOrgOwnerGrantsAllKnownPermissions(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	for index, role := range policy.Roles {
		if role.RoleKey == RoleOrgAdmin {
			policy.Roles[index].Permissions = []string{PermissionOrganizationRead}
		}
	}

	permissions := PermissionsForRoles(policy, []string{RoleForgeOrgOwner})
	if len(permissions) != len(KnownPermissions()) {
		t.Fatalf("forge org owner permissions = %v, want all known permissions", permissions)
	}
}
