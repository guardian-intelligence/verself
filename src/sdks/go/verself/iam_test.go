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
	testSignupID = "signup_01J8QK4M5N6P7Q8R9S0T1V2W3X"
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

func TestAuthClientUsesDeviceSessionAPI(t *testing.T) {
	const sessionID = "sess_01J8QK4M5N6P7Q8R9S0T1V2W3X"
	const otherSessionID = "sess_01J8QK4M5N6P7Q8R9S0T1V2W3Y"
	var createIDKey string
	var createBody map[string]any
	var revoked string
	var removed string

	authContextJSON := `{"account":{"accountId":"acct_01J8QK4M5N6P7Q8R9S0T1V2W3X","issuer":"https://verself.sh","subject":"user_test","email":"operator@example.test","emailVerified":true,"displayName":"Operator","createdAt":"2026-05-25T00:00:00Z"},"session":{"sessionId":"` + sessionID + `","accountId":"acct_01J8QK4M5N6P7Q8R9S0T1V2W3X","channel":"cli","state":"active","current":true,"deviceLabel":"CLI","createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z","expiresAt":"2026-05-26T00:00:00Z"},"organizations":[{"orgId":"` + testOrgID + `","displayName":"Guardian Intelligence","slug":"guardian","selected":true}],"selectedOrgId":"` + testOrgID + `","authTime":"2026-05-25T00:00:00Z","authMethods":["pwd"]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer tok_test" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost && r.Header.Get("X-Verself-Device-Session") != sessionID {
			t.Fatalf("device session header = %q", r.Header.Get("X-Verself-Device-Session"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/device-sessions":
			createIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(authContextJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth-context":
			_, _ = w.Write([]byte(authContextJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/device-sessions":
			_, _ = w.Write([]byte(`{"sessions":[{"sessionId":"` + sessionID + `","accountId":"acct_01J8QK4M5N6P7Q8R9S0T1V2W3X","channel":"cli","state":"active","current":true,"deviceLabel":"CLI","createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z","expiresAt":"2026-05-26T00:00:00Z"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/device-sessions/"+otherSessionID:
			revoked = otherSessionID
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/account-connections":
			_, _ = w.Write([]byte(`{"connections":[{"connectionId":"conn_01J8QK4M5N6P7Q8R9S0T1V2W3X","accountId":"acct_01J8QK4M5N6P7Q8R9S0T1V2W3X","issuer":"https://verself.sh","subject":"user_test","state":"linked","email":"operator@example.test","emailVerified":true,"createdAt":"2026-05-25T00:00:00Z","lastSeenAt":"2026-05-25T00:00:00Z"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/account-connections/conn_01J8QK4M5N6P7Q8R9S0T1V2W3X":
			removed = "conn_01J8QK4M5N6P7Q8R9S0T1V2W3X"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", DeviceSessionID: sessionID, IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.Auth.CreateDeviceSession(context.Background(), CreateDeviceSessionInput{
		Channel:        AuthChannelCLI,
		DeviceLabel:    "CLI",
		IdempotencyKey: "iam:session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createIDKey != "iam:session" || createBody["channel"] != "cli" || created.Session.SessionID != sessionID {
		t.Fatalf("unexpected device session create: key=%q body=%#v context=%#v", createIDKey, createBody, created)
	}
	authContext, err := client.Auth.GetContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if authContext.Account.Email != "operator@example.test" || authContext.SelectedOrgID != testOrgID {
		t.Fatalf("unexpected auth context: %#v", authContext)
	}
	sessions, err := client.Auth.ListDeviceSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
	if err := client.Auth.RevokeDeviceSession(context.Background(), otherSessionID); err != nil {
		t.Fatal(err)
	}
	if revoked != otherSessionID {
		t.Fatalf("revoked session = %q", revoked)
	}
	connections, err := client.Auth.ListAccountConnections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 || connections[0].ConnectionID == "" {
		t.Fatalf("unexpected connections: %#v", connections)
	}
	if err := client.Auth.RemoveAccountConnection(context.Background(), connections[0].ConnectionID); err != nil {
		t.Fatal(err)
	}
	if removed != connections[0].ConnectionID {
		t.Fatalf("removed connection = %q", removed)
	}
}

func TestIAMSignupUsesPublicAPI(t *testing.T) {
	var startIDKey string
	var startBody map[string]any
	var checkSlugPath string
	var verifyIDKey string
	var verifyBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/signup-intents":
			startIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&startBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"message":"Check your email to continue.","status":"accepted","verificationExpiresAt":"2026-05-24T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/organization-slugs/guardian-intelligence/availability":
			checkSlugPath = r.URL.Path
			_, _ = w.Write([]byte(`{"slug":"guardian-intelligence","available":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/signup-intents/"+testSignupID+"/verification":
			verifyIDKey = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&verifyBody); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"organization":` + organizationJSON("1") + `,"loginUrl":"https://verself.sh/login?required_email=operator%40example.test"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(Options{IAMURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	slug := "guardian-intelligence"
	availability, err := client.IAM.CheckOrganizationSlugAvailability(context.Background(), slug)
	if err != nil {
		t.Fatal(err)
	}
	if checkSlugPath == "" || !availability.Available || availability.Slug != slug {
		t.Fatalf("unexpected slug availability: path=%q result=%#v", checkSlugPath, availability)
	}
	started, err := client.IAM.StartSignup(context.Background(), StartSignupInput{
		Email:                   "operator@example.test",
		OrganizationDisplayName: "Guardian Intelligence",
		OrganizationSlug:        &slug,
		IdempotencyKey:          "iam:start-signup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if startIDKey != "iam:start-signup" {
		t.Fatalf("start idempotency key = %q", startIDKey)
	}
	if startBody["email"] != "operator@example.test" || startBody["organizationDisplayName"] != "Guardian Intelligence" || startBody["organizationSlug"] != slug {
		t.Fatalf("unexpected start body: %#v", startBody)
	}
	if started.Status != "accepted" || started.VerificationExpiresAt != "2026-05-24T12:00:00Z" {
		t.Fatalf("unexpected signup start result: %#v", started)
	}
	verifySlug := "guardian-labs"
	result, err := client.IAM.VerifySignup(context.Background(), VerifySignupInput{
		SignupIntentID:          testSignupID,
		VerificationToken:       "signup-verification-token-0000000001",
		InitialPassword:         "correct horse battery staple",
		OrganizationDisplayName: ptrString("Guardian Labs"),
		OrganizationSlug:        &verifySlug,
		IdempotencyKey:          "iam:verify-signup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if verifyIDKey != "iam:verify-signup" {
		t.Fatalf("verify idempotency key = %q", verifyIDKey)
	}
	if verifyBody["verificationToken"] != "signup-verification-token-0000000001" {
		t.Fatalf("unexpected verify body: %#v", verifyBody)
	}
	if verifyBody["initialPassword"] != "correct horse battery staple" {
		t.Fatalf("unexpected verify initial password body: %#v", verifyBody)
	}
	if verifyBody["organizationDisplayName"] != "Guardian Labs" {
		t.Fatalf("unexpected verify organization display name: %#v", verifyBody)
	}
	if verifyBody["organizationSlug"] != "guardian-labs" {
		t.Fatalf("unexpected verify organization slug: %#v", verifyBody)
	}
	if result.Organization.OrgID != testOrgID || result.LoginURL == "" {
		t.Fatalf("unexpected signup verification result: %#v", result)
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
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:iam:version-conflict","title":"Version conflict","status":409,"detail":"Organization version is stale.","code":"iam.organization.version_conflict","requestId":"req_test","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}`))
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
	if apiErr.Code != "iam.organization.version_conflict" || apiErr.RequestID != "req_test" || apiErr.Traceparent == "" {
		t.Fatalf("problem metadata was not preserved: %#v", apiErr)
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
