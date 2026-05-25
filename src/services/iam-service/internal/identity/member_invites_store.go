package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	identitystore "github.com/verself/iam-service/internal/store"
)

func (s SQLStore) CreateMemberInviteAcceptance(ctx context.Context, invite MemberInviteAcceptance) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if err := s.q().InsertMemberInviteAcceptanceToken(ctx, identitystore.InsertMemberInviteAcceptanceTokenParams{
		TokenHash:             invite.TokenHash,
		OrgID:                 invite.OrgID,
		UserID:                invite.UserID,
		Email:                 invite.Email,
		EmailVerificationCode: invite.EmailVerificationCode,
		CreatedAt:             timestamptz(invite.CreatedAt),
		ExpiresAt:             timestamptz(invite.ExpiresAt),
	}); err != nil {
		return fmt.Errorf("%w: create invite acceptance token: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s SQLStore) GetMemberInviteAcceptance(ctx context.Context, tokenHash string, now time.Time) (MemberInviteAcceptance, error) {
	if s.PG == nil {
		return MemberInviteAcceptance{}, ErrStoreUnavailable
	}
	row, err := s.q().GetActiveMemberInviteAcceptanceToken(ctx, identitystore.GetActiveMemberInviteAcceptanceTokenParams{
		TokenHash: tokenHash,
		Now:       timestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberInviteAcceptance{}, ErrMemberMissing
	}
	if err != nil {
		return MemberInviteAcceptance{}, fmt.Errorf("%w: get invite acceptance token: %v", ErrStoreUnavailable, err)
	}
	return memberInviteAcceptanceFromRow(row)
}

func (s SQLStore) AcceptMemberInviteAcceptance(ctx context.Context, tokenHash string, now time.Time) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	rows, err := s.q().AcceptMemberInviteAcceptanceToken(ctx, identitystore.AcceptMemberInviteAcceptanceTokenParams{
		TokenHash:  tokenHash,
		AcceptedAt: timestamptz(now),
	})
	if err != nil {
		return fmt.Errorf("%w: accept invite acceptance token: %v", ErrStoreUnavailable, err)
	}
	if rows == 0 {
		return ErrMemberMissing
	}
	return nil
}

func memberInviteAcceptanceFromRow(row identitystore.IamMemberInviteAcceptanceToken) (MemberInviteAcceptance, error) {
	createdAt, err := requiredTime(row.CreatedAt, "created_at")
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	expiresAt, err := requiredTime(row.ExpiresAt, "expires_at")
	if err != nil {
		return MemberInviteAcceptance{}, err
	}
	return MemberInviteAcceptance{
		TokenHash:             row.TokenHash,
		OrgID:                 row.OrgID,
		UserID:                row.UserID,
		Email:                 row.Email,
		EmailVerificationCode: row.EmailVerificationCode,
		CreatedAt:             createdAt,
		ExpiresAt:             expiresAt,
		AcceptedAt:            nullableTime(row.AcceptedAt),
	}, nil
}
