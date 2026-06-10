-- Deployment-channel evidence is now a verified sigstore bundle: persist the
-- derived verifying-key identity and forbid self-asserted signer identities on
-- the deployment channel.

ALTER TABLE distribution_artifact_evidence
    ADD COLUMN signing_key_id TEXT NOT NULL DEFAULT ''
    CHECK (signing_key_id = '' OR signing_key_id ~ '^[a-f0-9]{64}$');

-- Drop the 001 non-empty CHECK first: it still fires on the cutover UPDATE
-- below, which would abort the migration on any DB with deployment rows.
ALTER TABLE distribution_artifacts
    DROP CONSTRAINT distribution_artifacts_signer_identity_check;

-- Cut over pre-existing deployment rows that carried the old self-asserted
-- builder identity as signer_identity.
UPDATE distribution_artifacts
SET signer_identity = ''
WHERE channel_name = 'deployment';

ALTER TABLE distribution_artifacts
    ADD CONSTRAINT distribution_artifacts_signer_identity_check
    CHECK (
        (channel_name = 'deployment' AND signer_identity = '')
        OR (channel_name <> 'deployment' AND length(btrim(signer_identity)) > 0)
    );
