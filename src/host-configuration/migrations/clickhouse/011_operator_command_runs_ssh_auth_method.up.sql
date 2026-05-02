ALTER TABLE verself.operator_command_runs
    RENAME COLUMN IF EXISTS actor_device TO ssh_auth_method;
