package identity

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identitystore "github.com/verself/iam-service/internal/store"
)

const signupVerificationResendInterval = time.Minute

func (s SQLStore) StartSignupIntent(ctx context.Context, intent SignupIntent, now time.Time) (SignupStartDecision, error) {
	if s.PG == nil {
		return SignupStartDecision{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SignupStartDecision{}, fmt.Errorf("%w: begin signup start: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := lockSignupEmailIdentityHash(ctx, tx, intent.EmailIdentityHash); err != nil {
		return SignupStartDecision{}, err
	}
	q := identitystore.New(tx)
	if strings.TrimSpace(intent.EmailIdentityKey) == "" {
		return SignupStartDecision{}, fmt.Errorf("%w: signup email identity key is required", ErrInvalidInput)
	}
	if err := q.LockAccountEmailIdentityKey(ctx, identitystore.LockAccountEmailIdentityKeyParams{EmailIdentityKey: intent.EmailIdentityKey}); err != nil {
		return SignupStartDecision{}, fmt.Errorf("%w: lock signup account email: %v", ErrStoreUnavailable, err)
	}
	if accountEmail, err := q.GetAccountEmailByIdentityKeyForUpdate(ctx, identitystore.GetAccountEmailByIdentityKeyForUpdateParams{EmailIdentityKey: intent.EmailIdentityKey}); err == nil {
		converted := signupAccountEmailFromRow(accountEmail)
		outcome := SignupStartOutcomeAccountExistsNotice
		if converted.AccountState == AccountStateProvisioning {
			outcome = SignupStartOutcomeInFlightSuppressed
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: commit account email signup start: %v", ErrStoreUnavailable, err)
		}
		return SignupStartDecision{AccountEmail: converted, Outcome: outcome}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, fmt.Errorf("%w: load account email by identity: %v", ErrStoreUnavailable, err)
	}
	if reusable, err := q.GetReusableSignupIntentByEmailIdentityHashForUpdate(ctx, identitystore.GetReusableSignupIntentByEmailIdentityHashForUpdateParams{EmailIdentityHash: append([]byte(nil), intent.EmailIdentityHash...)}); err == nil {
		if reusable.State != string(SignupIntentStateExpired) && now.Before(reusable.UpdatedAt.Time.Add(signupVerificationResendInterval)) {
			converted, err := signupIntentFromRow(reusable)
			if err != nil {
				return SignupStartDecision{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return SignupStartDecision{}, fmt.Errorf("%w: commit signup resend throttle: %v", ErrStoreUnavailable, err)
			}
			return SignupStartDecision{Intent: converted, Outcome: SignupStartOutcomeVerificationRecentlySent}, nil
		}
		rotated, err := q.RotateReusableSignupIntentVerification(ctx, identitystore.RotateReusableSignupIntentVerificationParams{
			SignupIntentID:            reusable.SignupIntentID,
			RequestHash:               append([]byte(nil), intent.RequestHash...),
			EmailDelivery:             intent.EmailDelivery,
			OrganizationDisplayName:   intent.OrganizationDisplayName,
			RequestedOrganizationSlug: intent.RequestedOrganizationSlug,
			GivenName:                 intent.GivenName,
			FamilyName:                intent.FamilyName,
			VerificationTokenHash:     append([]byte(nil), intent.VerificationTokenHash...),
			VerificationExpiresAt:     timestamptz(intent.VerificationExpiresAt),
		})
		if err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: rotate signup verification token: %v", ErrStoreUnavailable, err)
		}
		converted, err := signupIntentFromRow(rotated)
		if err != nil {
			return SignupStartDecision{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: commit signup verification resend: %v", ErrStoreUnavailable, err)
		}
		return SignupStartDecision{Intent: converted, Outcome: SignupStartOutcomeVerificationResent}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, fmt.Errorf("%w: load reusable signup intent by email: %v", ErrStoreUnavailable, err)
	}
	if inflight, err := q.GetInFlightSignupIntentByEmailIdentityHashForUpdate(ctx, identitystore.GetInFlightSignupIntentByEmailIdentityHashForUpdateParams{EmailIdentityHash: append([]byte(nil), intent.EmailIdentityHash...)}); err == nil {
		converted, err := signupIntentFromRow(inflight)
		if err != nil {
			return SignupStartDecision{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: commit inflight signup start: %v", ErrStoreUnavailable, err)
		}
		return SignupStartDecision{Intent: converted, Outcome: SignupStartOutcomeInFlightSuppressed}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, fmt.Errorf("%w: load inflight signup intent by email: %v", ErrStoreUnavailable, err)
	}
	row, err := q.InsertSignupIntent(ctx, identitystore.InsertSignupIntentParams{
		SignupIntentID:            intent.SignupIntentID,
		IdempotencyKey:            intent.IdempotencyKey,
		RequestHash:               append([]byte(nil), intent.RequestHash...),
		EmailDelivery:             intent.EmailDelivery,
		EmailIdentityHash:         append([]byte(nil), intent.EmailIdentityHash...),
		EmailIdentityHashKeyID:    intent.EmailIdentityHashKeyID,
		OrganizationDisplayName:   intent.OrganizationDisplayName,
		RequestedOrganizationSlug: intent.RequestedOrganizationSlug,
		GivenName:                 intent.GivenName,
		FamilyName:                intent.FamilyName,
		VerificationTokenHash:     append([]byte(nil), intent.VerificationTokenHash...),
		OrgID:                     intent.OrgID,
		VerificationExpiresAt:     timestamptz(intent.VerificationExpiresAt),
	})
	if err == nil {
		created, convErr := signupIntentFromRow(row)
		if convErr != nil {
			return SignupStartDecision{}, convErr
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: commit signup intent create: %v", ErrStoreUnavailable, err)
		}
		return SignupStartDecision{Intent: created, Outcome: SignupStartOutcomeCreated}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, fmt.Errorf("%w: create signup intent: %v", ErrStoreUnavailable, err)
	}
	existing, err := q.GetSignupIntentByIdempotencyKey(ctx, identitystore.GetSignupIntentByIdempotencyKeyParams{IdempotencyKey: intent.IdempotencyKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, ErrSignupIntentMissing
	}
	if err != nil {
		return SignupStartDecision{}, fmt.Errorf("%w: load signup intent by idempotency key: %v", ErrStoreUnavailable, err)
	}
	converted, err := signupIntentFromRow(existing)
	if err != nil {
		return SignupStartDecision{}, err
	}
	if !bytes.Equal(converted.RequestHash, intent.RequestHash) {
		return SignupStartDecision{}, ErrIdempotencyConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return SignupStartDecision{}, fmt.Errorf("%w: commit signup idempotent replay: %v", ErrStoreUnavailable, err)
	}
	return SignupStartDecision{Intent: converted, Outcome: SignupStartOutcomeInFlightSuppressed}, nil
}

func lockSignupEmailIdentityHash(ctx context.Context, tx pgx.Tx, emailHash []byte) error {
	if len(emailHash) < 8 {
		return fmt.Errorf("%w: signup email hash is invalid", ErrInvalidInput)
	}
	var lockKey int64
	if err := binary.Read(bytes.NewReader(emailHash[:8]), binary.BigEndian, &lockKey); err != nil {
		return fmt.Errorf("%w: decode signup email lock hash: %v", ErrInvalidInput, err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return fmt.Errorf("%w: lock signup email hash: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) DeletePendingSignupIntent(ctx context.Context, signupIntentID string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := s.q().DeletePendingSignupIntent(ctx, identitystore.DeletePendingSignupIntentParams{SignupIntentID: signupIntentID}); err != nil {
		return fmt.Errorf("%w: delete pending signup intent: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) ClaimSignupIntentForVerification(ctx context.Context, signupIntentID string, verificationTokenHash []byte, idempotencyKey string, verifyRequestHash []byte, organizationDisplayName string, requestedOrganizationSlug string, now time.Time, leaseExpiresAt time.Time) (SignupIntent, error) {
	if s.PG == nil {
		return SignupIntent{}, ErrStoreUnavailable
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SignupIntent{}, fmt.Errorf("%w: begin signup verification claim: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	row, err := q.GetSignupIntentForUpdate(ctx, identitystore.GetSignupIntentForUpdateParams{SignupIntentID: signupIntentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SignupIntent{}, ErrSignupIntentMissing
	}
	if err != nil {
		return SignupIntent{}, fmt.Errorf("%w: lock signup intent: %v", ErrStoreUnavailable, err)
	}
	intent, err := signupIntentFromRow(row)
	if err != nil {
		return SignupIntent{}, err
	}
	if now.After(intent.VerificationExpiresAt) && intent.State == SignupIntentStatePendingVerification {
		if err := q.MarkSignupIntentExpired(ctx, identitystore.MarkSignupIntentExpiredParams{SignupIntentID: signupIntentID}); err != nil {
			return SignupIntent{}, fmt.Errorf("%w: mark signup intent expired: %v", ErrStoreUnavailable, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupIntent{}, fmt.Errorf("%w: commit signup intent expiration: %v", ErrStoreUnavailable, err)
		}
		return SignupIntent{}, ErrSignupIntentExpired
	}
	if subtle.ConstantTimeCompare(intent.VerificationTokenHash, verificationTokenHash) != 1 {
		return SignupIntent{}, ErrSignupIntentMissing
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
			if err := tx.Commit(ctx); err != nil {
				return SignupIntent{}, fmt.Errorf("%w: commit completed verification read: %v", ErrStoreUnavailable, err)
			}
			return intent, nil
		}
	}
	if intent.State == SignupIntentStateMaterializing {
		if intent.MaterializationLeaseExpires != nil && intent.MaterializationLeaseExpires.After(now) {
			return SignupIntent{}, ErrSignupMaterializing
		}
	} else if intent.State != SignupIntentStatePendingVerification && intent.State != SignupIntentStateFailedRetryable {
		return SignupIntent{}, ErrSignupIntentConflict
	}
	if err := q.MarkSignupIntentMaterializing(ctx, identitystore.MarkSignupIntentMaterializingParams{
		VerifyIdempotencyKey:          idempotencyKey,
		VerifyRequestHash:             append([]byte(nil), verifyRequestHash...),
		OrganizationDisplayName:       organizationDisplayName,
		RequestedOrganizationSlug:     requestedOrganizationSlug,
		VerifiedAt:                    timestamptz(now),
		MaterializationLeaseExpiresAt: timestamptz(leaseExpiresAt),
		SignupIntentID:                signupIntentID,
	}); err != nil {
		return SignupIntent{}, fmt.Errorf("%w: mark signup intent materializing: %v", ErrStoreUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SignupIntent{}, fmt.Errorf("%w: commit signup verification claim: %v", ErrStoreUnavailable, err)
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
	intent.MaterializationLastError = ""
	intent.MaterializationLeaseExpires = &leaseExpiresAt
	if intent.VerifiedAt == nil {
		intent.VerifiedAt = &now
	}
	return intent, nil
}

func signupRetryCanChangeRequest(intent SignupIntent) bool {
	return intent.State == SignupIntentStateFailedRetryable &&
		intent.IdentityProviderOrgID == "" &&
		intent.IdentityProviderUserID == "" &&
		intent.OrganizationSlug == ""
}

func (s SQLStore) RecordSignupIntentStep(ctx context.Context, signupIntentID, step string, leaseExpiresAt time.Time) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().MarkSignupIntentStep(ctx, identitystore.MarkSignupIntentStepParams{
		MaterializationStep:           step,
		MaterializationLeaseExpiresAt: timestamptz(leaseExpiresAt),
		SignupIntentID:                signupIntentID,
	}); err != nil {
		return fmt.Errorf("%w: mark signup intent step: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) RecordSignupIntentAccount(ctx context.Context, signupIntentID, accountID string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().RecordSignupIntentAccount(ctx, identitystore.RecordSignupIntentAccountParams{
		SignupIntentID: signupIntentID,
		AccountID:      accountID,
	}); err != nil {
		return fmt.Errorf("%w: record signup account: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) ReserveSignupAccountEmail(ctx context.Context, input SignupAccountEmailReservation) (SignupAccountEmail, error) {
	if s.PG == nil {
		return SignupAccountEmail{}, ErrStoreUnavailable
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.EmailDelivery = strings.TrimSpace(input.EmailDelivery)
	input.EmailIdentityKey = strings.TrimSpace(input.EmailIdentityKey)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.AccountID == "" || input.EmailDelivery == "" || input.EmailIdentityKey == "" || input.DisplayName == "" {
		return SignupAccountEmail{}, fmt.Errorf("%w: account_id, email, email identity key, and display name are required", ErrInvalidInput)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SignupAccountEmail{}, fmt.Errorf("%w: begin signup account reservation: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.LockAccountEmailIdentityKey(ctx, identitystore.LockAccountEmailIdentityKeyParams{EmailIdentityKey: input.EmailIdentityKey}); err != nil {
		return SignupAccountEmail{}, fmt.Errorf("%w: lock signup account email: %v", ErrStoreUnavailable, err)
	}
	existing, err := q.GetAccountEmailByIdentityKeyForUpdate(ctx, identitystore.GetAccountEmailByIdentityKeyForUpdateParams{EmailIdentityKey: input.EmailIdentityKey})
	if err == nil {
		email := signupAccountEmailFromRow(existing)
		if email.AccountID != input.AccountID {
			return SignupAccountEmail{}, ErrSignupAccountExists
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupAccountEmail{}, fmt.Errorf("%w: commit existing signup account reservation: %v", ErrStoreUnavailable, err)
		}
		return email, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SignupAccountEmail{}, fmt.Errorf("%w: load signup account email: %v", ErrStoreUnavailable, err)
	}
	rows, err := q.CreateSignupAccountReservation(ctx, identitystore.CreateSignupAccountReservationParams{
		AccountID:    input.AccountID,
		PrimaryEmail: input.EmailDelivery,
		DisplayName:  input.DisplayName,
	})
	if err != nil {
		return SignupAccountEmail{}, fmt.Errorf("%w: create signup account reservation: %v", ErrStoreUnavailable, err)
	}
	if rows == 0 {
		return SignupAccountEmail{}, ErrSignupAccountExists
	}
	if err := q.InsertSignupAccountEmail(ctx, identitystore.InsertSignupAccountEmailParams{
		AccountID:        input.AccountID,
		Email:            input.EmailDelivery,
		EmailIdentityKey: input.EmailIdentityKey,
	}); err != nil {
		return SignupAccountEmail{}, fmt.Errorf("%w: insert signup account email: %v", ErrStoreUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SignupAccountEmail{}, fmt.Errorf("%w: commit signup account reservation: %v", ErrStoreUnavailable, err)
	}
	return SignupAccountEmail{
		AccountID:        input.AccountID,
		EmailDelivery:    input.EmailDelivery,
		EmailIdentityKey: input.EmailIdentityKey,
		AccountState:     AccountStateProvisioning,
	}, nil
}

func (s SQLStore) RecordSignupIntentProviderOrg(ctx context.Context, signupIntentID, providerOrgID string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().RecordSignupIntentProviderOrg(ctx, identitystore.RecordSignupIntentProviderOrgParams{SignupIntentID: signupIntentID, IdentityProviderOrgID: providerOrgID}); err != nil {
		return fmt.Errorf("%w: record signup provider org: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) RecordSignupIntentProviderUser(ctx context.Context, signupIntentID, providerUserID string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().RecordSignupIntentProviderUser(ctx, identitystore.RecordSignupIntentProviderUserParams{SignupIntentID: signupIntentID, IdentityProviderUserID: providerUserID}); err != nil {
		return fmt.Errorf("%w: record signup provider user: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) RecordSignupIntentOrganization(ctx context.Context, signupIntentID, orgID, organizationSlug string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().RecordSignupIntentOrganization(ctx, identitystore.RecordSignupIntentOrganizationParams{SignupIntentID: signupIntentID, OrgID: orgID, OrganizationSlug: organizationSlug}); err != nil {
		return fmt.Errorf("%w: record signup organization: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) CompleteSignupAccount(ctx context.Context, input SignupAccountCompletion) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.Subject = strings.TrimSpace(input.Subject)
	input.EmailDelivery = strings.TrimSpace(input.EmailDelivery)
	input.EmailIdentityKey = strings.TrimSpace(input.EmailIdentityKey)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.ConnectionID = strings.TrimSpace(input.ConnectionID)
	if input.AccountID == "" || input.Issuer == "" || input.Subject == "" || input.EmailDelivery == "" || input.EmailIdentityKey == "" || input.DisplayName == "" || input.ConnectionID == "" {
		return fmt.Errorf("%w: account completion fields are required", ErrInvalidInput)
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: begin signup account completion: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := identitystore.New(tx)
	if err := q.LockAccountEmailIdentityKey(ctx, identitystore.LockAccountEmailIdentityKeyParams{EmailIdentityKey: input.EmailIdentityKey}); err != nil {
		return fmt.Errorf("%w: lock signup account email completion: %v", ErrStoreUnavailable, err)
	}
	existing, err := q.GetAccountEmailByIdentityKeyForUpdate(ctx, identitystore.GetAccountEmailByIdentityKeyForUpdateParams{EmailIdentityKey: input.EmailIdentityKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSignupIntentConflict
	}
	if err != nil {
		return fmt.Errorf("%w: load signup account email completion: %v", ErrStoreUnavailable, err)
	}
	if existing.AccountID != input.AccountID {
		return ErrSignupAccountExists
	}
	connection, err := q.GetAccountConnectionByProviderSubject(ctx, identitystore.GetAccountConnectionByProviderSubjectParams{Issuer: input.Issuer, Subject: input.Subject})
	if err == nil && connection.AccountID != input.AccountID {
		return ErrSignupAccountExists
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: load signup account connection: %v", ErrStoreUnavailable, err)
	}
	rows, err := q.ActivateSignupAccount(ctx, identitystore.ActivateSignupAccountParams{
		AccountID:    input.AccountID,
		PrimaryEmail: input.EmailDelivery,
		DisplayName:  input.DisplayName,
	})
	if err != nil {
		return fmt.Errorf("%w: activate signup account: %v", ErrStoreUnavailable, err)
	}
	if rows == 0 {
		return ErrSignupIntentConflict
	}
	claimsJSON := []byte("{}")
	rows, err = q.UpsertAccountSubject(ctx, identitystore.UpsertAccountSubjectParams{
		Issuer:        input.Issuer,
		Subject:       input.Subject,
		AccountID:     input.AccountID,
		Email:         input.EmailDelivery,
		EmailVerified: true,
		DisplayName:   input.DisplayName,
		ClaimsJson:    claimsJSON,
	})
	if err != nil {
		return fmt.Errorf("%w: link signup account subject: %v", ErrStoreUnavailable, err)
	}
	if rows == 0 {
		return ErrSignupAccountExists
	}
	if err := q.UpsertAccountEmail(ctx, identitystore.UpsertAccountEmailParams{
		AccountID:        input.AccountID,
		Email:            input.EmailDelivery,
		EmailIdentityKey: input.EmailIdentityKey,
		Verified:         true,
		Source:           "signup",
	}); err != nil {
		return fmt.Errorf("%w: update signup account email: %v", ErrStoreUnavailable, err)
	}
	rows, err = q.UpsertAccountConnection(ctx, identitystore.UpsertAccountConnectionParams{
		ConnectionID:  input.ConnectionID,
		AccountID:     input.AccountID,
		Issuer:        input.Issuer,
		Subject:       input.Subject,
		Email:         input.EmailDelivery,
		EmailVerified: true,
	})
	if err != nil {
		return fmt.Errorf("%w: link signup account connection: %v", ErrStoreUnavailable, err)
	}
	if rows == 0 {
		return ErrSignupAccountExists
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: commit signup account completion: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) MarkSignupIntentFailed(ctx context.Context, signupIntentID string, state SignupIntentState, message string) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if state != SignupIntentStateFailedRetryable && state != SignupIntentStateFailedTerminal {
		return fmt.Errorf("%w: unsupported signup failure state %q", ErrInvalidInput, state)
	}
	if err := s.q().MarkSignupIntentFailed(ctx, identitystore.MarkSignupIntentFailedParams{
		SignupIntentID:           signupIntentID,
		State:                    string(state),
		MaterializationLastError: message,
	}); err != nil {
		return fmt.Errorf("%w: mark signup intent failed: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) CompleteSignupIntent(ctx context.Context, signupIntentID string, completedAt time.Time) (SignupIntent, error) {
	if s.PG == nil {
		return SignupIntent{}, ErrStoreUnavailable
	}
	row, err := s.q().MarkSignupIntentCompleted(ctx, identitystore.MarkSignupIntentCompletedParams{SignupIntentID: signupIntentID, CompletedAt: timestamptz(completedAt)})
	if errors.Is(err, pgx.ErrNoRows) {
		return SignupIntent{}, ErrSignupIntentMissing
	}
	if err != nil {
		return SignupIntent{}, fmt.Errorf("%w: complete signup intent: %v", ErrStoreUnavailable, err)
	}
	return signupIntentFromRow(row)
}

func signupIntentFromRow(row identitystore.IamSignupIntent) (SignupIntent, error) {
	createdAt, err := requiredTime(row.CreatedAt, "iam_signup_intents.created_at")
	if err != nil {
		return SignupIntent{}, err
	}
	updatedAt, err := requiredTime(row.UpdatedAt, "iam_signup_intents.updated_at")
	if err != nil {
		return SignupIntent{}, err
	}
	expiresAt, err := requiredTime(row.VerificationExpiresAt, "iam_signup_intents.verification_expires_at")
	if err != nil {
		return SignupIntent{}, err
	}
	return SignupIntent{
		SignupIntentID:              row.SignupIntentID,
		IdempotencyKey:              row.IdempotencyKey,
		RequestHash:                 append([]byte(nil), row.RequestHash...),
		EmailDelivery:               row.EmailDelivery,
		EmailIdentityHash:           append([]byte(nil), row.EmailIdentityHash...),
		EmailIdentityHashKeyID:      row.EmailIdentityHashKeyID,
		OrganizationDisplayName:     row.OrganizationDisplayName,
		RequestedOrganizationSlug:   row.RequestedOrganizationSlug,
		OrganizationSlug:            row.OrganizationSlug,
		GivenName:                   row.GivenName,
		FamilyName:                  row.FamilyName,
		VerificationTokenHash:       append([]byte(nil), row.VerificationTokenHash...),
		State:                       SignupIntentState(row.State),
		MaterializationStep:         row.MaterializationStep,
		MaterializationAttempts:     row.MaterializationAttempts,
		MaterializationLastError:    row.MaterializationLastError,
		MaterializationLeaseExpires: nullableTime(row.MaterializationLeaseExpiresAt),
		VerifyIdempotencyKey:        row.VerifyIdempotencyKey,
		VerifyRequestHash:           append([]byte(nil), row.VerifyRequestHash...),
		OrgID:                       row.OrgID,
		AccountID:                   row.AccountID,
		IdentityProviderOrgID:       row.IdentityProviderOrgID,
		IdentityProviderUserID:      row.IdentityProviderUserID,
		CreatedAt:                   createdAt,
		UpdatedAt:                   updatedAt,
		VerificationExpiresAt:       expiresAt,
		VerifiedAt:                  nullableTime(row.VerifiedAt),
		CompletedAt:                 nullableTime(row.CompletedAt),
	}, nil
}

func signupAccountEmailFromRow(row identitystore.GetAccountEmailByIdentityKeyForUpdateRow) SignupAccountEmail {
	return SignupAccountEmail{
		AccountID:        row.AccountID,
		EmailDelivery:    row.Email,
		EmailIdentityKey: row.EmailIdentityKey,
		AccountState:     AccountState(row.AccountState),
	}
}
