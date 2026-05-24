UPDATE github_installation_repositories ir
SET state = 'selected',
    updated_at = now()
FROM github_installations i,
     github_repositories r
WHERE i.provider_installation_id = ir.provider_installation_id
  AND r.provider_repository_id = ir.provider_repository_id
  AND i.state = 'active'
  AND r.state = 'active'
  AND ir.state = 'active';
