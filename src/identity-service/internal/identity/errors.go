package identity

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrInvalidCapabilities  = errors.New("invalid capabilities")
	ErrCapabilitiesConflict = errors.New("capabilities conflict")
	ErrOrgACLConflict       = errors.New("organization acl conflict")
	ErrMemberMissing        = errors.New("member missing")
	ErrAPICredentialMissing = errors.New("api credential missing")
	ErrStoreUnavailable     = errors.New("identity store unavailable")
	ErrZitadelUnavailable   = errors.New("zitadel unavailable")
)
