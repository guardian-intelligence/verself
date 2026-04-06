package billing

import "errors"

var (
	ErrInvalidConfig          = errors.New("billing: invalid config")
	ErrAccountConflict        = errors.New("billing: tigerbeetle account exists with conflicting attributes")
	ErrInsufficientBalance    = errors.New("billing: insufficient balance")
	ErrOrgSuspended           = errors.New("billing: org suspended")
	ErrNoActiveSubscription   = errors.New("billing: no active subscription")
	ErrOverageCeilingExceeded = errors.New("billing: overage ceiling exceeded")
	ErrDimensionMismatch      = errors.New("billing: allocation dimension missing from rate card")
	ErrWebhookVerification    = errors.New("billing: webhook verification failed")
	ErrNoPriceConfigured      = errors.New("billing: no stripe price configured for cadence")
	ErrTaskNotImplemented     = errors.New("billing: task type not yet implemented")
	ErrPendingTransferExpired = errors.New("billing: pending transfer expired, requires manual reconciliation")
)