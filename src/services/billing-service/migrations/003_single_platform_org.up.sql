CREATE UNIQUE INDEX IF NOT EXISTS orgs_single_platform_trust_tier_idx
    ON orgs (trust_tier)
    WHERE trust_tier = 'platform';
