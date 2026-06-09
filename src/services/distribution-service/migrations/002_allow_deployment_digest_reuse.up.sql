ALTER TABLE distribution_artifacts
    DROP CONSTRAINT IF EXISTS distribution_artifacts_oci_repository_oci_digest_key,
    DROP CONSTRAINT IF EXISTS distribution_artifacts_package_name_platform_os_platform_ar_key;

DO $$
DECLARE
    has_coordinate_unique BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'distribution_artifacts'::regclass
          AND contype = 'u'
          AND conkey = ARRAY[
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'package_name'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'package_version'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'channel_name'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'platform_os'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'platform_arch'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_artifacts'::regclass AND attname = 'flavor')
          ]::int2[]
    ) INTO has_coordinate_unique;

    IF NOT has_coordinate_unique THEN
        ALTER TABLE distribution_artifacts
            ADD CONSTRAINT distribution_artifacts_package_version_channel_key
            UNIQUE (package_name, package_version, channel_name, platform_os, platform_arch, flavor);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS distribution_artifacts_repository_digest_idx
    ON distribution_artifacts (oci_repository, oci_digest, available_at DESC);

ALTER TABLE distribution_channel_targets
    DROP CONSTRAINT IF EXISTS distribution_channel_targets_package_name_channel_name_plat_key;

DO $$
DECLARE
    has_target_unique BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'distribution_channel_targets'::regclass
          AND contype = 'u'
          AND conkey = ARRAY[
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'package_name'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'channel_name'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'platform_os'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'platform_arch'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'flavor'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'package_version'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = 'distribution_channel_targets'::regclass AND attname = 'artifact_digest')
          ]::int2[]
    ) INTO has_target_unique;

    IF NOT has_target_unique THEN
        ALTER TABLE distribution_channel_targets
            ADD CONSTRAINT distribution_channel_targets_package_version_digest_key
            UNIQUE (package_name, channel_name, platform_os, platform_arch, flavor, package_version, artifact_digest);
    END IF;
END $$;
