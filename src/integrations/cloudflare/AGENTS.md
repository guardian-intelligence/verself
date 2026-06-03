# Cloudflare Integration

Cloudflare is a single global provider control plane anchored to prod authority. The repository still passes `--site=<site>` to Cloudflare tooling; that argument selects target site records, object prefixes, and R2 child credential destinations. It does not select a site-local Cloudflare authority.

Site `prod` has a special infrastructure role: it owns global Cloudflare DNS and TLS/certificate control-plane operations for prod, gamma, dev, and future sites. Other sites receive derived DNS state and certificate projections. They do not receive Cloudflare DNS API tokens.

Global Cloudflare account identity and account-owned R2 buckets are declared only in `src/integrations/cloudflare/account.json`. Site files may reference Cloudflare as a consumed capability, but must not declare `cloudflare_account_id` or global R2 bucket names.

The Cloudflare account-admin pair is stored only in prod controller OpenBao:

- `kv-controller/data/integrations/cloudflare/account-admin/a`
- `kv-controller/data/integrations/cloudflare/account-admin/b`

Initial provider ingress writes the account-admin pair directly to prod controller OpenBao. Do not copy account-admin tokens into repo files, generated bootstrap vars, Nomad jobs, Ansible vars, generated artifacts, or service environments.

Required account-admin token policies:

- Account API Tokens Read and Account API Tokens Write on the Cloudflare account.
- Workers R2 Storage Read and Workers R2 Storage Write on the Cloudflare account.
- Workers R2 Storage Bucket Item Read and Workers R2 Storage Bucket Item Write on the Cloudflare account.
- Zone Read and DNS Write for every managed hosted zone, currently `verself.sh` and any company zone reconciled by site vars.

## R2 Model

R2 is required for bootstrap and ongoing deployment. `aspect site bootstrap-deploy` publishes the initial immutable build artifacts through a short-lived publisher credential minted for that run; after Nomad starts deployment-service, normal deployments publish through the site-local Cloudflare R2 control-plane job.

The deployment artifact bucket is a global account resource declared in `account.json`:

```text
verself-deployment-artifacts
```

Target-site isolation is by object prefix and child credential scope:

```text
verself-deployment-artifacts/<site>/sha256/<artifact-sha256>/...
verself-deployment-artifacts/<site>/candidate/<deploy-run-key>/...
```

The account-admin token creates the bucket through Cloudflare's REST R2 bucket API. Runtime and bootstrap jobs receive bucket-scoped child credentials only. R2 child credentials are delivered as S3-compatible access key IDs plus secret access keys. The Cloudflare API token value used to create a child credential is not a site runtime secret.

- Bootstrap publisher: bucket item read/write, minted for each `aspect site bootstrap-deploy` run, passed in memory to the local R2 helper, and revoked before the command exits.
- Nomad artifact getter: bucket item read, written to the generated bootstrap-vars JSON so Nomad can fetch initial artifacts before it can render OpenBao templates.
- Object storage service admin/proxy: bucket item read/write, written only to OpenBao runtime secret names declared by `src/services/object-storage-service/deploy/runtime-secrets.yml`.
- Deployment publisher: bucket item read/write, stored as capability metadata and projected into the OpenBao runtime names declared by `src/integrations/cloudflare/r2-control-plane/deploy/runtime-secrets.yml`.

Do not create account-wide R2 child tokens. Live Cloudflare behavior requires bucket-scoped child token resources for S3-compatible credentials; account-wide R2 bucket management stays with the account-admin pair.

## DNS Model

DNS records are target-site resources inside global hosted zones. For Gamma, `verself_domain: gamma.verself.sh` maps records into hosted zone `verself.sh`; record names are rendered under the Gamma subdomain.

`cloudflare_product_zone` and `cloudflare_company_zone` name hosted zones. `verself_domain` and `company_domain` name public domains inside those zones. Do not infer the hosted zone from a subdomain site name.

The prod Cloudflare control plane reconciles DNS using the account-admin pair. `aspect integrations cloudflare-control-plane --site=<site> --action=reconcile-dns` reads the prod account-admin pair from controller authority and applies the target site's `cloudflare_dns_records`. DNS reconciliation produces records and evidence only; target sites receive no DNS credential material.

DNS and ACME/TLS issuer authority are controller-only surfaces. Site hosts may consume public certificates projected by the prod control plane through OpenBao/Nomad/host convergence, but they must not receive Cloudflare DNS credentials for DNS-01 issuance.

## TLS Certificate Model

HAProxy public certificates are issued by the prod Cloudflare control plane before host convergence. The account-admin token creates short-lived ACME DNS-01 TXT records in the managed Cloudflare zones, completes the ACME authorization, deletes the TXT records, and projects combined private-key plus certificate-chain PEM files under:

```text
.verself/site-bootstrap/<site>/tls/haproxy/<certificate-name>.pem
```

Host convergence copies those projected PEM files to `/etc/haproxy/certs` and reloads HAProxy. It must fail when a projected certificate is missing. A site bootstrap should provision the real public certificate; it should not manage a temporary public-edge certificate lifecycle.

The certificate set is derived from target site vars:

```text
verself_domain + *.verself_domain + *.api.verself_domain
company_domain
```

The hosted zone is derived from `cloudflare_product_zone` and `cloudflare_company_zone`, not by trimming labels from the domain. This supports subdomain sites such as `gamma.verself.sh` inside the `verself.sh` zone.

Certificate issuance state machine:

```text
load account-admin slot A from prod controller authority
  -> resolve every hosted zone referenced by target site vars
  -> inspect projected PEM for keypair validity, SAN coverage, and expiry
  -> reuse projected PEM when it is valid and outside the renewal window
  -> create ACME DNS-01 TXT records through Cloudflare
  -> wait for public DNS visibility
  -> complete ACME authorization and finalize the order
  -> write projected PEM atomically with 0600 mode
  -> delete ACME TXT records
  -> emit certificate names, domains, paths, expiry, and reuse evidence
```

The prod controller is the only holder of DNS issuer authority. Target sites receive certificate bytes, not Cloudflare DNS credentials. Renewal uses the same controller state transition and should run before host convergence or as a controller-owned operational task that refreshes the projection and then triggers host convergence.

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
  -> create new bucket-scoped R2 child token
  -> verify child token against the real provider API
  -> persist the new generation to OpenBao runtime names, except the Nomad artifact getter explicitly needed before Nomad can render secrets
  -> delete the newly created child token on any pre-persistence failure
```

Provider verification is part of the state transition. R2 child tokens must complete a PUT/HEAD/GET round trip. Verification may retry short-lived 401/403 responses because newly created Cloudflare tokens can require a short propagation interval; other provider errors are data and should fail the transition.

DNS is verified through hosted-zone visibility from the account-admin pair:

```text
load account-admin slot A from prod controller OpenBao
  -> list every hosted zone referenced by target site vars
  -> report zone ID fingerprints
  -> reconcile records from the controller
```

The DNS transition emits evidence and produces no child credential.

## Bootstrap Boundary

Bootstrap Cloudflare operations load account-admin authority from prod controller OpenBao. When prod controller OpenBao is not reachable, establish or recover controller OpenBao first; do not run Cloudflare DNS, TLS, or R2 provisioning from a local static secret file.

Generated bootstrap vars contain only the machine-provisioned Nomad artifact getter needed before Nomad can render OpenBao templates. Product-provider secrets such as Stripe, Resend, GitHub App private material, object-storage provider keys, and runtime deployment publisher keys enter OpenBao through their service-owned lifecycle after OpenBao and Nomad are available.

The bootstrap artifact publisher is a dynamic provider credential. The current control-plane binary mints it from prod controller OpenBao authority, writes it only to the child process environment for the local R2 helper, and revokes the Cloudflare token before `bootstrap-deploy` exits. The OpenBao-native form of this boundary is a Cloudflare secrets-engine plugin mounted in prod controller OpenBao: roles such as `bootstrap-artifact-publisher/<site>` return leased S3-compatible child credentials and revoke the provider token when the OpenBao lease is revoked or expires.

## Module Boundaries

- `control-plane/`: prod-owned Cloudflare authority, account-admin pair verification/rotation, hosted-zone authority verification, DNS reconciliation, R2 bucket creation, and R2 child credential provisioning.
- `r2-control-plane/`: site-local runtime upload-session service. It consumes a scoped publisher S3 credential and locally signs temporary R2 upload credentials. It does not hold account-admin authority or Cloudflare API token values.
- `email-routing/`: zone email-routing automation. It must use scoped zone credentials and must not depend on account-admin material.

## Code Pointers

- `control-plane/cmd/cloudflare-control-plane/main.go` provisions R2 child tokens, verifies each child token against R2, writes runtime values to `kv-runtime/data/secret/org/<secret-name>`, writes only the Nomad artifact getter to generated bootstrap vars, and mints/revokes the one-run bootstrap artifact publisher.
- `r2-control-plane/nomad.hcl` reads `cloudflare-r2-control-plane.publisher_token_id` and `cloudflare-r2-control-plane.publisher_secret_access_key` through Nomad's OpenBao template integration.
- `../../services/object-storage-service/nomad.hcl` reads the object-storage R2 admin/proxy credentials through Nomad OpenBao templates.
- Generated bootstrap vars are local operator artifacts. Runtime provider credentials must not appear there.

Generated local files under `.verself/site-bootstrap/<site>/` are operator bootstrap artifacts. Do not commit them.
