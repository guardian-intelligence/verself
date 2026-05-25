package identity

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
	if err := lockSignupEmailHash(ctx, tx, intent.EmailHash); err != nil {
		return SignupStartDecision{}, err
	}
	q := identitystore.New(tx)
	if completed, err := q.GetCompletedSignupIntentByEmailHashForUpdate(ctx, identitystore.GetCompletedSignupIntentByEmailHashForUpdateParams{EmailHash: append([]byte(nil), intent.EmailHash...)}); err == nil {
		converted, err := signupIntentFromRow(completed)
		if err != nil {
			return SignupStartDecision{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SignupStartDecision{}, fmt.Errorf("%w: commit completed signup start: %v", ErrStoreUnavailable, err)
		}
		return SignupStartDecision{Intent: converted, Outcome: SignupStartOutcomeExistingAccountSuppressed}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return SignupStartDecision{}, fmt.Errorf("%w: load completed signup intent by email: %v", ErrStoreUnavailable, err)
	}
	if reusable, err := q.GetReusableSignupIntentByEmailHashForUpdate(ctx, identitystore.GetReusableSignupIntentByEmailHashForUpdateParams{EmailHash: append([]byte(nil), intent.EmailHash...)}); err == nil {
		if reusable.State == string(SignupIntentStatePendingVerification) && now.Before(reusable.UpdatedAt.Time.Add(signupVerificationResendInterval)) {
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
	if inflight, err := q.GetInFlightSignupIntentByEmailHashForUpdate(ctx, identitystore.GetInFlightSignupIntentByEmailHashForUpdateParams{EmailHash: append([]byte(nil), intent.EmailHash...)}); err == nil {
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
		Email:                     intent.Email,
		EmailHash:                 append([]byte(nil), intent.EmailHash...),
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

func lockSignupEmailHash(ctx context.Context, tx pgx.Tx, emailHash []byte) error {
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
	if !bytes.Equal(intent.VerificationTokenHash, verificationTokenHash) {
		return SignupIntent{}, ErrSignupIntentMissing
	}
	if intent.VerifyIdempotencyKey != "" {
		if intent.VerifyIdempotencyKey != idempotencyKey || !bytes.Equal(intent.VerifyRequestHash, verifyRequestHash) {
			return SignupIntent{}, ErrIdempotencyConflict
		}
		if intent.State == SignupIntentStateCompleted {
			if err := tx.Commit(ctx); err != nil {
				return SignupIntent{}, fmt.Errorf("%w: commit completed signup intent read: %v", ErrStoreUnavailable, err)
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
		Email:                       row.Email,
		EmailHash:                   append([]byte(nil), row.EmailHash...),
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
		IdentityProviderOrgID:       row.IdentityProviderOrgID,
		IdentityProviderUserID:      row.IdentityProviderUserID,
		CreatedAt:                   createdAt,
		UpdatedAt:                   updatedAt,
		VerificationExpiresAt:       expiresAt,
		VerifiedAt:                  nullableTime(row.VerifiedAt),
		CompletedAt:                 nullableTime(row.CompletedAt),
	}, nil
}
