package distribution

import "errors"

var (
	ErrInvalid              = errors.New("distribution invalid request")
	ErrNotFound             = errors.New("distribution resource not found")
	ErrConflict             = errors.New("distribution conflict")
	ErrIdempotencyMismatch  = errors.New("distribution idempotency payload mismatch")
	ErrPolicyDenied         = errors.New("distribution policy denied")
	ErrMissingReferrers     = errors.New("distribution missing oci referrers")
	ErrDigestMismatch       = errors.New("distribution digest mismatch")
	ErrUntrustedBuilder     = errors.New("distribution untrusted builder")
	ErrUntrustedSigner      = errors.New("distribution untrusted signer")
	ErrSourcePolicyFailure  = errors.New("distribution source policy failure")
	ErrChannelPolicyFailure = errors.New("distribution channel policy failure")
	ErrQuarantined          = errors.New("distribution artifact quarantined")
	ErrReplication          = errors.New("distribution replication failure")
	ErrStoreUnavailable     = errors.New("distribution store unavailable")
	ErrRegistryUnavailable  = errors.New("distribution registry unavailable")
)
