
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
aspect deploy --site=prod --sha=HEAD
```

`aspect deploy` resolves the site endpoint, authenticates, submits the request,
and returns a deployment ID. The deployment-service owns build orchestration,
artifact publication, Nomad submission, state, errors as data, and ClickHouse
evidence. Nomad jobs own runtime rollout behavior.

## Gamma Wipe + Bootstrap

If the Latitude box is already freshly reinstalled to Ubuntu and you
have the fresh-host SSH root password, start at step 2. If it still has old
gamma state, first wipe it by reinstalling the OS from Latitude, then use
the new IP and fresh-host SSH root password from that reinstall.

```shell
cd /home/ubuntu/Projects/verself-sh
git pull --ff-only

aspect site seed-template --site=gamma --force
```

Fill .verself/site-bootstrap/gamma/seed.yml with the requested bootstrap values.
No SOPS file is created. Cloudflare R2 child credentials for deployment
publication, Nomad artifact retrieval, recovery, and object-storage-service are
provisioned by the R2 control plane and merged into the local seed/vars outputs
by the rotation commands below. Materialization fails if those
machine-provisioned values are missing. Product provider credentials such as
Stripe and GitHub App private material may be absent during substrate bootstrap;
if absent, the owning service or gate fails when it consumes that runtime
secret. Resend sending credentials are created by `email-service` after site
OpenBao is available.

The site root key is site secret-zero for OpenBao initialization, seal, and
unseal. It is separate from the fresh-host SSH root password. Runtime DEKs and
generated site-local credentials are derived or created after site OpenBao is
available. Cloudflare account tokens, R2 child token creation, DNS authority,
Stripe, Resend full-access authority, and GitHub App private material are
external provider authorities; the site root key protects imported copies after
they enter OpenBao. Provider-side authority originates from the provider control
plane and enters OpenBao through an approved import or rotation path.

The Cloudflare account ID is checked into site metadata. Cloudflare R2 is
modeled as account-level capability buckets, with site isolation handled by
object prefixes and OpenBao policy. Cloudflare account API tokens are not
site-isolated; they are stored only in controller OpenBao and are exposed only
to the rotation/provisioning control plane. Controller-only bootstrap
exceptions are not site seed values.

Provision two equivalent Cloudflare token-admin credentials before bootstrap:

- `cloudflare_token_admin_a`
- `cloudflare_token_admin_b`

Each token has a seven-day expiration and the minimal Cloudflare account
permissions needed to verify, update, roll, create, and delete account-owned R2
child tokens. The pair is required because Cloudflare does not allow a
sub-token to create another token-management sub-token; steady-state automation
extends one operator-created token's expiration and rolls its value using the
other token as the recovery path if one update/roll/write step fails. The
Cloudflare API token value returned by the provider is available only once and
must be written directly to controller OpenBao.

The R2 buckets are capability-owned, not environment-owned:

| Capability | Bucket | Durable credential | Access |
| --- | --- | --- | --- |
| Deployment artifact publication | `verself-deployment-artifacts` | deployment publisher | read/write for deployment artifact prefixes |
| Nomad artifact retrieval | `verself-deployment-artifacts` | deployment getter | read-only for deployment artifact prefixes |
| Recovery and backup bytes | `verself-recovery` | recovery writer/reader | read/write for recovery prefixes |

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

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=import-token-admin \
  --token-admin-slot=a \
  --token-admin-api-token-file=<cloudflare-token-admin-a-file> \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=import-token-admin \
  --token-admin-slot=b \
  --token-admin-api-token-file=<cloudflare-token-admin-b-file> \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-token-admin-pair \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=provision-site-bootstrap \
  --token-admin-openbao-path=kv-controller/data/integrations/cloudflare/token-admin/a \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>
```

Steady-state rotation uses the same token-admin path for the individual
capabilities:

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-publisher \
  --token-admin-openbao-path=kv-controller/data/integrations/cloudflare/token-admin/a \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-getter \
  --token-admin-openbao-path=kv-controller/data/integrations/cloudflare/token-admin/a \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-object-storage-provider \
  --token-admin-openbao-path=kv-controller/data/integrations/cloudflare/token-admin/a \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-recovery \
  --token-admin-openbao-path=kv-controller/data/integrations/cloudflare/token-admin/a \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>
```

The Cloudflare token-admin pair stays in controller OpenBao and is used only by
the rotation/provisioning control plane. Steady-state artifact publication uses
the site-local `cloudflare-r2-control-plane` Nomad job with a scoped publisher
credential. That job receives the publisher token ID and API token from
OpenBao, then mints temporary R2 upload credentials per deployment. The durable
Nomad getter credential remains read-only so allocation restarts can refetch
artifacts.

Generated deployment publisher, Nomad getter, and object-storage child
credentials default to the site seed path. Recovery credentials default to
controller OpenBao.

When controller OpenBao is not reachable during first bootstrap, the same
token-admin authority may be passed as an ignored token file to provision the
site seed before host convergence. If a controller-only value was accidentally
placed in the ignored seed bundle, extract it without logging the value:

```shell
aspect site controller-secret-handoff \
  --site=gamma \
  --key=cloudflare_api_token \
  --out=.verself/site-bootstrap/gamma/cloudflare-token-admin.txt \
  --force
```

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=provision-site-bootstrap \
  --token-admin-api-token-file=.verself/site-bootstrap/gamma/cloudflare-token-admin.txt
```

Daily Cloudflare rotation is controller-owned:

```text
verify token_admin_a and token_admin_b
  -> use token_admin_a to update and roll token_admin_b
  -> write and verify token_admin_b
  -> use token_admin_b to update and roll token_admin_a
  -> write and verify token_admin_a
  -> create new child R2 token generation for each capability
  -> write child generation to OpenBao
  -> verify real R2 access for every child generation
  -> delete superseded child generations after overlap
  -> emit ClickHouse evidence
  -> email the operator on any failure
```

Every failed transition is terminal for that run. The control plane must keep
the last known valid token generation in OpenBao, write a structured failure
event, and notify the operator instead of retrying silently or deleting old
credentials.

```shell
install -m 700 -d .verself/site-bootstrap/gamma
printf '%s\n' '<gamma-root-password>' > .verself/site-bootstrap/gamma/root-password.txt
chmod 600 .verself/site-bootstrap/gamma/root-password.txt
printf '%s\n' '<gamma-site-root-key>' > .verself/site-bootstrap/gamma/site-root.key
chmod 600 .verself/site-bootstrap/gamma/site-root.key
```

Prefer pinned host key verification:

```shell
aspect site root-handoff \
  --site=gamma \
  --host=<gamma-public-ip> \
  --root-password-file=.verself/site-bootstrap/gamma/root-password.txt \
  --host-key-sha256='<SHA256:fingerprint>' \
  --force-inventory
```

If you do not have the host key fingerprint yet, use TOFU explicitly:

```shell
aspect site root-handoff \
  --site=gamma \
  --host=<gamma-public-ip> \
  --root-password-file=.verself/site-bootstrap/gamma/root-password.txt \
  --trust-first-use \
  --force-inventory
```

Then materialize, converge, publish DNS, and deploy:

OpenBao initialization uses the root token returned by `bao operator init` only
inside the first bootstrap transaction. The token is not stored in `/etc`,
`.verself`, git, logs, or generated artifacts. Breakglass plaintext access is
limited to an operator-approved incident or gameday through controller OpenBao
with an auditable reason and no persistence in repo files, generated artifacts,
shell history, or logs.

```shell
aspect site validate-seed --site=gamma
aspect site materialize-seed --site=gamma
aspect site converge-host \
  --site=gamma \
  --openbao-site-root-key-file=.verself/site-bootstrap/gamma/site-root.key
aspect integrations cloudflare-dns --site=gamma --dry-run
aspect integrations cloudflare-dns --site=gamma
aspect site bootstrap-deploy --site=gamma --sha="$(git rev-parse HEAD)"
aspect deploy --site=gamma --sha="$(git rev-parse HEAD)"
```

## Bootstrap State Machine

```text
external provider authority available through controller OpenBao or approved env-file
  -> site root key is copied to the host as OpenBao bootstrap authority
  -> site seed bundle materialized
  -> host base converged
  -> OpenBao initialized and unsealed; unseal material is wrapped by the site root key
  -> Nomad starts with site-local OpenBao integration
  -> quarantined bootstrap-deploy publishes initial R2 artifacts and tunnels to Nomad
  -> substrate-control-plane applies runtime secrets and workload roles
  -> Nomad deploys deployment-service and site-local control-plane jobs
  -> Pomerium operator access handoff is verified
  -> normal aspect deploy submits requests to deployment-service
```

Deployment-service-managed deploys require S0-S7 to pass in under one second:
S0 site metadata, S1 Nomad allocation evidence, S2 recovery SSH bootstrap
handoff declaration, S3 `bazelisk` and `git`, S4 OpenBao runtime secret
delivery, S5 substrate-control-plane import marker, S6 Nomad, and S7 Postgres,
deployment repo, and site-local R2 control-plane. `aspect deploy` reports
`deployment_service_unavailable` with the failed stage and does not SSH or run
Ansible.

`aspect site bootstrap-deploy` is the only deployment-shaped bootstrap escape
hatch. It runs from the controller, opens a temporary recovery SSH tunnel to the
site Nomad API, runs a local one-shot Cloudflare R2 control-plane backed by an
external provider-authority credential, publishes the initial immutable
artifacts to R2, registers the Nomad jobs, and exits. The preferred credential
source is controller OpenBao path
`kv-controller/data/integrations/cloudflare/r2/capabilities/deployment-publisher`;
the quarantined recovery alternatives are `--r2-credential-source=env-file`
for a scoped publisher credential or `--r2-credential-source=token-admin`
with `--r2-token-admin-api-token-file=<token-admin-file>` for a one-shot
publisher minted and deleted by the local control plane. It is not used after
the site-local deployment-service is reachable.

Only two local host state classes remain after handoff:

- `/etc/verself/bootstrap/openbao-root.key`: root-only site root key material
  needed to unwrap OpenBao recovery material during bootstrap and unseal.
- `/var/lib/verself/bootstrap/openbao`: OpenBao recovery material wrapped by
  the site root key. The `bao operator init` root token exists only in memory
  during this state transition and is revoked before the bootstrap task exits.
- `/var/lib/verself/access/pomerium`: operator-access key material needed by
  Pomerium and sshd before an authenticated operator can reach the host through
  Pomerium.
