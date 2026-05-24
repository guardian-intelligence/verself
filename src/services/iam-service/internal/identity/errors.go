package identity

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrOrganizationConflict = errors.New("organization conflict")
	ErrOrganizationMissing  = errors.New("organization missing")
	ErrMemberMissing        = errors.New("member missing")
	ErrAPICredentialMissing = errors.New("api credential missing")
	ErrSignupIntentMissing  = errors.New("signup intent missing")
	ErrSignupIntentExpired  = errors.New("signup intent expired")
	ErrSignupIntentConflict = errors.New("signup intent conflict")
	ErrSignupMaterializing  = errors.New("signup intent materializing")
	ErrIdempotencyConflict  = errors.New("idempotency payload mismatch")
	ErrStoreUnavailable     = errors.New("iam store unavailable")
	ErrZitadelUnavailable   = errors.New("zitadel unavailable")
	ErrBillingUnavailable   = errors.New("billing unavailable")
	ErrAuthzUnavailable     = errors.New("authorization graph unavailable")
)
