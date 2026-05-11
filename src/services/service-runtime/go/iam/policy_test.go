package iam

import (
	"net/http"
	"testing"
)

func TestOperationPolicyOpenAPIExtension(t *testing.T) {
	policy := OperationPolicy{
		Permission:     Permission("iam:api_credentials:create"),
		Resource:       ResourceKind("api_credential"),
		Action:         ActionCreate,
		OrgScope:       OrgScopeTokenOrgID,
		RateLimitClass: RateLimitClass("api_credential_mutation"),
		Idempotency:    IdempotencyHeaderKey,
		AuditEvent:     AuditEvent("iam.api_credential.create"),
		BodyLimitBytes: 16 << 10,
	}

	if err := policy.ValidateHTTPOperation(http.MethodPost, "create-api-credential"); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	extension := policy.OpenAPIExtension()
	for key, want := range map[string]any{
		"permission":             "iam:api_credentials:create",
		"resource":               "api_credential",
		"action":                 "create",
		"org_scope":              "token_org_id",
		"rate_limit_class":       "api_credential_mutation",
		"idempotency":            "idempotency_key_header",
		"audit_event":            "iam.api_credential.create",
		"request_body_max_bytes": int64(16 << 10),
	} {
		if got := extension[key]; got != want {
			t.Fatalf("extension[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestOperationPolicyValidationRejectsLooseValues(t *testing.T) {
	base := OperationPolicy{
		Permission:     Permission("iam:organization:read"),
		Resource:       ResourceKind("organization"),
		Action:         ActionRead,
		OrgScope:       OrgScopeTokenOrgID,
		RateLimitClass: RateLimitClass("read"),
		AuditEvent:     AuditEvent("iam.organization.read"),
	}

	tests := []struct {
		name   string
		method string
		mutate func(*OperationPolicy)
	}{
		{
			name: "permission namespace required",
			mutate: func(policy *OperationPolicy) {
				policy.Permission = Permission("organization_read")
			},
		},
		{
			name: "permission characters are constrained",
			mutate: func(policy *OperationPolicy) {
				policy.Permission = Permission("iam:Organization:read")
			},
		},
		{
			name: "resource kind is constrained",
			mutate: func(policy *OperationPolicy) {
				policy.Resource = ResourceKind("organization.member")
			},
		},
		{
			name: "action enum is closed",
			mutate: func(policy *OperationPolicy) {
				policy.Action = Action("mutate")
			},
		},
		{
			name: "org scope enum is closed",
			mutate: func(policy *OperationPolicy) {
				policy.OrgScope = OrgScope("token_org_name")
			},
		},
		{
			name: "idempotency enum is closed",
			mutate: func(policy *OperationPolicy) {
				policy.Idempotency = IdempotencyPolicy("custom_idempotency")
			},
		},
		{
			name: "audit event is dotted",
			mutate: func(policy *OperationPolicy) {
				policy.AuditEvent = AuditEvent("iam_organization_read")
			},
		},
		{
			name:   "mutating methods require body limit",
			method: http.MethodPost,
			mutate: func(policy *OperationPolicy) {
				policy.Action = ActionCreate
				policy.Idempotency = IdempotencyHeaderKey
			},
		},
		{
			name: "body limit cannot be negative",
			mutate: func(policy *OperationPolicy) {
				policy.BodyLimitBytes = -1
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := base
			tc.mutate(&policy)
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			if err := policy.ValidateHTTPOperation(method, "test-operation"); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestOperationRequiresBodyBudget(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, " post "} {
		if !OperationRequiresBodyBudget(method) {
			t.Fatalf("%q should require body budget", method)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, ""} {
		if OperationRequiresBodyBudget(method) {
			t.Fatalf("%q should not require body budget", method)
		}
	}
}
