package billing

import (
	"errors"
	"fmt"
)

const (
	defaultReservationWindowSecs = 300
	defaultRenewSlackSecs        = 60
	defaultPendingTimeoutSecs    = 3600
	defaultTigerBeetleAddress    = "127.0.0.1:3320"
	defaultTigerBeetleClusterID  = 0
)

// Config controls billing client behavior and backing service connectivity.
type Config struct {
	ReservationWindowSecs uint32
	RenewSlackSecs        uint32
	PendingTimeoutSecs    uint32
	StripeSecretKey       string
	TigerBeetleAddresses  []string
	TigerBeetleClusterID  uint64
}

// DefaultConfig returns the normative billing defaults from the spec.
func DefaultConfig() Config {
	return Config{
		ReservationWindowSecs: defaultReservationWindowSecs,
		RenewSlackSecs:        defaultRenewSlackSecs,
		PendingTimeoutSecs:    defaultPendingTimeoutSecs,
		TigerBeetleAddresses:  []string{defaultTigerBeetleAddress},
		TigerBeetleClusterID:  defaultTigerBeetleClusterID,
	}
}

// Validate enforces the process-level configuration invariants from the spec.
func (c Config) Validate() error {
	var problems []error

	if c.ReservationWindowSecs < 60 || c.ReservationWindowSecs > 600 {
		problems = append(problems, fmt.Errorf("reservation_window_secs must be in [60,600], got %d", c.ReservationWindowSecs))
	}
	if c.RenewSlackSecs == 0 {
		problems = append(problems, errors.New("renew_slack_secs must be greater than zero"))
	}
	if c.RenewSlackSecs >= c.ReservationWindowSecs {
		problems = append(problems, fmt.Errorf("renew_slack_secs must be less than reservation_window_secs"))
	}
	if c.PendingTimeoutSecs < 600 || c.PendingTimeoutSecs > 7200 {
		problems = append(problems, fmt.Errorf("pending_timeout_secs must be in [600,7200], got %d", c.PendingTimeoutSecs))
	}
	if c.PendingTimeoutSecs <= c.ReservationWindowSecs {
		problems = append(problems, fmt.Errorf("pending_timeout_secs must be greater than reservation_window_secs"))
	}
	if c.StripeSecretKey == "" {
		problems = append(problems, errors.New("stripe_secret_key is required"))
	}
	if len(c.TigerBeetleAddresses) == 0 {
		problems = append(problems, errors.New("at least one tigerbeetle address is required"))
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(problems...))
	}

	return nil
}
