package identity

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrOrganizationConflict = errors.New("organization conflict")
	ErrOrganizationMissing  = errors.New("organization missing")
	ErrMemberMissing        = errors.New("member missing")
	ErrAPICredentialMissing = errors.New("api credential missing")
	ErrStoreUnavailable     = errors.New("iam store unavailable")
	ErrZitadelUnavailable   = errors.New("zitadel unavailable")
)
