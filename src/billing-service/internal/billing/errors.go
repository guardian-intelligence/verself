package billing

import "errors"

var (
	ErrInvalidConfig             = errors.New("billing: invalid config")
	ErrAccountConflict           = errors.New("billing: tigerbeetle account exists with conflicting attributes")
	ErrInsufficientBalance       = errors.New("billing: insufficient balance")
	ErrUnsupportedBilling        = errors.New("billing: unsupported billing mode")
	ErrWindowNotFound            = errors.New("billing: window not found")
	ErrWindowAlreadySettled      = errors.New("billing: window already settled")
	ErrWindowAlreadyVoided       = errors.New("billing: window already voided")
	ErrWindowNotReserved         = errors.New("billing: window not reserved")
	ErrOrgSuspended              = errors.New("billing: org suspended")
	ErrNoDefaultPlan             = errors.New("billing: no default plan")
	ErrDimensionMismatch         = errors.New("billing: allocation dimension missing from rate card")
	ErrPendingTransferExpired    = errors.New("billing: pending transfer expired")
	ErrWindowNotActivated        = errors.New("billing: window not activated")
	ErrNoStripeCustomer          = errors.New("billing: org has no stripe customer")
	ErrContractNotFound          = errors.New("billing: contract not found")
	ErrUnsupportedCadence        = errors.New("billing: unsupported contract cadence")
	ErrUnsupportedContractChange = errors.New("billing: unsupported contract change")
)
