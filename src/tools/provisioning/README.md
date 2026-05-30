# Provisioning Tools

Provisioning is intentionally narrow: this directory contains the OpenTofu
module that allocates bare-metal hosts and optional recovery storage. The
operator-facing command surface is:

```text
aspect site allocate --site=<site> --confirm
aspect site destroy --site=<site> --confirm
```

Provider bootstrap credentials are controller environment variables, not repo
secrets:

- `LATITUDESH_AUTH_TOKEN`
- `CLOUDFLARE_API_TOKEN` when the site tfvars enable Cloudflare R2 recovery
  storage

`aspect site allocate` uses site-local OpenTofu state under `.verself/` and
writes `src/host/sites/<site>/inventory.ini`. Host convergence is
`aspect site bootstrap`; service deployment is Nomad through `aspect deploy`.
