package verself

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testOrgID    = "org_01J8QK0M2A7W4H3P9FQ6G1R8ZT"
	testMemberID = "member_01J8QK4M5N6P7Q8R9S0T1V2W3X"
)

func TestIAMOrganizationsUsePublicAPI(t *testing.T) {
	var authHeader string
	var createIDKey string
	var createBody map[string]any
	var updateIDKey string
	var updateBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs":
			createIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(organizationJSON("1")))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs":
			authHeader = r.Header.Get("Authorization")
			if r.URL.Query().Get("page_size") != "10" || r.URL.Query().Get("page_token") != "cursor-1" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"nextPageToken":"cursor-2","organizations":[` + organizationJSON("1") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID:
			_, _ = w.Write([]byte(organizationJSON("1")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/orgs/"+testOrgID:
			updateIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(organizationJSON("2")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.IAM.CreateOrganization(context.Background(), CreateOrganizationInput{
		DisplayName:    "Guardian Intelligence",
		Slug:           ptrString("guardian-intelligence"),
		IdempotencyKey: "iam:create-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createIDKey != "iam:create-org" {
		t.Fatalf("create idempotency key = %q", createIDKey)
	}
	if createBody["displayName"] != "Guardian Intelligence" || createBody["slug"] != "guardian-intelligence" {
		t.Fatalf("unexpected create body: %#v", createBody)
	}
	if created.OrgID != testOrgID {
		t.Fatalf("unexpected created organization: %#v", created)
	}
	page, err := client.IAM.ListOrganizations(context.Background(), ListOrganizationsOptions{
		PageSize:  10,
		PageToken: "cursor-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer tok_test" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
	if page.NextPageToken != "cursor-2" || len(page.Organizations) != 1 || page.Organizations[0].OrgID != testOrgID {
		t.Fatalf("unexpected organization page: %#v", page)
	}
	organization, err := client.IAM.GetOrganization(context.Background(), testOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if organization.Version != 1 || organization.DisplayName != "Guardian Intelligence" {
		t.Fatalf("unexpected organization: %#v", organization)
	}
	displayName := "Guardian Intelligence"
	slug := "guardian-intelligence"
	updated, err := client.IAM.UpdateOrganization(context.Background(), UpdateOrganizationInput{
		OrgID:          testOrgID,
		Version:        1,
		DisplayName:    &displayName,
		Slug:           &slug,
		IdempotencyKey: "iam:update-org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updateIDKey != "iam:update-org" {
		t.Fatalf("update idempotency key = %q", updateIDKey)
	}
	if updateBody["version"] != float64(1) || updateBody["displayName"] != displayName || updateBody["slug"] != slug {
		t.Fatalf("unexpected update body: %#v", updateBody)
	}
	if updated.Version != 2 {
		t.Fatalf("unexpected updated organization: %#v", updated)
	}
}

func TestIAMMembersUsePublicAPI(t *testing.T) {
	var inviteIDKey string
	var inviteBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members":
			if r.URL.Query().Get("page_size") != "25" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"members":[` + memberJSON() + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members/"+testMemberID:
			_, _ = w.Write([]byte(memberJSON()))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members:invite":
			inviteIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&inviteBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(invitationJSON()))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.IAM.ListMembers(context.Background(), ListMembersOptions{
		OrgID:    testOrgID,
		PageSize: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Members) != 1 || page.Members[0].MemberID != testMemberID {
		t.Fatalf("unexpected member page: %#v", page)
	}
	member, err := client.IAM.GetMember(context.Background(), GetMemberInput{
		OrgID:    testOrgID,
		MemberID: testMemberID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if member.Email != "operator@example.test" || member.DisplayName != "Operator" {
		t.Fatalf("unexpected member: %#v", member)
	}
	invitation, err := client.IAM.InviteMember(context.Background(), InviteMemberInput{
		OrgID:          testOrgID,
		Email:          "operator@example.test",
		Roles:          []IAMRoleName{IAMRoleAdmin, IAMRoleMember},
		IdempotencyKey: "iam:invite",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inviteIDKey != "iam:invite" {
		t.Fatalf("invite idempotency key = %q", inviteIDKey)
	}
	if inviteBody["email"] != "operator@example.test" {
		t.Fatalf("unexpected invite body: %#v", inviteBody)
	}
	if roles, ok := inviteBody["roles"].([]any); !ok || len(roles) != 2 {
		t.Fatalf("unexpected invite roles body: %#v", inviteBody)
	}
	if invitation.Status != "invited" || len(invitation.Roles) != 2 {
		t.Fatalf("unexpected invitation: %#v", invitation)
	}
}

func TestIAMPoliciesUsePublicAPI(t *testing.T) {
	var setIDKey string
	var setBody map[string]any
	var testBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/iamPolicy":
			_, _ = w.Write([]byte(policyJSON("etag-1")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/iamPolicy:set":
			setIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&setBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(policyJSON("etag-2")))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/iamPolicy:testPermissions":
			if err := json.NewDecoder(r.Body).Decode(&testBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"permissions":["iam:organization:read"]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := client.IAM.GetIamPolicy(context.Background(), testOrgID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Etag != "etag-1" || len(policy.Bindings) != 1 || policy.Bindings[0].Role != IAMRoleOwner {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	updated, err := client.IAM.SetIamPolicy(context.Background(), SetIamPolicyInput{
		OrgID:          testOrgID,
		Policy:         policy,
		IdempotencyKey: "iam:set-policy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if setIDKey != "iam:set-policy" {
		t.Fatalf("set policy idempotency key = %q", setIDKey)
	}
	if setBody["policy"] == nil {
		t.Fatalf("missing set policy body: %#v", setBody)
	}
	if updated.Etag != "etag-2" {
		t.Fatalf("unexpected updated policy: %#v", updated)
	}
	allowed, err := client.IAM.TestIamPermissions(context.Background(), TestIamPermissionsInput{
		OrgID:       testOrgID,
		Permissions: []string{"iam:organization:read", "iam:policy:set"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed) != 1 || allowed[0] != "iam:organization:read" {
		t.Fatalf("unexpected permissions: %#v", allowed)
	}
	if _, ok := testBody["permissions"].([]any); !ok {
		t.Fatalf("unexpected test body: %#v", testBody)
	}
}

func TestIAMNormalizesProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/"+testOrgID {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:iam:version-conflict","title":"Version conflict","status":409,"detail":"Organization version is stale."}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.IAM.GetOrganization(context.Background(), testOrgID)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error = %#v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict || apiErr.Title != "Version conflict" || apiErr.Detail != "Organization version is stale." {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

func organizationJSON(version string) string {
	return `{"orgId":"` + testOrgID + `","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/` + testOrgID + `","slug":"guardian-intelligence","displayName":"Guardian Intelligence","version":` + version + `}`
}

func memberJSON() string {
	return `{"orgId":"` + testOrgID + `","memberId":"` + testMemberID + `","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/` + testOrgID + `/members/` + testMemberID + `","email":"operator@example.test","displayName":"Operator"}`
}

func invitationJSON() string {
	return `{"orgId":"` + testOrgID + `","memberId":"` + testMemberID + `","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/` + testOrgID + `/members/` + testMemberID + `","email":"operator@example.test","status":"invited","roles":["roles/admin","roles/member"]}`
}

func policyJSON(etag string) string {
	return `{"version":1,"etag":"` + etag + `","bindings":[{"role":"roles/owner","members":["user:acct_01J8QK4M5N6P7Q8R9S0T1V2W3X"]}]}`
}

func ptrString(value string) *string {
	return &value
}
