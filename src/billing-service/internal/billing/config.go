package billing

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultPendingTimeoutSecs   = 3600
	defaultTigerBeetleAddress   = "127.0.0.1:3320"
	defaultTigerBeetleClusterID = 0
	defaultSubscriptionGrace    = 7 * 24 * time.Hour
	defaultProjectorInterval    = 200 * time.Millisecond
	defaultReconcilerInterval   = time.Hour
)

type Config struct {
	PendingTimeoutSecs        uint32
	StripeSecretKey           string
	TigerBeetleAddresses      []string
	TigerBeetleClusterID      uint64
	SubscriptionGracePeriod   time.Duration
	EntitlementReconcileEvery time.Duration
	OutboxProjectEvery        time.Duration
}

func DefaultConfig() Config {
	return Config{
		PendingTimeoutSecs:        defaultPendingTimeoutSecs,
		TigerBeetleAddresses:      []string{defaultTigerBeetleAddress},
		TigerBeetleClusterID:      defaultTigerBeetleClusterID,
		SubscriptionGracePeriod:   defaultSubscriptionGrace,
		EntitlementReconcileEvery: defaultReconcilerInterval,
		OutboxProjectEvery:        defaultProjectorInterval,
	}
}

func (c Config) Validate() error {
	var problems []error

	if c.PendingTimeoutSecs < 60 || c.PendingTimeoutSecs > 7200 {
		problems = append(problems, fmt.Errorf("pending_timeout_secs must be in [60,7200], got %d", c.PendingTimeoutSecs))
	}
	if c.StripeSecretKey == "" {
		problems = append(problems, errors.New("stripe_secret_key is required"))
	}
	if len(c.TigerBeetleAddresses) == 0 {
		problems = append(problems, errors.New("at least one tigerbeetle address is required"))
	}
	if c.SubscriptionGracePeriod < 0 {
		problems = append(problems, errors.New("subscription_grace_period must be non-negative"))
	}
	if c.EntitlementReconcileEvery <= 0 {
		problems = append(problems, errors.New("entitlement_reconcile_every must be positive"))
	}
	if c.OutboxProjectEvery <= 0 {
		problems = append(problems, errors.New("outbox_project_every must be positive"))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(problems...))
	}
	return nil
}
