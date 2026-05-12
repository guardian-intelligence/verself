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
	var updateIDKey string
	var updateBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs":
			authHeader = r.Header.Get("Authorization")
			if r.URL.Query().Get("page_size") != "10" || r.URL.Query().Get("page_token") != "cursor-1" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"nextPageToken":"cursor-2","organizations":[` + organizationJSON("admin", "1", "3") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID:
			_, _ = w.Write([]byte(organizationJSON("admin", "1", "3")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/orgs/"+testOrgID:
			updateIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(organizationJSON("owner", "2", "4")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
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
	if organization.CallerRole != "admin" || organization.OrgACLVersion != 3 {
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
	if updated.CallerRole != "owner" || updated.Version != 2 {
		t.Fatalf("unexpected updated organization: %#v", updated)
	}
}

func TestIAMMembersUsePublicAPI(t *testing.T) {
	var roleIDKey string
	var roleBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members":
			if r.URL.Query().Get("page_size") != "25" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"members":[` + memberJSON("member") + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members/"+testMemberID:
			_, _ = w.Write([]byte(memberJSON("member")))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/orgs/"+testOrgID+"/members/"+testMemberID+"/role":
			roleIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&roleBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(memberJSON("admin")))
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
	if member.Role != "member" || member.Email != "operator@example.test" {
		t.Fatalf("unexpected member: %#v", member)
	}
	updated, err := client.IAM.UpdateMemberRole(context.Background(), UpdateMemberRoleInput{
		OrgID:                 testOrgID,
		MemberID:              testMemberID,
		Role:                  OrganizationRoleAdmin,
		ExpectedRole:          OrganizationRoleMember,
		ExpectedOrgACLVersion: 3,
		IdempotencyKey:        "iam:update-role",
	})
	if err != nil {
		t.Fatal(err)
	}
	if roleIDKey != "iam:update-role" {
		t.Fatalf("role idempotency key = %q", roleIDKey)
	}
	if roleBody["role"] != "admin" || roleBody["expectedRole"] != "member" || roleBody["expectedOrgAclVersion"] != float64(3) {
		t.Fatalf("unexpected role body: %#v", roleBody)
	}
	if updated.Role != "admin" {
		t.Fatalf("unexpected updated member: %#v", updated)
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

func organizationJSON(role string, version string, orgACLVersion string) string {
	return `{"orgId":"` + testOrgID + `","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/` + testOrgID + `","slug":"guardian-intelligence","displayName":"Guardian Intelligence","callerRole":"` + role + `","version":` + version + `,"orgAclVersion":` + orgACLVersion + `}`
}

func memberJSON(role string) string {
	return `{"orgId":"` + testOrgID + `","memberId":"` + testMemberID + `","resourceName":"urn:verself:inst_01J8QJ4P1R7S9W2X5M6N8P0Q2A:orgs/` + testOrgID + `/members/` + testMemberID + `","email":"operator@example.test","displayName":"Operator","role":"` + role + `"}`
}
