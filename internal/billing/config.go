package billing

import (
	"errors"
	"fmt"
)

const (
	defaultReservationWindowSecs = 300
	defaultPendingTimeoutSecs    = 3600
	defaultTigerBeetleAddress    = "127.0.0.1:3320"
	defaultTigerBeetleClusterID  = 0
)

// Config controls billing client behavior and backing service connectivity.
type Config struct {
	ReservationWindowSecs uint32
	PendingTimeoutSecs    uint32
	StripeSecretKey       string
	StripeWebhookSecret   string
	PgDSN                 string
	TigerBeetleAddresses  []string
	TigerBeetleClusterID  uint64
}

// DefaultConfig returns the normative billing defaults from the spec.
func DefaultConfig() Config {
	return Config{
		ReservationWindowSecs: defaultReservationWindowSecs,
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
	if c.PendingTimeoutSecs < 600 || c.PendingTimeoutSecs > 7200 {
		problems = append(problems, fmt.Errorf("pending_timeout_secs must be in [600,7200], got %d", c.PendingTimeoutSecs))
	}
	if c.PendingTimeoutSecs <= c.ReservationWindowSecs {
		problems = append(problems, fmt.Errorf("pending_timeout_secs must be greater than reservation_window_secs"))
	}
	if c.StripeSecretKey == "" {
		problems = append(problems, errors.New("stripe_secret_key is required"))
	}
	if c.StripeWebhookSecret == "" {
		problems = append(problems, errors.New("stripe_webhook_secret is required"))
	}
	if c.PgDSN == "" {
		problems = append(problems, errors.New("pg_dsn is required"))
	}
	if len(c.TigerBeetleAddresses) == 0 {
		problems = append(problems, errors.New("at least one tigerbeetle address is required"))
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(problems...))
	}

	return nil
}
