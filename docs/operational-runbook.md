
SSH access is tied to identity via Pomerium using Zitadel as its OIDC.

If you are doing work that involves pulling logs or interacting with infrastructure you may be presented a URL to log in to Pomerium. If that happens, please pause and present the URL to the user.

```shell
ssh ubuntu@prod@access.verself.sh
```

- access.verself.sh: the Pomerium SSH listener.
- prod: the Pomerium SSH route name.
- ubuntu: the upstream Linux account Pomerium is allowed to request from sshd.

Additional site routes use the same native-SSH shape once the access plane has
routes for those upstreams:

```shell
ssh ubuntu@gamma@access.verself.sh
ssh ubuntu@dev@access.verself.sh
```

During first bootstrap before IAM, Zitadel, Pomerium, and WireGuard are healthy,
use direct host SSH only as the temporary bootstrap path. After the operator
access handoff, public SSH is Pomerium-only and fallback access is WireGuard:

```shell
ssh -p 2222 ubuntu@10.66.66.1
```

Run `aspect observe` to discover available telemetry, run `aspect db ch query`/`aspect db pg query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the host configuration, Ansible playbooks table.

Before testing the authenticated console against the production website, read the agent-browser login runbook in `src/viteplus-monorepo/apps/verself-web/AGENTS.md`.

Deployment requests go through the site-local deployment-service for the requested SHA:

```shell
aspect deploy
aspect deploy --site=gamma
aspect deploy --site=prod --sha=HEAD
```

`aspect deploy` resolves the site endpoint, authenticates, submits the request,
and returns a deployment ID. The deployment-service owns build orchestration,
artifact publication, Nomad submission, state, errors as data, and ClickHouse
evidence. Nomad jobs own runtime rollout behavior.

**Don't use personal access tokens**. We don't use static permanent long-standing credentials.

## Gamma Wipe + Bootstrap

If Gamma still has old state, reinstall it to Ubuntu from Latitude. Select the
generic `verself-bootstrap` user data. That cloud-init only installs the
operator SSH key and Python for Ansible; all site-specific state is applied by
inventory and `aspect site converge-host`.

If the reinstall does not use the generic user data, use the fresh-host SSH
root password for `aspect site root-handoff` before host convergence.

```shell
cd /home/ubuntu/Projects/verself-sh
git pull --ff-only
```

`aspect site converge-host` configures the OS, installs host tools, starts
Nomad, and skips the public edge. `aspect bootstrap-deploy` receives the
operator-provided OpenBao site root token file and one per-site bootstrap
credential YAML file. It builds locally, copies the bootstrap artifacts to the
target host over SSH, registers OpenBao and PostgreSQL through a temporary SSH
tunnel, stages the root token under `/run`, initializes or unseals OpenBao,
imports external runtime secrets, registers the remaining Nomad jobs, and
exits.

The bootstrap credential YAML is a single-use local handoff. It must be a
regular non-symlink file with mode `0600` or stricter and is passed with
`--bootstrap-credentials-file`. It contains:

- `site`: the target site name; it must match `--site`.
- `cloudflare`: account-admin token, account ID, object-storage bucket, and
  child-token TTL. Bootstrap uses it to create bucket-scoped R2 child
  credentials, verifies each child credential with real R2 PUT/HEAD/GET, and
  imports only those child credentials into site OpenBao.
- `openbao_runtime_secrets`: provider-originated values keyed by the OpenBao
  runtime secret logical name. GitHub App material, GitHub Sign-in OAuth
  client secret, Stripe keys, and Resend full-access authority enter the site
  through this map.

Resend is a prod/global provider authority. The
`email-service.resend.full_access_api_key` value is imported into site OpenBao
so the `email-service-resend-keys` Nomad batch job can create a site-local
`sending_access` key, write `email-service.resend.api_key`, and write
`zitadel.smtp.password`. Runtime services do not receive the full-access key.

The site root token is the operator-held bootstrap input for OpenBao
initialization and unseal. It is separate from the fresh-host SSH root password,
and the staged host copy is deleted after OpenBao is seeded. Runtime DEKs and
generated site-local credentials are derived or created after site OpenBao is
available. Cloudflare account tokens, R2 child token creation, DNS authority,
Stripe, Resend full-access authority, and GitHub App private material are
external provider authorities; after import, OpenBao protects the site-local
copies. Provider-side authority originates from the provider control plane and
enters OpenBao through an approved import or rotation path.

The Cloudflare account is a single global provider account anchored to prod
authority. `site=gamma` selects target records, R2 prefixes, runtime capability
credentials, and evidence labels; it does not select a Gamma-local Cloudflare
account. The Cloudflare account ID and account-owned R2 buckets are declared
only in `src/integrations/cloudflare/account.json`.

`cloudflare-integration-service` owns Cloudflare provider lifecycle for every
site. It is the only service that calls Cloudflare. `object-storage-service` is
the caller for Cloudflare-backed object-byte workflows. Edge/TLS, site
provisioning, and email provisioning call Cloudflare integration for DNS, ACME
DNS-01, and Email Routing capabilities. `secrets-service` stores Cloudflare
secret material, enforces SPIFFE/JIT access, and records OpenBao audit evidence.
OpenBao supplies KV and Transit primitives; provider policy and rotation state
machines remain in `cloudflare-integration-service`.

Target hosts do not receive Cloudflare account authority during host
convergence.

Bootstrap credential inventory is declared in `src/bootstrap/credentials.yml`.
The file contains names, provider surfaces, target OpenBao logical names, and
retrieval pointers only. It must not contain live, encrypted, redacted, or
example secret values.

Provision one Cloudflare account admin API token before bootstrap and place it
only in the local bootstrap credential YAML. The token is not durable site
state. Bootstrap uses it to derive and verify the object-storage R2 runtime
credentials that are stored in OpenBao.

The token must have Account API Tokens Read/Write, Workers R2 Storage
Read/Write, Workers R2 Storage Bucket Item Read/Write, Zone Read, DNS Write,
and Email Routing permissions for managed zones. The provider returns token
values only once; write the value directly into a local operator-only handoff
file or read it from the approved password manager. Do not stage Cloudflare
account authority in Nomad jobs, Ansible vars, generated artifacts, or
committed encrypted blobs.

The R2 buckets are capability-owned global account resources:

| Capability | Bucket | Runtime capability |
| --- | --- | --- |
| Deployment artifacts | `verself-deployment-artifacts` | object-storage deployment-artifact write sessions and read handles |
| Product object storage | service-declared bucket set | object-storage S3-compatible transfer handles and product credentials |
| Recovery and backup bytes | `verself-recovery` | object-storage recovery writer and reader handles |

Object prefixes include the site name:

```text
verself-deployment-artifacts/<site>/sha256/<artifact-sha256>/...
verself-deployment-artifacts/<site>/candidate/<deploy-run-key>/...
verself-recovery/<site>/...
```

The deployment bucket should retain only currently referenced artifact digests,
the in-flight candidate deployment, and a short drain window for previous
allocations. Recovery retention follows the recovery policy for the protected
data class.

After the site is alive, `cloudflare-integration-service` owns steady-state
provider reconciliation and performs these transitions:

```text
VerifyCloudflareAuthority
  -> RotateCloudflareAuthority
  -> ReconcileCloudflareDNS
  -> ReconcileR2Capability for deployment_artifacts, object_storage_provider, and recovery
  -> RotateR2CapabilityCredential for each runtime capability
  -> ReconcileEmailRouting for managed forwarding zones
```

If prod authority storage is not reachable during first-site recovery, use the
local bootstrap credential YAML only for the first handoff. TLS issuance is an
explicit public-edge step outside host bootstrap.

Steady-state artifact publication uses
`object-storage-service.CreateObjectWriteSession` and
`CompleteObjectWriteSession` with capability `deployment_artifacts`.
Deployment-service uploads artifact bytes to S3-compatible write handles and
receives read handles for Nomad. Nomad receives object-scoped read sources.

Bootstrap artifact delivery is SSH file copy to the target host. The target
serves those files over a loopback-only artifact server for the first Nomad
deployment. Steady-state deployments use per-object download sources returned
by `object-storage-service` after upload verification.

Runtime deployment, object-storage, and recovery credentials are
`secrets-service` entries backed by OpenBao.

Daily Cloudflare reconciliation is service-owned:

```text
verify Cloudflare account authority
  -> reconcile DNS state from desired records
  -> create new R2 capability credential generation when due
  -> persist capability generations through secrets-service
  -> verify real R2 access for every capability generation
  -> delete superseded R2 child generations after overlap
  -> emit ClickHouse evidence
  -> email the operator on any failure
```

Every failed transition is terminal for that run. The control plane must keep
the last known valid token generation in OpenBao, write a structured failure
event, and notify the operator instead of retrying silently or deleting old
credentials.

The OpenBao site root token is a single operator-provided file path passed to
`aspect bootstrap-deploy`. Do not place it under `.verself`, git, generated
artifacts, or site inventory.

GitHub Sign-in is a manual provider credential. In GitHub, create or open the
OAuth App used for browser login, set the homepage URL to the site product
domain, and set the authorization callback URL to the Zitadel GitHub IdP
callback for that site:

```text
https://<site-product-domain>/idps/callback
```

For gamma this is `https://gamma.verself.sh/idps/callback`. Store the client
secret in the approved password manager and import it into site OpenBao as:

```text
auth-control-plane.github_login.oauth_client_secret
```

The client ID is site metadata rendered into `auth-control-plane`; the client
secret is not procedurally provisioned by bootstrap and must never be committed
as an env file, SOPS file, Ansible var, or generated artifact.

If the reinstall used `verself-bootstrap`, write inventory for the new host and
verify direct SSH as `ubuntu` before convergence:

```shell
aspect site inventory-write \
  --site=gamma \
  --host=<gamma-public-ip> \
  --force

ssh ubuntu@<gamma-public-ip> true
```

If the reinstall did not use `verself-bootstrap`, prefer pinned host key
verification for root handoff:

```shell
aspect site root-handoff \
  --site=gamma \
  --host=<gamma-public-ip> \
  --root-password-file=<path-to-gamma-root-password-file> \
  --host-key-sha256='<SHA256:fingerprint>' \
  --force-inventory
```

If you do not have the host key fingerprint yet, use TOFU explicitly:

```shell
aspect site root-handoff \
  --site=gamma \
  --host=<gamma-public-ip> \
  --root-password-file=<path-to-gamma-root-password-file> \
  --trust-first-use \
  --force-inventory
```

Then materialize provider state, publish DNS, converge the host, and deploy:

OpenBao initialization uses the root token returned by `bao operator init` only
inside the first bootstrap transaction. The token is not stored in `/etc`,
`.verself`, git, logs, or generated artifacts. Breakglass plaintext access is
limited to an operator-approved incident or gameday through controller OpenBao
with an auditable reason and no persistence in repo files, generated artifacts,
shell history, or logs.

```text
cloudflare-integration-service.VerifyCloudflareAuthority(site=gamma)
cloudflare-integration-service.ReconcileCloudflareDNS(site=gamma, dry_run=true)
cloudflare-integration-service.ReconcileCloudflareDNS(site=gamma, dry_run=false)
cloudflare-integration-service.ReconcileR2Capability(site=gamma, capability=deployment_artifacts)
```

```shell
aspect site converge-host --site=gamma
aspect bootstrap-deploy \
  --site=gamma \
  --sha="$(git rev-parse HEAD)" \
  --openbao-site-root-token-file=<path-to-gamma-openbao-site-root-token> \
  --bootstrap-credentials-file=<path-to-gamma-bootstrap-credentials-yaml>
aspect deploy --site=gamma --sha="$(git rev-parse HEAD)"
```

## Bootstrap State Machine

```text
operator bootstrap credential YAML available as a local 0600 file
  -> host base converged
  -> Nomad starts with site-local OpenBao integration
  -> bootstrap-deploy stages the site root token under /run over recovery SSH
  -> bootstrap-deploy copies local OpenBao and PostgreSQL artifacts over SSH and tunnels to Nomad
  -> bootstrap-deploy registers OpenBao and PostgreSQL
  -> OpenBao initializes and unseals; unseal material is wrapped by the site root token
  -> the staged host copy of the site root token is deleted
  -> bootstrap-deploy imports external provider runtime secrets into OpenBao
  -> service-owned jobs or rotation commands create derived runtime secrets
  -> Nomad deploys deployment-service and site-local services
  -> public-edge certificates and HAProxy are converged after core services are healthy
  -> Pomerium operator access handoff is verified
  -> normal aspect deploy submits requests to deployment-service
```

Deployment-service-managed deploys require S0-S7 to pass in under one second:
S0 site metadata, S1 Nomad allocation evidence, S2 recovery SSH bootstrap
handoff declaration, S3 `bazelisk` and `git`, S4 OpenBao runtime secret
delivery, S6 Nomad, S7 Postgres, deployment repo, and artifact publishing
through `object-storage-service`. `aspect deploy` reports
`deployment_service_unavailable` with the failed stage and does not SSH or run
Ansible.

`aspect bootstrap-deploy` is the only deployment-shaped bootstrap escape
hatch. It runs from the controller, opens a temporary recovery SSH tunnel to the
site Nomad API, builds the deployment inputs locally, copies the artifact bytes
to `/var/lib/verself/bootstrap-artifacts/<site>/<sha>/` on the host, rewrites
the Nomad artifact sources to the host loopback artifact server, registers the
Nomad jobs, and exits.

Only durable local host state classes remain after handoff:

- `/var/lib/verself/bootstrap/openbao`: OpenBao recovery material wrapped by
  the operator-provided site root token. The token itself is not durable host
  state; the staged `/run/verself/bootstrap/openbao-site-root.token` copy is deleted
  after OpenBao is seeded. The `bao operator init` root token exists only in
  memory during this state transition and is revoked before the bootstrap task
  exits.
- `/var/lib/verself/access/pomerium`: operator-access key material needed by
  Pomerium and sshd before an authenticated operator can reach the host through
  Pomerium.
