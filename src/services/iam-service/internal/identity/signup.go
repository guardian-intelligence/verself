package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	signupIntentPublicIDPrefix = "signup_"
	signupIntentLeaseDuration  = 5 * time.Minute
)

func (s *Service) StartSignup(ctx context.Context, input StartSignupRequest) (StartSignupResult, error) {
	input, err := normalizeStartSignup(input)
	if err != nil {
		return StartSignupResult{}, err
	}
	store, err := s.signupStore()
	if err != nil {
		return StartSignupResult{}, err
	}
	signupIntentID, err := randomSignupIntentID()
	if err != nil {
		return StartSignupResult{}, err
	}
	orgID, err := randomOrganizationID()
	if err != nil {
		return StartSignupResult{}, err
	}
	token, tokenHash, err := randomSignupVerificationToken()
	if err != nil {
		return StartSignupResult{}, err
	}
	requestHash, err := signupStartRequestHash(input)
	if err != nil {
		return StartSignupResult{}, err
	}
	_, emailHash := SecretHash(input.Email)
	now := s.now()
	responseExpiresAt := now.Add(24 * time.Hour)
	decision, err := store.StartSignupIntent(ctx, SignupIntent{
		SignupIntentID:            signupIntentID,
		IdempotencyKey:            input.IdempotencyKey,
		RequestHash:               requestHash,
		Email:                     input.Email,
		EmailHash:                 emailHash,
		OrganizationDisplayName:   input.OrganizationName,
		RequestedOrganizationSlug: input.OrganizationSlug,
		GivenName:                 input.GivenName,
		FamilyName:                input.FamilyName,
		VerificationTokenHash:     tokenHash,
		State:                     SignupIntentStatePendingVerification,
		OrgID:                     orgID,
		CreatedAt:                 now,
		UpdatedAt:                 now,
		VerificationExpiresAt:     responseExpiresAt,
	}, now)
	if err != nil {
		return StartSignupResult{}, err
	}
	if decision.Intent.VerificationExpiresAt.After(now) {
		responseExpiresAt = decision.Intent.VerificationExpiresAt
	}
	switch decision.Outcome {
	case SignupStartOutcomeExistingAccountSuppressed:
		responseExpiresAt = now.Add(24 * time.Hour)
		if err := s.emitSignupEvent(ctx, store, IAMEvent{
			EventType:      "iam.signup_intent.existing_account_suppressed",
			AggregateType:  "signup_intent",
			AggregateID:    decision.Intent.SignupIntentID,
			SignupIntentID: decision.Intent.SignupIntentID,
			OrgID:          decision.Intent.OrgID,
			State:          decision.Intent.State,
			Outcome:        "suppressed",
			OccurredAt:     now,
			Payload:        map[string]any{"email_fingerprint": hashText(input.Email)},
		}); err != nil {
			return StartSignupResult{}, err
		}
		token = ""
	case SignupStartOutcomeInFlightSuppressed:
		responseExpiresAt = now.Add(24 * time.Hour)
		if err := s.emitSignupEvent(ctx, store, IAMEvent{
			EventType:      "iam.signup_intent.inflight_suppressed",
			AggregateType:  "signup_intent",
			AggregateID:    decision.Intent.SignupIntentID,
			SignupIntentID: decision.Intent.SignupIntentID,
			OrgID:          decision.Intent.OrgID,
			State:          decision.Intent.State,
			Outcome:        "suppressed",
			OccurredAt:     now,
			Payload:        map[string]any{"email_fingerprint": hashText(input.Email)},
		}); err != nil {
			return StartSignupResult{}, err
		}
		token = ""
	case SignupStartOutcomeVerificationRecentlySent:
		if err := s.emitSignupEvent(ctx, store, IAMEvent{
			EventType:          "iam.signup_intent.verification_recently_sent",
			AggregateType:      "signup_intent",
			AggregateID:        decision.Intent.SignupIntentID,
			SignupIntentID:     decision.Intent.SignupIntentID,
			OrgID:              decision.Intent.OrgID,
			State:              decision.Intent.State,
			Outcome:            "throttled",
			OccurredAt:         now,
			IdempotencyKeyHash: hashText(input.IdempotencyKey),
			CorrelationID:      input.IdempotencyKey,
			Payload:            map[string]any{"organization_display_name": decision.Intent.OrganizationDisplayName, "requested_organization_slug": decision.Intent.RequestedOrganizationSlug},
		}); err != nil {
			return StartSignupResult{}, err
		}
		token = ""
	case SignupStartOutcomeVerificationResent:
		if err := s.emitSignupEvent(ctx, store, IAMEvent{
			EventType:          "iam.signup_intent.verification_resent",
			AggregateType:      "signup_intent",
			AggregateID:        decision.Intent.SignupIntentID,
			SignupIntentID:     decision.Intent.SignupIntentID,
			OrgID:              decision.Intent.OrgID,
			State:              decision.Intent.State,
			Outcome:            "resent",
			OccurredAt:         now,
			IdempotencyKeyHash: hashText(input.IdempotencyKey),
			CorrelationID:      input.IdempotencyKey,
			Payload:            map[string]any{"organization_display_name": decision.Intent.OrganizationDisplayName, "requested_organization_slug": decision.Intent.RequestedOrganizationSlug},
		}); err != nil {
			return StartSignupResult{}, err
		}
	case SignupStartOutcomeCreated:
		if err := s.emitSignupEvent(ctx, store, IAMEvent{
			EventType:          "iam.signup_intent.started",
			AggregateType:      "signup_intent",
			AggregateID:        decision.Intent.SignupIntentID,
			SignupIntentID:     decision.Intent.SignupIntentID,
			OrgID:              decision.Intent.OrgID,
			State:              decision.Intent.State,
			Outcome:            "created",
			OccurredAt:         now,
			IdempotencyKeyHash: hashText(input.IdempotencyKey),
			CorrelationID:      input.IdempotencyKey,
			Payload:            map[string]any{"organization_display_name": decision.Intent.OrganizationDisplayName, "requested_organization_slug": decision.Intent.RequestedOrganizationSlug},
		}); err != nil {
			return StartSignupResult{}, err
		}
	default:
		token = ""
	}
	return StartSignupResult{Intent: decision.Intent, VerificationToken: token, Outcome: decision.Outcome, ResponseExpiresAt: responseExpiresAt}, nil
}

func (s *Service) DeletePendingSignupIntent(ctx context.Context, signupIntentID string) error {
	store, err := s.signupStore()
	if err != nil {
		return err
	}
	return store.DeletePendingSignupIntent(ctx, strings.TrimSpace(signupIntentID))
}

func (s *Service) VerifySignup(ctx context.Context, input VerifySignupRequest) (VerifySignupResult, error) {
	input, err := normalizeVerifySignup(input)
	if err != nil {
		return VerifySignupResult{}, err
	}
	signupStore, err := s.signupStore()
	if err != nil {
		return VerifySignupResult{}, err
	}
	_, tokenHash := SecretHash(input.VerificationToken)
	verifyHash, err := signupVerifyRequestHash(input, tokenHash)
	if err != nil {
		return VerifySignupResult{}, err
	}
	now := s.now()
	intent, err := signupStore.ClaimSignupIntentForVerification(ctx, input.SignupIntentID, tokenHash, input.IdempotencyKey, verifyHash, input.OrganizationDisplayName, input.OrganizationSlug, now, now.Add(signupIntentLeaseDuration))
	if err != nil {
		return VerifySignupResult{}, err
	}
	if intent.State == SignupIntentStateCompleted {
		store, err := s.store()
		if err != nil {
			return VerifySignupResult{}, err
		}
		org, err := s.organizationForCompletedSignup(ctx, store, intent)
		if err != nil {
			return VerifySignupResult{}, err
		}
		return VerifySignupResult{Intent: intent, Organization: org}, nil
	}
	if err := s.emitSignupEvent(ctx, signupStore, IAMEvent{
		EventType:          "iam.signup_intent.verification_accepted",
		AggregateType:      "signup_intent",
		AggregateID:        intent.SignupIntentID,
		SignupIntentID:     intent.SignupIntentID,
		OrgID:              intent.OrgID,
		State:              SignupIntentStateMaterializing,
		Outcome:            "accepted",
		Attempt:            signupEventAttempt(intent.MaterializationAttempts),
		OccurredAt:         now,
		IdempotencyKeyHash: hashText(input.IdempotencyKey),
		CorrelationID:      input.IdempotencyKey,
	}); err != nil {
		return VerifySignupResult{}, err
	}
	store, err := s.store()
	if err != nil {
		return VerifySignupResult{}, err
	}
	result, err := s.materializeSignup(ctx, store, signupStore, &intent, input.IdempotencyKey)
	if err != nil {
		failureState := SignupIntentStateFailedRetryable
		retryable := true
		if signupFailureTerminal(err) {
			failureState = SignupIntentStateFailedTerminal
			retryable = false
		}
		_ = signupStore.MarkSignupIntentFailed(ctx, intent.SignupIntentID, failureState, trimErrorMessage(err))
		_ = s.emitSignupEvent(ctx, signupStore, IAMEvent{
			EventType:              "iam.signup_intent.materialization_failed",
			AggregateType:          "signup_intent",
			AggregateID:            intent.SignupIntentID,
			SignupIntentID:         intent.SignupIntentID,
			OrgID:                  intent.OrgID,
			IdentityProviderOrgID:  intent.IdentityProviderOrgID,
			IdentityProviderUserID: intent.IdentityProviderUserID,
			Step:                   intent.MaterializationStep,
			State:                  failureState,
			Outcome:                "failed",
			Retryable:              retryable,
			Attempt:                signupEventAttempt(intent.MaterializationAttempts),
			ErrorKind:              signupErrorKind(err),
			ErrorMessage:           trimErrorMessage(err),
			OccurredAt:             s.now(),
			IdempotencyKeyHash:     hashText(input.IdempotencyKey),
			CorrelationID:          input.IdempotencyKey,
		})
		return VerifySignupResult{}, err
	}
	return result, nil
}

func (s *Service) materializeSignup(ctx context.Context, store Store, signupStore SignupStore, intent *SignupIntent, idempotencyKey string) (VerifySignupResult, error) {
	directory, err := s.signupDirectory()
	if err != nil {
		return VerifySignupResult{}, err
	}
	policyWriter, err := s.policyWriter()
	if err != nil {
		return VerifySignupResult{}, err
	}
	billing, err := s.billing()
	if err != nil {
		return VerifySignupResult{}, err
	}
	profile, err := store.GetOrganizationProfile(ctx, intent.OrgID, "system:signup")
	createProfile := false
	profileSlug := ""
	if errors.Is(err, ErrOrganizationMissing) {
		createProfile = true
		profileSlug, err = s.availableOrganizationSlug(ctx, store, intent.RequestedOrganizationSlug, intent.OrganizationDisplayName)
		if err != nil {
			return VerifySignupResult{}, err
		}
	} else if err != nil {
		return VerifySignupResult{}, err
	}
	lease := s.now().Add(signupIntentLeaseDuration)
	if strings.TrimSpace(intent.IdentityProviderOrgID) == "" {
		if err := s.recordSignupStep(ctx, signupStore, intent, "zitadel_organization", lease, idempotencyKey); err != nil {
			return VerifySignupResult{}, err
		}
		provider, err := directory.CreateOrganization(ctx, DirectoryCreateOrganizationRequest{Name: signupProviderOrganizationName(*intent)})
		if err != nil {
			return VerifySignupResult{}, err
		}
		intent.IdentityProviderOrgID = strings.TrimSpace(provider.OrganizationID)
		if err := signupStore.RecordSignupIntentProviderOrg(ctx, intent.SignupIntentID, intent.IdentityProviderOrgID); err != nil {
			return VerifySignupResult{}, err
		}
	}
	if strings.TrimSpace(intent.IdentityProviderUserID) == "" {
		lease = s.now().Add(signupIntentLeaseDuration)
		if err := s.recordSignupStep(ctx, signupStore, intent, "zitadel_user", lease, idempotencyKey); err != nil {
			return VerifySignupResult{}, err
		}
		user, err := directory.CreateSignupUser(ctx, DirectoryCreateSignupUserRequest{
			OrgID:      intent.IdentityProviderOrgID,
			Email:      intent.Email,
			GivenName:  intent.GivenName,
			FamilyName: intent.FamilyName,
		})
		if err != nil {
			return VerifySignupResult{}, err
		}
		intent.IdentityProviderUserID = strings.TrimSpace(user.UserID)
		if err := signupStore.RecordSignupIntentProviderUser(ctx, intent.SignupIntentID, intent.IdentityProviderUserID); err != nil {
			return VerifySignupResult{}, err
		}
	}
	if createProfile {
		lease = s.now().Add(signupIntentLeaseDuration)
		if err := s.recordSignupStep(ctx, signupStore, intent, "iam_organization", lease, idempotencyKey); err != nil {
			return VerifySignupResult{}, err
		}
		profile, err = store.CreateOrganizationProfile(ctx, CreateOrganizationRequest{
			OrgID:                 intent.OrgID,
			IdentityProviderOrgID: intent.IdentityProviderOrgID,
			DisplayName:           intent.OrganizationDisplayName,
			Slug:                  profileSlug,
			ActorID:               intent.IdentityProviderUserID,
		})
		if err != nil {
			return VerifySignupResult{}, err
		}
		if err := signupStore.RecordSignupIntentOrganization(ctx, intent.SignupIntentID, profile.OrgID, profile.Slug); err != nil {
			return VerifySignupResult{}, err
		}
	}
	intent.OrganizationSlug = profile.Slug
	if err := s.recordSignupStep(ctx, signupStore, intent, "spicedb_owner_policy", s.now().Add(signupIntentLeaseDuration), idempotencyKey); err != nil {
		return VerifySignupResult{}, err
	}
	if err := policyWriter.SetOrganizationOwner(ctx, OrganizationOwnerPolicyRequest{
		OrgID:       profile.OrgID,
		OwnerUserID: intent.IdentityProviderUserID,
		OperationID: "verify-signup",
	}); err != nil {
		return VerifySignupResult{}, err
	}
	if err := s.recordSignupStep(ctx, signupStore, intent, "billing_organization", s.now().Add(signupIntentLeaseDuration), idempotencyKey); err != nil {
		return VerifySignupResult{}, err
	}
	if err := billing.EnsureBillingOrganization(ctx, BillingOrganizationProvisioningRequest{
		OrgID:       profile.OrgID,
		DisplayName: profile.DisplayName,
		TrustTier:   "new",
	}); err != nil {
		return VerifySignupResult{}, err
	}
	completed, err := signupStore.CompleteSignupIntent(ctx, intent.SignupIntentID, s.now())
	if err != nil {
		return VerifySignupResult{}, err
	}
	org := Organization{OrgID: profile.OrgID, DisplayName: profile.DisplayName, Slug: profile.Slug, Version: profile.Version}
	if err := s.emitSignupEvent(ctx, signupStore, IAMEvent{
		EventType:              "iam.signup_intent.completed",
		AggregateType:          "signup_intent",
		AggregateID:            completed.SignupIntentID,
		SignupIntentID:         completed.SignupIntentID,
		OrgID:                  completed.OrgID,
		IdentityProviderOrgID:  completed.IdentityProviderOrgID,
		IdentityProviderUserID: completed.IdentityProviderUserID,
		Step:                   "completed",
		State:                  completed.State,
		Outcome:                "completed",
		Attempt:                signupEventAttempt(completed.MaterializationAttempts),
		OccurredAt:             s.now(),
		IdempotencyKeyHash:     hashText(idempotencyKey),
		CorrelationID:          idempotencyKey,
		Payload:                map[string]any{"organization_slug": profile.Slug},
	}); err != nil {
		return VerifySignupResult{}, err
	}
	return VerifySignupResult{Intent: completed, Organization: org}, nil
}

func (s *Service) recordSignupStep(ctx context.Context, store SignupStore, intent *SignupIntent, step string, lease time.Time, idempotencyKey string) error {
	if err := store.RecordSignupIntentStep(ctx, intent.SignupIntentID, step, lease); err != nil {
		return err
	}
	intent.MaterializationStep = step
	intent.MaterializationLeaseExpires = &lease
	return s.emitSignupEvent(ctx, store, IAMEvent{
		EventType:              "iam.signup_intent.materialization_step",
		AggregateType:          "signup_intent",
		AggregateID:            intent.SignupIntentID,
		SignupIntentID:         intent.SignupIntentID,
		OrgID:                  intent.OrgID,
		IdentityProviderOrgID:  intent.IdentityProviderOrgID,
		IdentityProviderUserID: intent.IdentityProviderUserID,
		Step:                   step,
		State:                  SignupIntentStateMaterializing,
		Outcome:                "started",
		Attempt:                signupEventAttempt(intent.MaterializationAttempts),
		OccurredAt:             s.now(),
		IdempotencyKeyHash:     hashText(idempotencyKey),
		CorrelationID:          idempotencyKey,
	})
}

func (s *Service) organizationForCompletedSignup(ctx context.Context, store Store, intent SignupIntent) (Organization, error) {
	profile, err := store.GetOrganizationProfile(ctx, intent.OrgID, "system:signup")
	if err != nil {
		return Organization{}, err
	}
	return Organization{OrgID: profile.OrgID, DisplayName: profile.DisplayName, Slug: profile.Slug, Version: profile.Version}, nil
}

func (s *Service) emitSignupEvent(ctx context.Context, store interface {
	AppendIAMEvent(context.Context, IAMEvent) error
}, event IAMEvent,
) error {
	if event.TraceID == "" {
		if span := trace.SpanContextFromContext(ctx); span.HasTraceID() {
			event.TraceID = span.TraceID().String()
		}
	}
	return store.AppendIAMEvent(ctx, event)
}

func (s *Service) policyWriter() (OrganizationOwnerPolicyWriter, error) {
	if s == nil || s.PolicyWriter == nil {
		return nil, ErrAuthzUnavailable
	}
	return s.PolicyWriter, nil
}

func (s *Service) billing() (BillingProvisioner, error) {
	if s == nil || s.Billing == nil {
		return nil, ErrBillingUnavailable
	}
	return s.Billing, nil
}

func (s *Service) signupStore() (SignupStore, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStoreUnavailable
	}
	store, ok := s.Store.(SignupStore)
	if !ok {
		return nil, ErrStoreUnavailable
	}
	return store, nil
}

func (s *Service) signupDirectory() (SignupDirectory, error) {
	if s == nil || s.Directory == nil {
		return nil, ErrZitadelUnavailable
	}
	directory, ok := s.Directory.(SignupDirectory)
	if !ok {
		return nil, ErrZitadelUnavailable
	}
	return directory, nil
}

func normalizeStartSignup(input StartSignupRequest) (StartSignupRequest, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return StartSignupRequest{}, err
	}
	input.Email = email
	input.OrganizationName = normalizeHumanText(input.OrganizationName)
	input.OrganizationSlug = normalizeSlug(input.OrganizationSlug)
	input.GivenName = strings.TrimSpace(input.GivenName)
	input.FamilyName = strings.TrimSpace(input.FamilyName)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if err := validateHumanText("organization_display_name", input.OrganizationName, 1, 120, 240); err != nil {
		return StartSignupRequest{}, err
	}
	if input.OrganizationSlug != "" {
		if err := validateSlug("organization_slug", input.OrganizationSlug); err != nil {
			return StartSignupRequest{}, err
		}
	}
	if input.GivenName != "" && len(input.GivenName) > 100 {
		return StartSignupRequest{}, fmt.Errorf("%w: given_name is too long", ErrInvalidInput)
	}
	if input.FamilyName != "" && len(input.FamilyName) > 100 {
		return StartSignupRequest{}, fmt.Errorf("%w: family_name is too long", ErrInvalidInput)
	}
	if input.IdempotencyKey == "" {
		return StartSignupRequest{}, fmt.Errorf("%w: idempotency_key is required", ErrInvalidInput)
	}
	return input, nil
}

func normalizeVerifySignup(input VerifySignupRequest) (VerifySignupRequest, error) {
	input.SignupIntentID = strings.TrimSpace(input.SignupIntentID)
	input.VerificationToken = strings.TrimSpace(input.VerificationToken)
	input.OrganizationDisplayName = normalizeHumanText(input.OrganizationDisplayName)
	input.OrganizationSlug = normalizeSlug(input.OrganizationSlug)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !strings.HasPrefix(input.SignupIntentID, signupIntentPublicIDPrefix) || len(input.SignupIntentID) != len(signupIntentPublicIDPrefix)+26 {
		return VerifySignupRequest{}, fmt.Errorf("%w: signup_intent_id is invalid", ErrInvalidInput)
	}
	if input.VerificationToken == "" || len(input.VerificationToken) > 512 {
		return VerifySignupRequest{}, fmt.Errorf("%w: verification token is invalid", ErrInvalidInput)
	}
	if input.IdempotencyKey == "" {
		return VerifySignupRequest{}, fmt.Errorf("%w: idempotency_key is required", ErrInvalidInput)
	}
	if input.OrganizationDisplayName != "" {
		if err := validateHumanText("organization_display_name", input.OrganizationDisplayName, 1, 120, 240); err != nil {
			return VerifySignupRequest{}, err
		}
	}
	if input.OrganizationSlug != "" {
		if err := validateSlug("organization_slug", input.OrganizationSlug); err != nil {
			return VerifySignupRequest{}, err
		}
	}
	return input, nil
}

func normalizeEmail(value string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%w: email is invalid", ErrInvalidInput)
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
}

func signupStartRequestHash(input StartSignupRequest) ([]byte, error) {
	return canonicalHash(struct {
		Email            string `json:"email"`
		OrganizationName string `json:"organization_name"`
		OrganizationSlug string `json:"organization_slug"`
		GivenName        string `json:"given_name"`
		FamilyName       string `json:"family_name"`
	}{input.Email, input.OrganizationName, input.OrganizationSlug, input.GivenName, input.FamilyName})
}

func signupVerifyRequestHash(input VerifySignupRequest, tokenHash []byte) ([]byte, error) {
	return canonicalHash(struct {
		SignupIntentID          string `json:"signup_intent_id"`
		OrganizationDisplayName string `json:"organization_display_name"`
		OrganizationSlug        string `json:"organization_slug"`
		VerificationTokenHash   string `json:"verification_token_hash"`
	}{input.SignupIntentID, input.OrganizationDisplayName, input.OrganizationSlug, base64.RawURLEncoding.EncodeToString(tokenHash)})
}

func canonicalHash(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

func randomSignupIntentID() (string, error) {
	payload, err := randomCrockfordText(26)
	if err != nil {
		return "", err
	}
	return signupIntentPublicIDPrefix + payload, nil
}

func randomSignupVerificationToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	_, hash = SecretHash(token)
	return token, hash, nil
}

func signupProviderOrganizationName(intent SignupIntent) string {
	base := normalizeSlug(firstNonEmpty(intent.RequestedOrganizationSlug, intent.OrganizationDisplayName, "organization"))
	if base == "" {
		base = "organization"
	}
	return trimSlugBase(base, 52) + "-" + strings.TrimPrefix(intent.OrgID, organizationPublicIDPrefix)
}

func signupEventAttempt(value int32) uint32 {
	if value <= 0 {
		return 0
	}
	return uint32(value)
}

func hashText(value string) string {
	fingerprint, _ := SecretHash(strings.TrimSpace(value))
	return fingerprint
}

func signupFailureTerminal(err error) bool {
	return errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrOrganizationConflict) || errors.Is(err, ErrIdempotencyConflict)
}

func signupErrorKind(err error) string {
	switch {
	case errors.Is(err, ErrZitadelUnavailable):
		return "zitadel_unavailable"
	case errors.Is(err, ErrBillingUnavailable):
		return "billing_unavailable"
	case errors.Is(err, ErrAuthzUnavailable):
		return "authz_unavailable"
	case errors.Is(err, ErrStoreUnavailable):
		return "store_unavailable"
	case errors.Is(err, ErrOrganizationConflict):
		return "organization_conflict"
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input"
	default:
		return "unknown"
	}
}

func trimErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 2048 {
		return text[:2048]
	}
	return text
}
