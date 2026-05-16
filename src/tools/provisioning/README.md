# Provisioning Tools

Provisioning tools own the bare-metal allocation boundary:

- `terraform/` contains the Latitude.sh OpenTofu project.
- `ansible/` contains the local controller playbooks that apply or destroy
  that OpenTofu state and write the host inventory consumed by render,
  substrate, and deploy commands.

Optional Cloudflare R2 backup infrastructure is also provisioned here when the
site tfvars set `r2_backups_enabled = true`. The Cloudflare provider reads
`CLOUDFLARE_API_TOKEN` from `src/host/sites/<site>/secrets/provisioning.sops.yml`
key `cloudflare_api_token`, with a controller environment variable fallback.
OpenTofu creates scoped R2 runtime and restore tokens, so the local OpenTofu
state is Tier 0 secret material.

Use the explicit command surface:

```bash
aspect provision apply
aspect provision destroy --confirm
```

Provisioning stops after inventory exists. Host and daemon convergence is
`src/host/`; application rollout is Nomad through `aspect deploy`.
