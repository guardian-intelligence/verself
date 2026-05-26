package release

import "errors"

var (
	ErrInvalid          = errors.New("release invalid request")
	ErrNotFound         = errors.New("release resource not found")
	ErrConflict         = errors.New("release conflict")
	ErrPolicyDenied     = errors.New("release policy denied")
	ErrStateConflict    = errors.New("release state conflict")
	ErrStoreUnavailable = errors.New("release store unavailable")
	ErrDistribution     = errors.New("release distribution unavailable")
)
