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

func TestValidatePolicyRequiresOrgAdminToKeepIdentityPermissions(t *testing.T) {
	policy := DefaultPolicy("42", "user-1")
	for index, role := range policy.Roles {
		if role.RoleKey == RoleOrgAdmin {
			policy.Roles[index].Permissions = []string{PermissionOrganizationRead}
		}
	}

	if err := ValidatePolicy(policy); err == nil {
		t.Fatal("expected weakened org admin role to be rejected")
	}
}
