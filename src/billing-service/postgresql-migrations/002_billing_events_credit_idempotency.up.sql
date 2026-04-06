CREATE UNIQUE INDEX idx_billing_events_credits_deposited_grant
    ON billing_events (grant_id)
    WHERE event_type = 'credits_deposited' AND grant_id IS NOT NULL;
