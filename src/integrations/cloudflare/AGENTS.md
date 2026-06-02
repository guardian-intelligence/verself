# Cloudflare Integration

Cloudflare is a single global provider control plane anchored to prod authority. The repository still passes `--site=<site>` to Cloudflare tooling; that argument selects target site records, object prefixes, and child credential destinations. It does not select a site-local Cloudflare authority.

Global Cloudflare account identity and account-owned R2 buckets are declared only in `src/integrations/cloudflare/account.json`. Site files may reference Cloudflare as a consumed capability, but must not declare `cloudflare_account_id` or global R2 bucket names.

The Cloudflare account-admin pair is stored only in prod controller OpenBao:

- `kv-controller/data/integrations/cloudflare/account-admin/a`
- `kv-controller/data/integrations/cloudflare/account-admin/b`

First-site recovery may read the same pair from the controller-local ingress file `secret.env` with keys `account-admin-a` and `account-admin-b`. That path is ingress-only. Do not copy account-admin tokens into a site seed, Nomad job, Ansible vars, runtime OpenBao seed, or service environment.

Required account-admin token policies:

- Account API Tokens Read and Account API Tokens Write on the Cloudflare account.
- Workers R2 Storage Read and Workers R2 Storage Write on the Cloudflare account.
- Workers R2 Storage Bucket Item Read and Workers R2 Storage Bucket Item Write on the Cloudflare account.
- Zone Read and DNS Write for every managed hosted zone, currently `verself.sh` and any company zone reconciled by site vars.

## R2 Model

R2 is required for bootstrap and ongoing deployment. `aspect site bootstrap-deploy` publishes the initial immutable build artifacts through the bootstrap publisher credential; after Nomad starts deployment-service, normal deployments publish through the site-local Cloudflare R2 control-plane job.

The deployment artifact bucket is a global account resource declared in `account.json`:

```text
verself-deployment-artifacts
```

Target-site isolation is by object prefix and child credential scope:

```text
verself-deployment-artifacts/<site>/sha256/<artifact-sha256>/...
verself-deployment-artifacts/<site>/candidate/<deploy-run-key>/...
```

The account-admin token creates the bucket through Cloudflare's REST R2 bucket API. Runtime and bootstrap jobs receive bucket-scoped child credentials only. R2 publisher and getter credentials are delivered to site runtime as S3-compatible access key IDs plus secret access keys. The Cloudflare API token value used to create a child credential is not a site runtime secret.

- Bootstrap publisher: bucket item read/write, written as S3 credential material to `.verself/site-bootstrap/<site>/r2-publisher.env`.
- Nomad artifact getter: bucket item read, written to site seed/materialized Ansible vars.
- Object storage service admin/proxy: bucket item read/write, written to site seed or controller OpenBao depending on action.
- Deployment publisher: bucket item read/write, stored as S3 credential material in site runtime OpenBao for the site-local R2 control-plane job.

Do not create account-wide R2 child tokens. Live Cloudflare behavior requires bucket-scoped child token resources for S3-compatible credentials; account-wide R2 bucket management stays with the account-admin pair.

## DNS Model

DNS records are target-site resources inside global hosted zones. For Gamma, `verself_domain: gamma.verself.sh` maps records into hosted zone `verself.sh`; record names are rendered under the Gamma subdomain.

`cloudflare_product_zone` and `cloudflare_company_zone` name hosted zones. `verself_domain` and `company_domain` name public domains inside those zones. Do not infer the hosted zone from a subdomain site name.

The DNS child token is zone-scoped and contains Zone Read plus DNS Write for the hosted zones referenced by `cloudflare_dns_records`. The child token is written as `cloudflare_api_token` for the DNS reconciler and host roles.

## Account-Admin Rotation

The account-admin pair rotates by peer authority:

```text
verify slot A and slot B
  -> use slot A to roll/update slot B value and expiry
  -> write slot B value and token ID to prod controller OpenBao
  -> verify slot B
  -> use slot B to roll/update slot A value and expiry
  -> write slot A value and token ID to prod controller OpenBao
  -> verify both slots have distinct active token IDs
```

The token value returned by Cloudflare is available only at roll/create time. Store it immediately in prod controller OpenBao. Reports may include token ID fingerprints and value fingerprints; never print token values.

Rotation requires both slots to have Account API Tokens Read and Write. A slot that cannot read token metadata or update the peer is not a valid account-admin token.

## Child Credential Rotation

Child credentials are disposable projections from the prod account-admin pair. They are rotated by target site and capability:

```text
load account-admin slot A from prod controller OpenBao
  -> verify account-admin token status
  -> ensure required global resource exists
  -> create new bucket- or zone-scoped child token
  -> verify child token against the real provider API
  -> persist the new generation to site seed or controller OpenBao
  -> delete the newly created child token on any pre-persistence failure
```

Provider verification is part of the state transition. R2 child tokens must complete a PUT/HEAD/GET round trip. DNS child tokens must list the intended zones before persistence. Verification may retry short-lived 401/403 responses because newly created Cloudflare tokens can require a short propagation interval; other provider errors are data and should fail the transition.

## Bootstrap Boundary

Bootstrap may use `--account-admin-source=secret-env` only when prod controller OpenBao is not reachable. That source is for provisioning child credentials and R2 buckets from local ingress material. It is not valid for account-admin rotation.

Bootstrap seeds contain machine-provisioned Cloudflare children and generated host secrets. Product-provider secrets such as Stripe, Resend, and GitHub App private material do not belong in the S0-S7 site seed. Those providers enter site OpenBao through their service-owned lifecycle after OpenBao and Nomad are available.

## Module Boundaries

- `control-plane/`: prod-owned Cloudflare authority, account-admin pair verification/rotation, DNS child provisioning, R2 bucket creation, and child credential provisioning.
- `r2-control-plane/`: site-local runtime upload-session service. It consumes a scoped publisher S3 credential and locally signs temporary R2 upload credentials. It does not hold account-admin authority or Cloudflare API token values.
- `dns-reconciler/`: applies target-site DNS desired state using a scoped DNS child token. It does not mint or rotate tokens.
- `email-routing/`: zone email-routing automation. It must use scoped zone credentials and must not depend on account-admin material.

Generated local files under `.verself/site-bootstrap/<site>/` are operator bootstrap artifacts. Do not commit them.
