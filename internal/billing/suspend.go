package billing

import (
	"context"
	"fmt"
	"strconv"
)

// SuspendOrg suspends ALL active/past_due subscriptions for an org.
// Used for dispute-driven suspension where all access should be revoked.
//
// Sets subscriptions.status = 'suspended' for all active/past_due subscriptions.
// Future Reserve calls for any product return ErrOrgSuspended.
func (c *Client) SuspendOrg(ctx context.Context, orgID OrgID, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	result, err := c.pg.ExecContext(ctx, `
		UPDATE subscriptions
		SET status = 'suspended', status_changed_at = now()
		WHERE org_id = $1
		  AND status IN ('active', 'past_due')
	`, orgIDStr)
	if err != nil {
		return fmt.Errorf("suspend org: update subscriptions: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("suspend org: rows affected: %w", err)
	}

	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'org_suspended', $2::jsonb)
	`,
		orgIDStr,
		mustJSON(map[string]interface{}{
			"reason":                 reason,
			"subscriptions_affected": affected,
		}),
	); err != nil {
		return fmt.Errorf("suspend org: log billing event: %w", err)
	}

	return nil
}

// SuspendSubscription suspends a single subscription.
// Used for product-specific suspension (e.g., quota abuse on one product).
func (c *Client) SuspendSubscription(ctx context.Context, subscriptionID int64, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var orgIDStr string
	err := c.pg.QueryRowContext(ctx, `
		UPDATE subscriptions
		SET status = 'suspended', status_changed_at = now()
		WHERE subscription_id = $1
		  AND status IN ('active', 'past_due')
		RETURNING org_id
	`, subscriptionID).Scan(&orgIDStr)
	if err != nil {
		return fmt.Errorf("suspend subscription %d: %w", subscriptionID, err)
	}

	if _, err := c.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, subscription_id, payload)
		VALUES ($1, 'org_suspended', $2, $3::jsonb)
	`,
		orgIDStr,
		subscriptionID,
		mustJSON(map[string]interface{}{
			"reason":          reason,
			"subscription_id": subscriptionID,
		}),
	); err != nil {
		return fmt.Errorf("suspend subscription: log billing event: %w", err)
	}

	return nil
}
