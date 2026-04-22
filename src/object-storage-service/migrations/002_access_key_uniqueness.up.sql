ALTER TABLE object_storage_credentials
    DROP CONSTRAINT object_storage_credentials_access_key_id_key;

CREATE UNIQUE INDEX object_storage_credentials_access_key_nonempty_idx
    ON object_storage_credentials (access_key_id)
    WHERE access_key_id <> '';
