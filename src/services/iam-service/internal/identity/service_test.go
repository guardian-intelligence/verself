package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var testEmailIdentityKey = []byte("01234567890123456789012345678901")

func TestServiceMembersHidesMachineUsers(t *testing.T) {
	directory := &fakeMembersDirectory{members: []Member{
		{UserID: "u1", Type: MemberTypeHuman, Email: "owner@example.test", DisplayName: "Owner"},
		{UserID: "u2", Type: MemberTypeHuman, Email: "agent@example.test", DisplayName: "Agent"},
		{UserID: "u3", Type: MemberTypeMachine, LoginName: "ci-bot", DisplayName: "ci-bot"},
	}}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
	}

	got, err := svc.Members(context.Background(), Principal{Subject: "u2", OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2"})
	if err != nil {
		t.Fatalf("Members: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 visible members, got %d: %#v", len(got), got)
	}
	for _, member := range got {
		if member.Type == MemberTypeMachine {
			t.Fatalf("machine user leaked into members table: %#v", member)
		}
	}
}

func TestAccessibleOrganizationsUsesAuthorizationGraph(t *testing.T) {
	svc := &Service{
		Store: fakeMembersStore{},
		AuthorizationGraph: fakeMembersAuthz{
			orgIDs: []string{"org_01J8QJ4P1R7S9W2X5M6N8P0Q2"},
		},
	}

	got, err := svc.AccessibleOrganizations(context.Background(), AuthorizationSubject{Kind: AuthorizationSubjectKindUser, ID: "user-1"})
	if err != nil {
		t.Fatalf("AccessibleOrganizations: %v", err)
	}
	if len(got) != 1 || got[0].OrgID != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" {
		t.Fatalf("unexpected organizations: %#v", got)
	}
}

func TestAccessibleOrganizationsIgnoresMissingMetadata(t *testing.T) {
	svc := &Service{
		Store: partialMetadataStore{
			available: map[string]OrganizationMetadata{
				"org_live": {OrgID: "org_live", IdentityProviderOrgID: "42", DisplayName: "Live", Slug: "live", Version: 1},
			},
		},
		AuthorizationGraph: fakeMembersAuthz{
			orgIDs: []string{"org_missing", "org_live"},
		},
	}

	got, err := svc.AccessibleOrganizations(context.Background(), AuthorizationSubject{Kind: AuthorizationSubjectKindUser, ID: "user-1"})
	if err != nil {
		t.Fatalf("AccessibleOrganizations: %v", err)
	}
	if len(got) != 1 || got[0].OrgID != "org_live" {
		t.Fatalf("unexpected organizations: %#v", got)
	}
}

func TestServiceCreateOrganizationCreatesDirectoryOrgAndProfile(t *testing.T) {
	store := &fakeSignupStore{}
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:        store,
		Directory:    directory,
		PolicyWriter: fakeOwnerPolicyWriter{},
		Billing:      fakeBillingProvisioner{},
	}

	got, err := svc.CreateOrganization(context.Background(), "user-1", PublicCreateOrganizationRequest{
		DisplayName:    "  Acme   Labs  ",
		Slug:           "Acme_Labs",
		IdempotencyKey: "signup-key",
	})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if !strings.HasPrefix(got.OrgID, "org_") || got.Slug != "acme-labs" || got.DisplayName != "Acme Labs" || got.Version != 1 {
		t.Fatalf("unexpected created organization: %#v", got)
	}
	if directory.createInput.AdminUserID != "user-1" || !strings.HasPrefix(directory.createInput.Name, "acme-labs-") {
		t.Fatalf("unexpected directory create input: %#v", directory.createInput)
	}
	if store.created.OrgID != got.OrgID || store.created.IdentityProviderOrgID != "43" || store.created.ActorID != "user-1" {
		t.Fatalf("unexpected stored profile input: %#v", store.created)
	}
}

func TestServiceVerifySignupMaterializesOrganizationPolicyAndBilling(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	directory := &fakeMembersDirectory{}
	policy := &recordingOwnerPolicyWriter{}
	billing := &recordingBillingProvisioner{}
	svc := &Service{
		Store:            store,
		Directory:        directory,
		PolicyWriter:     policy,
		Billing:          billing,
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}

	start, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            " Founder@Example.Test ",
		OrganizationName: "  Acme   Labs  ",
		OrganizationSlug: "Acme_Labs",
		GivenName:        "Ada",
		FamilyName:       "Lovelace",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup: %v", err)
	}
	if start.VerificationToken == "" || start.Intent.EmailDelivery != "Founder@example.test" || start.Intent.RequestedOrganizationSlug != "acme-labs" {
		t.Fatalf("unexpected signup start result: %#v", start)
	}

	got, err := svc.VerifySignup(context.Background(), VerifySignupRequest{
		SignupIntentID:          start.Intent.SignupIntentID,
		VerificationToken:       start.VerificationToken,
		OrganizationDisplayName: "  Acme Production  ",
		OrganizationSlug:        "Acme_Prod",
		IdempotencyKey:          "signup-verify-1",
	})
	if err != nil {
		t.Fatalf("VerifySignup: %v", err)
	}
	if got.Organization.OrgID != start.Intent.OrgID || got.Organization.Slug != "acme-prod" {
		t.Fatalf("unexpected materialized organization: %#v", got.Organization)
	}
	if directory.signupInput.OrgID != "43" || directory.signupInput.Email != "Founder@example.test" {
		t.Fatalf("unexpected signup user request: %#v", directory.signupInput)
	}
	if got := strings.Join(store.steps, ","); got != "zitadel_organization,zitadel_user,iam_organization,spicedb_owner_policy,billing_organization" {
		t.Fatalf("materialization steps = %s", got)
	}
	if policy.input.OrgID != got.Organization.OrgID || policy.input.OwnerUserID != "signup-user" {
		t.Fatalf("unexpected owner policy request: %#v", policy.input)
	}
	if directory.createInput.Name == "" || !strings.Contains(directory.createInput.Name, "acme-prod") {
		t.Fatalf("unexpected provider organization request: %#v", directory.createInput)
	}
	if store.created.DisplayName != "Acme Production" || got.Organization.DisplayName != "Acme Production" {
		t.Fatalf("finalized organization display name was not materialized: created=%#v got=%#v", store.created, got.Organization)
	}
	if billing.input.OrgID != got.Organization.OrgID || billing.input.DisplayName != "Acme Production" || billing.input.TrustTier != "new" {
		t.Fatalf("unexpected billing provisioning request: %#v", billing.input)
	}
	if !store.completed {
		t.Fatalf("signup intent was not completed")
	}
	if event := store.lastEvent("iam.signup_intent.completed"); event.Step != "completed" || event.State != SignupIntentStateCompleted || event.OrgID != got.Organization.OrgID {
		t.Fatalf("unexpected completion event: %#v", event)
	}
}

func TestServiceStartSignupResendsExistingPendingIntent(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	svc := &Service{
		Store:            store,
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}

	first, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Labs",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup first: %v", err)
	}
	now = now.Add(signupVerificationResendInterval + time.Second)
	second, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Production",
		OrganizationSlug: "acme-prod",
		IdempotencyKey:   "signup-start-2",
	})
	if err != nil {
		t.Fatalf("StartSignup second: %v", err)
	}
	if second.Intent.SignupIntentID != first.Intent.SignupIntentID {
		t.Fatalf("resend created a new signup intent: first=%s second=%s", first.Intent.SignupIntentID, second.Intent.SignupIntentID)
	}
	if second.VerificationToken == "" || second.VerificationToken == first.VerificationToken {
		t.Fatalf("resend did not rotate the verification token")
	}
	if !second.SendVerificationEmail() || second.CreatedIntent() {
		t.Fatalf("unexpected resend flags: %#v", second)
	}
	if second.Outcome != SignupStartOutcomeVerificationResent {
		t.Fatalf("unexpected resend outcome: %#v", second)
	}
	if second.Intent.OrganizationDisplayName != "Acme Production" || second.Intent.RequestedOrganizationSlug != "acme-prod" {
		t.Fatalf("resend did not update pending signup details: %#v", second.Intent)
	}
	if second.Intent.EmailDelivery != "founder@example.test" {
		t.Fatalf("resend should preserve submitted delivery address: %#v", second.Intent)
	}
	if event := store.lastEvent("iam.signup_intent.verification_resent"); event.Outcome != "resent" || event.SignupIntentID != first.Intent.SignupIntentID {
		t.Fatalf("missing resend event: %#v", event)
	}
}

func TestServiceStartSignupTreatsSubaddressAsSameEmailIdentity(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	svc := &Service{
		Store:            store,
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}

	first, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder+agent@example.test",
		OrganizationName: "Acme Labs",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup first: %v", err)
	}
	now = now.Add(signupVerificationResendInterval + time.Second)
	second, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "Founder@example.test",
		OrganizationName: "Acme Production",
		IdempotencyKey:   "signup-start-2",
	})
	if err != nil {
		t.Fatalf("StartSignup second: %v", err)
	}
	if second.Intent.SignupIntentID != first.Intent.SignupIntentID {
		t.Fatalf("subaddress signup created another intent: first=%s second=%s", first.Intent.SignupIntentID, second.Intent.SignupIntentID)
	}
	if second.Outcome != SignupStartOutcomeVerificationResent || second.VerificationToken == "" {
		t.Fatalf("subaddress signup should rotate the existing verification: %#v", second)
	}
	if second.Intent.EmailDelivery != "Founder@example.test" {
		t.Fatalf("resend should use the latest submitted delivery address: %#v", second.Intent)
	}
}

func TestServiceStartSignupSuppressesRapidPendingResend(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	svc := &Service{
		Store:            store,
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}

	first, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Labs",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup first: %v", err)
	}
	now = now.Add(30 * time.Second)
	second, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Production",
		OrganizationSlug: "acme-prod",
		IdempotencyKey:   "signup-start-2",
	})
	if err != nil {
		t.Fatalf("StartSignup second: %v", err)
	}
	if second.Intent.SignupIntentID != first.Intent.SignupIntentID {
		t.Fatalf("rapid resend changed signup intent: first=%s second=%s", first.Intent.SignupIntentID, second.Intent.SignupIntentID)
	}
	if second.VerificationToken != "" || second.SendVerificationEmail() {
		t.Fatalf("rapid resend should not enqueue another verification email: %#v", second)
	}
	if second.Outcome != SignupStartOutcomeVerificationRecentlySent {
		t.Fatalf("unexpected rapid resend outcome: %#v", second)
	}
	if second.Intent.OrganizationDisplayName != "Acme Labs" || second.Intent.RequestedOrganizationSlug != "" {
		t.Fatalf("rapid resend should leave the existing link details intact: %#v", second.Intent)
	}
	if event := store.lastEvent("iam.signup_intent.verification_recently_sent"); event.Outcome != "throttled" || event.SignupIntentID != first.Intent.SignupIntentID {
		t.Fatalf("missing rapid resend event: %#v", event)
	}
}

func TestServiceStartSignupSuppressesCompletedEmail(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	svc := &Service{
		Store:            store,
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}
	completed, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Labs",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup completed seed: %v", err)
	}
	intent := store.intents[completed.Intent.SignupIntentID]
	intent.State = SignupIntentStateCompleted
	intent.CompletedAt = &now
	intent.OrganizationSlug = "acme"
	store.intents[completed.Intent.SignupIntentID] = intent

	got, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Another Org",
		IdempotencyKey:   "signup-start-2",
	})
	if err != nil {
		t.Fatalf("StartSignup duplicate completed: %v", err)
	}
	if got.Outcome != SignupStartOutcomeExistingAccountSuppressed || got.SendVerificationEmail() || got.VerificationToken != "" {
		t.Fatalf("completed account should be suppressed without a verification email: %#v", got)
	}
	if got.Intent.SignupIntentID != completed.Intent.SignupIntentID {
		t.Fatalf("unexpected completed intent returned: %#v", got.Intent)
	}
	if event := store.lastEvent("iam.signup_intent.existing_account_suppressed"); event.Outcome != "suppressed" {
		t.Fatalf("missing suppression event: %#v", event)
	}
}

func TestServiceVerifySignupRejectsUnavailableSlugBeforeDirectorySideEffects(t *testing.T) {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	store := newFakeSignupLifecycleStore()
	store.unavailableSlugs = map[string]bool{"acme-prod": true}
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:            store,
		Directory:        directory,
		PolicyWriter:     fakeOwnerPolicyWriter{},
		Billing:          fakeBillingProvisioner{},
		EmailIdentityKey: testEmailIdentityKey,
		Now:              func() time.Time { return now },
	}

	start, err := svc.StartSignup(context.Background(), StartSignupRequest{
		Email:            "founder@example.test",
		OrganizationName: "Acme Labs",
		IdempotencyKey:   "signup-start-1",
	})
	if err != nil {
		t.Fatalf("StartSignup: %v", err)
	}
	_, err = svc.VerifySignup(context.Background(), VerifySignupRequest{
		SignupIntentID:          start.Intent.SignupIntentID,
		VerificationToken:       start.VerificationToken,
		OrganizationDisplayName: "Acme Production",
		OrganizationSlug:        "acme-prod",
		IdempotencyKey:          "signup-verify-1",
	})
	if !errors.Is(err, ErrOrganizationSlugUnavailable) {
		t.Fatalf("VerifySignup err = %v, want ErrOrganizationSlugUnavailable", err)
	}
	if directory.createInput.Name != "" || directory.signupInput.Email != "" {
		t.Fatalf("slug conflict should happen before Zitadel side effects: org=%#v user=%#v", directory.createInput, directory.signupInput)
	}
	if store.profileCreated {
		t.Fatalf("organization profile should not be created on slug conflict")
	}
	if event := store.lastEvent("iam.signup_intent.materialization_failed"); event.ErrorKind != "organization_slug_unavailable" || !event.Retryable {
		t.Fatalf("unexpected failure event: %#v", event)
	}
	if got := store.intents[start.Intent.SignupIntentID].State; got != SignupIntentStateFailedRetryable {
		t.Fatalf("slug conflict state = %q, want %q", got, SignupIntentStateFailedRetryable)
	}
	store.unavailableSlugs = map[string]bool{}
	verified, err := svc.VerifySignup(context.Background(), VerifySignupRequest{
		SignupIntentID:          start.Intent.SignupIntentID,
		VerificationToken:       start.VerificationToken,
		OrganizationDisplayName: "Acme Production",
		OrganizationSlug:        "acme-prod-2",
		IdempotencyKey:          "signup-verify-2",
	})
	if err != nil {
		t.Fatalf("VerifySignup retry with different slug: %v", err)
	}
	if verified.Organization.Slug != "acme-prod-2" || directory.signupInput.Email != "founder@example.test" {
		t.Fatalf("unexpected retry result: org=%#v user=%#v", verified.Organization, directory.signupInput)
	}
}

func TestServiceInviteMemberNormalizesRolesForDirectoryAndPolicy(t *testing.T) {
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
	}

	got, err := svc.InviteMember(context.Background(), Principal{Subject: "owner-1", OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2"}, InviteMemberRequest{
		Email: " invited@example.test ",
		Roles: []string{"roles/member", "roles/admin", "roles/member"},
	})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	if directory.inviteProviderOrgID != "42" {
		t.Fatalf("provider org = %q", directory.inviteProviderOrgID)
	}
	if directory.inviteInput.Email != "invited@example.test" {
		t.Fatalf("directory invite input = %#v", directory.inviteInput)
	}
	if strings.Join(got.Roles, ",") != "roles/admin,roles/member" {
		t.Fatalf("roles = %#v", got.Roles)
	}
	if got.AcceptanceToken == "" || got.AcceptanceExpiresAt.IsZero() {
		t.Fatalf("invite acceptance token missing: %#v", got)
	}
}

func TestServiceCompleteMemberInviteReturnsAcceptedMember(t *testing.T) {
	directory := &fakeMembersDirectory{}
	svc := &Service{
		Store:     fakeMembersStore{},
		Directory: directory,
		Now:       func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	got, err := svc.CompleteMemberInvite(context.Background(), CompleteMemberInviteRequest{
		AcceptanceToken: strings.Repeat("a", 32),
	})
	if err != nil {
		t.Fatalf("CompleteMemberInvite: %v", err)
	}
	if directory.completeInput.UserID != "user-invite" || directory.completeInput.EmailVerificationCode != "email-code" {
		t.Fatalf("unexpected directory completion input: %#v", directory.completeInput)
	}
	if got.OrgID != "org_01J8QJ4P1R7S9W2X5M6N8P0Q2" || got.UserID != "user-invite" || got.AcceptedAt == nil {
		t.Fatalf("unexpected accepted invite: %#v", got)
	}
}

type fakeMembersDirectory struct {
	members             []Member
	createInput         DirectoryCreateOrganizationRequest
	signupInput         DirectoryCreateSignupUserRequest
	inviteProviderOrgID string
	inviteInput         InviteMemberRequest
	completeInput       DirectoryCompleteMemberInviteRequest
}

func (d *fakeMembersDirectory) ListMembers(context.Context, string) ([]Member, error) {
	out := make([]Member, len(d.members))
	copy(out, d.members)
	return out, nil
}

func (d *fakeMembersDirectory) CreateOrganization(_ context.Context, input DirectoryCreateOrganizationRequest) (DirectoryCreateOrganizationResult, error) {
	d.createInput = input
	return DirectoryCreateOrganizationResult{OrganizationID: "43"}, nil
}

func (d *fakeMembersDirectory) InviteMember(_ context.Context, providerOrgID string, input InviteMemberRequest) (DirectoryInviteMemberResult, error) {
	d.inviteProviderOrgID = providerOrgID
	d.inviteInput = input
	return DirectoryInviteMemberResult{UserID: "user-invite", Email: input.Email, Status: "invited", EmailVerificationCode: "email-code"}, nil
}

func (d *fakeMembersDirectory) CreateSignupUser(_ context.Context, input DirectoryCreateSignupUserRequest) (DirectoryCreateSignupUserResult, error) {
	d.signupInput = input
	return DirectoryCreateSignupUserResult{UserID: "signup-user"}, nil
}

func (d *fakeMembersDirectory) CompleteMemberInvite(_ context.Context, input DirectoryCompleteMemberInviteRequest) error {
	d.completeInput = input
	return nil
}

func (d *fakeMembersDirectory) UpdateHumanProfile(context.Context, string, HumanProfileUpdate) (HumanProfile, error) {
	return HumanProfile{}, nil
}

func (d *fakeMembersDirectory) CreateServiceAccountCredential(context.Context, string, ServiceAccountCredentialInput) (string, APICredentialIssuedMaterial, error) {
	return "", APICredentialIssuedMaterial{}, nil
}

func (d *fakeMembersDirectory) AddServiceAccountCredential(context.Context, AddServiceAccountCredentialInput) (APICredentialIssuedMaterial, error) {
	return APICredentialIssuedMaterial{}, nil
}

func (d *fakeMembersDirectory) RemoveServiceAccountCredential(context.Context, string, APICredentialSecret) error {
	return nil
}

func (d *fakeMembersDirectory) DeactivateServiceAccount(context.Context, string) error {
	return nil
}

type fakeMembersAuthz struct {
	orgIDs []string
}

func (a fakeMembersAuthz) LookupOrganizations(context.Context, AuthorizationSubject, string, string) ([]string, string, error) {
	return append([]string(nil), a.orgIDs...), "zed-token", nil
}

func (a fakeMembersAuthz) TestOrganizationPermissions(context.Context, string, AuthorizationSubject, []string, string) ([]string, string, error) {
	return nil, "zed-token", nil
}

type fakeOwnerPolicyWriter struct{}

func (fakeOwnerPolicyWriter) SetOrganizationOwner(context.Context, OrganizationOwnerPolicyRequest) error {
	return nil
}

type recordingOwnerPolicyWriter struct {
	input OrganizationOwnerPolicyRequest
}

func (w *recordingOwnerPolicyWriter) SetOrganizationOwner(_ context.Context, input OrganizationOwnerPolicyRequest) error {
	w.input = input
	return nil
}

type fakeBillingProvisioner struct{}

func (fakeBillingProvisioner) EnsureBillingOrganization(context.Context, BillingOrganizationProvisioningRequest) error {
	return nil
}

type recordingBillingProvisioner struct {
	input BillingOrganizationProvisioningRequest
}

func (p *recordingBillingProvisioner) EnsureBillingOrganization(_ context.Context, input BillingOrganizationProvisioningRequest) error {
	p.input = input
	return nil
}

type fakeMembersStore struct{}

type partialMetadataStore struct {
	fakeMembersStore
	available map[string]OrganizationMetadata
}

type fakeSignupStore struct {
	fakeMembersStore
	created CreateOrganizationRequest
}

type fakeSignupLifecycleStore struct {
	fakeMembersStore
	intents          map[string]SignupIntent
	idempotency      map[string]string
	unavailableSlugs map[string]bool
	profileCreated   bool
	profile          OrganizationProfile
	created          CreateOrganizationRequest
	steps            []string
	events           []IAMEvent
	completed        bool
}

func newFakeSignupLifecycleStore() *fakeSignupLifecycleStore {
	return &fakeSignupLifecycleStore{
		intents:     map[string]SignupIntent{},
		idempotency: map[string]string{},
	}
}

func (s partialMetadataStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		if organization, ok := s.available[orgID]; ok {
			out = append(out, organization)
		}
	}
	return out, nil
}

func (s *fakeSignupStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	s.created = input
	return OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}, nil
}

func (s *fakeSignupLifecycleStore) StartSignupIntent(_ context.Context, intent SignupIntent, now time.Time) (SignupStartDecision, error) {
	for _, existing := range s.intents {
		if bytes.Equal(existing.EmailIdentityHash, intent.EmailIdentityHash) && existing.State == SignupIntentStateCompleted {
			return SignupStartDecision{Intent: existing, Outcome: SignupStartOutcomeExistingAccountSuppressed}, nil
		}
	}
	for _, existing := range s.intents {
		if bytes.Equal(existing.EmailIdentityHash, intent.EmailIdentityHash) && (existing.State == SignupIntentStatePendingVerification || existing.State == SignupIntentStateExpired || signupRetryCanChangeRequest(existing)) {
			if existing.State != SignupIntentStateExpired && now.Before(existing.UpdatedAt.Add(signupVerificationResendInterval)) {
				return SignupStartDecision{Intent: existing, Outcome: SignupStartOutcomeVerificationRecentlySent}, nil
			}
			existing.RequestHash = append([]byte(nil), intent.RequestHash...)
			existing.VerificationTokenHash = append([]byte(nil), intent.VerificationTokenHash...)
			existing.EmailDelivery = intent.EmailDelivery
			existing.OrganizationDisplayName = intent.OrganizationDisplayName
			existing.RequestedOrganizationSlug = intent.RequestedOrganizationSlug
			existing.GivenName = intent.GivenName
			existing.FamilyName = intent.FamilyName
			existing.State = SignupIntentStatePendingVerification
			existing.VerificationExpiresAt = intent.VerificationExpiresAt
			existing.UpdatedAt = intent.UpdatedAt
			s.intents[existing.SignupIntentID] = existing
			return SignupStartDecision{Intent: existing, Outcome: SignupStartOutcomeVerificationResent}, nil
		}
	}
	for _, existing := range s.intents {
		if bytes.Equal(existing.EmailIdentityHash, intent.EmailIdentityHash) {
			return SignupStartDecision{Intent: existing, Outcome: SignupStartOutcomeInFlightSuppressed}, nil
		}
	}
	if existingID := s.idempotency[intent.IdempotencyKey]; existingID != "" {
		existing := s.intents[existingID]
		if !bytes.Equal(existing.RequestHash, intent.RequestHash) {
			return SignupStartDecision{}, ErrIdempotencyConflict
		}
		return SignupStartDecision{Intent: existing, Outcome: SignupStartOutcomeInFlightSuppressed}, nil
	}
	s.idempotency[intent.IdempotencyKey] = intent.SignupIntentID
	s.intents[intent.SignupIntentID] = intent
	return SignupStartDecision{Intent: intent, Outcome: SignupStartOutcomeCreated}, nil
}

func (s *fakeSignupLifecycleStore) DeletePendingSignupIntent(_ context.Context, signupIntentID string) error {
	intent := s.intents[signupIntentID]
	if intent.State == SignupIntentStatePendingVerification {
		delete(s.intents, signupIntentID)
	}
	return nil
}

func (s *fakeSignupLifecycleStore) ClaimSignupIntentForVerification(_ context.Context, signupIntentID string, verificationTokenHash []byte, idempotencyKey string, verifyRequestHash []byte, organizationDisplayName string, requestedOrganizationSlug string, now time.Time, leaseExpiresAt time.Time) (SignupIntent, error) {
	intent, ok := s.intents[signupIntentID]
	if !ok || !bytes.Equal(intent.VerificationTokenHash, verificationTokenHash) {
		return SignupIntent{}, ErrSignupIntentMissing
	}
	if now.After(intent.VerificationExpiresAt) {
		return SignupIntent{}, ErrSignupIntentExpired
	}
	if intent.VerifyIdempotencyKey != "" {
		sameVerifyRequest := intent.VerifyIdempotencyKey == idempotencyKey && bytes.Equal(intent.VerifyRequestHash, verifyRequestHash)
		if intent.VerifyIdempotencyKey == idempotencyKey && !bytes.Equal(intent.VerifyRequestHash, verifyRequestHash) {
			return SignupIntent{}, ErrIdempotencyConflict
		}
		if !sameVerifyRequest && !signupRetryCanChangeRequest(intent) {
			if intent.State == SignupIntentStateCompleted {
				return SignupIntent{}, ErrSignupVerificationAlreadyUsed
			}
			return SignupIntent{}, ErrIdempotencyConflict
		}
		if sameVerifyRequest && intent.State == SignupIntentStateCompleted {
			return intent, nil
		}
	}
	if intent.State != SignupIntentStatePendingVerification && intent.State != SignupIntentStateFailedRetryable {
		return SignupIntent{}, ErrSignupIntentConflict
	}
	intent.State = SignupIntentStateMaterializing
	intent.VerifyIdempotencyKey = idempotencyKey
	intent.VerifyRequestHash = append([]byte(nil), verifyRequestHash...)
	if organizationDisplayName != "" {
		intent.OrganizationDisplayName = organizationDisplayName
	}
	if requestedOrganizationSlug != "" {
		intent.RequestedOrganizationSlug = requestedOrganizationSlug
	}
	intent.MaterializationAttempts++
	intent.MaterializationLeaseExpires = &leaseExpiresAt
	intent.VerifiedAt = &now
	s.intents[signupIntentID] = intent
	return intent, nil
}

func (s *fakeSignupLifecycleStore) RecordSignupIntentStep(_ context.Context, signupIntentID, step string, leaseExpiresAt time.Time) error {
	intent := s.intents[signupIntentID]
	intent.MaterializationStep = step
	intent.MaterializationLeaseExpires = &leaseExpiresAt
	s.intents[signupIntentID] = intent
	s.steps = append(s.steps, step)
	return nil
}

func (s *fakeSignupLifecycleStore) RecordSignupIntentProviderOrg(_ context.Context, signupIntentID, providerOrgID string) error {
	intent := s.intents[signupIntentID]
	intent.IdentityProviderOrgID = providerOrgID
	s.intents[signupIntentID] = intent
	return nil
}

func (s *fakeSignupLifecycleStore) RecordSignupIntentProviderUser(_ context.Context, signupIntentID, providerUserID string) error {
	intent := s.intents[signupIntentID]
	intent.IdentityProviderUserID = providerUserID
	s.intents[signupIntentID] = intent
	return nil
}

func (s *fakeSignupLifecycleStore) RecordSignupIntentOrganization(_ context.Context, signupIntentID, orgID, organizationSlug string) error {
	intent := s.intents[signupIntentID]
	intent.OrgID = orgID
	intent.OrganizationSlug = organizationSlug
	s.intents[signupIntentID] = intent
	return nil
}

func (s *fakeSignupLifecycleStore) MarkSignupIntentFailed(_ context.Context, signupIntentID string, state SignupIntentState, message string) error {
	intent := s.intents[signupIntentID]
	intent.State = state
	intent.MaterializationLastError = message
	s.intents[signupIntentID] = intent
	return nil
}

func (s *fakeSignupLifecycleStore) CompleteSignupIntent(_ context.Context, signupIntentID string, completedAt time.Time) (SignupIntent, error) {
	intent := s.intents[signupIntentID]
	intent.State = SignupIntentStateCompleted
	intent.MaterializationStep = "completed"
	intent.CompletedAt = &completedAt
	intent.MaterializationLeaseExpires = nil
	s.intents[signupIntentID] = intent
	s.completed = true
	return intent, nil
}

func (s *fakeSignupLifecycleStore) AppendIAMEvent(_ context.Context, event IAMEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *fakeSignupLifecycleStore) GetOrganizationProfile(_ context.Context, orgID, _ string) (OrganizationProfile, error) {
	if !s.profileCreated {
		return OrganizationProfile{}, ErrOrganizationMissing
	}
	if s.profile.OrgID != orgID {
		return OrganizationProfile{}, ErrOrganizationMissing
	}
	return s.profile, nil
}

func (s *fakeSignupLifecycleStore) OrganizationSlugAvailable(_ context.Context, slug string) (bool, error) {
	return !s.unavailableSlugs[slug], nil
}

func (s *fakeSignupLifecycleStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	s.created = input
	s.profileCreated = true
	s.profile = OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}
	return s.profile, nil
}

func (s *fakeSignupLifecycleStore) lastEvent(eventType string) IAMEvent {
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].EventType == eventType {
			return s.events[i]
		}
	}
	return IAMEvent{}
}

func (fakeMembersStore) GetOrganizationProfile(context.Context, string, string) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) CreateMemberInviteAcceptance(context.Context, MemberInviteAcceptance) error {
	return nil
}

func (fakeMembersStore) GetMemberInviteAcceptance(context.Context, string, time.Time) (MemberInviteAcceptance, error) {
	return MemberInviteAcceptance{
		OrgID:                 "org_01J8QJ4P1R7S9W2X5M6N8P0Q2",
		UserID:                "user-invite",
		Email:                 "invited@example.test",
		EmailVerificationCode: "email-code",
	}, nil
}

func (fakeMembersStore) AcceptMemberInviteAcceptance(context.Context, string, time.Time) error {
	return nil
}

func (fakeMembersStore) ListOrganizationMetadataByOrgIDs(_ context.Context, orgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		out = append(out, OrganizationMetadata{OrgID: orgID, IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (fakeMembersStore) ListOrganizationMetadataByProviderOrgIDs(_ context.Context, providerOrgIDs []string) ([]OrganizationMetadata, error) {
	out := make([]OrganizationMetadata, 0, len(providerOrgIDs))
	for _, providerOrgID := range providerOrgIDs {
		out = append(out, OrganizationMetadata{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: providerOrgID, DisplayName: "Acme", Slug: "acme", Version: 1})
	}
	return out, nil
}

func (fakeMembersStore) OrganizationSlugAvailable(context.Context, string) (bool, error) {
	return true, nil
}

func (fakeMembersStore) CreateOrganizationProfile(_ context.Context, input CreateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: input.OrgID, IdentityProviderOrgID: input.IdentityProviderOrgID, DisplayName: input.DisplayName, Slug: input.Slug, State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) UpdateOrganizationProfile(context.Context, Principal, UpdateOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 2}, nil
}

func (fakeMembersStore) ResolveOrganizationProfile(context.Context, ResolveOrganizationRequest) (OrganizationProfile, error) {
	return OrganizationProfile{OrgID: "org_01J8QJ4P1R7S9W2X5M6N8P0Q2", IdentityProviderOrgID: "42", DisplayName: "Acme", Slug: "acme", State: OrganizationProfileStateActive, Version: 1}, nil
}

func (fakeMembersStore) CreateServiceAccount(context.Context, ServiceAccount, APICredential, APICredentialSecret) (ServiceAccount, APICredential, error) {
	return ServiceAccount{}, APICredential{}, nil
}

func (fakeMembersStore) ListServiceAccounts(context.Context, string) ([]ServiceAccount, error) {
	return nil, nil
}

func (fakeMembersStore) GetServiceAccount(context.Context, string, string) (ServiceAccount, error) {
	return ServiceAccount{}, ErrAPICredentialMissing
}

func (fakeMembersStore) DisableServiceAccount(context.Context, string, string, string, time.Time) (ServiceAccount, []APICredential, error) {
	return ServiceAccount{}, nil, nil
}

func (fakeMembersStore) CreateAPICredential(context.Context, APICredential, APICredentialSecret) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) ListAPICredentials(context.Context, string) ([]APICredential, error) {
	return nil, nil
}

func (fakeMembersStore) GetAPICredential(context.Context, string, string) (APICredential, error) {
	return APICredential{}, ErrAPICredentialMissing
}

func (fakeMembersStore) ActiveAPICredentialSecrets(context.Context, string, string) ([]APICredentialSecret, error) {
	return nil, nil
}

func (fakeMembersStore) AddAPICredentialSecret(context.Context, string, string, string, APICredentialSecret) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) RevokeAPICredential(context.Context, string, string, string, time.Time) (APICredential, error) {
	return APICredential{}, nil
}

func (fakeMembersStore) ResolveAPICredentialClaims(context.Context, string, time.Time) (ResolveAPICredentialClaimsResult, error) {
	return ResolveAPICredentialClaimsResult{}, ErrAPICredentialMissing
}
