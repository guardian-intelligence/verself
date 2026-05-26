UPDATE runner_class_filesystem_mounts
SET mount_path = '/opt/verself/toolchains/github-actions-runner',
    updated_at = now()
WHERE mount_name = 'gh-actions-runner'
  AND source_ref = 'gh-actions-runner';
