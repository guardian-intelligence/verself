package identity

import "errors"

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrInvalidPolicy      = errors.New("invalid policy")
	ErrPolicyConflict     = errors.New("policy conflict")
	ErrMemberMissing      = errors.New("member missing")
	ErrStoreUnavailable   = errors.New("identity store unavailable")
	ErrZitadelUnavailable = errors.New("zitadel unavailable")
)
