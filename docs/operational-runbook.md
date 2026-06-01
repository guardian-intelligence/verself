
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

Declared canaries can also be run against the current site without submitting
jobs:

```shell
aspect canary post-deploy --site=prod --size=medium
aspect canary post-deploy --site=prod --size=large
```

## Gamma Wipe + Bootstrap

If the Latitude box is already freshly reinstalled to Ubuntu and you
have the new root password, start at step 2. If it still has old gamma
state, first wipe it by reinstalling the OS from Latitude, then use
the new IP/root password from that reinstall.

```shell
cd /home/ubuntu/Projects/verself-sh
git pull --ff-only

aspect site seed-template --site=gamma --force
```

Fill .verself/site-bootstrap/gamma/seed.yml with only the requested
provider values. No SOPS file is created. The Cloudflare account ID is checked
into site metadata. Import the parent R2 admin credential directly into
controller OpenBao, then provision the site-scoped R2 credentials into the
local seed bundle before materializing it:

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=import-parent \
  --parent-api-token-file=<operator-only-token-file> \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>
```

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-getter \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>

aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=rotate-object-storage-provider \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>
```

Run the R2 control-plane upload-session API from a controller context that can
read the parent Cloudflare R2 credential from controller OpenBao. The
deployment-service talks to this HTTP boundary and does not read Cloudflare or
OpenBao credentials. The durable Nomad getter credential remains
bucket-read-only so allocation restarts can refetch artifacts after deploy-time
upload sessions expire.

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --action=serve \
  --openbao-addr=<controller-openbao-addr> \
  --openbao-token-file=<controller-openbao-token-file>
```

Keep the control-plane process running while deployments publish artifacts.

```shell
install -m 700 -d .verself/site-bootstrap/gamma
printf '%s\n' '<gamma-root-password>' > .verself/site-bootstrap/gamma/root-password.txt
chmod 600 .verself/site-bootstrap/gamma/root-password.txt
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
aspect site converge-host --site=gamma
aspect integrations cloudflare-dns --site=gamma --dry-run
aspect integrations cloudflare-dns --site=gamma
aspect deploy --site=gamma --sha="$(git rev-parse HEAD)"
```

## Bootstrap State Machine

```text
controller OpenBao unlocked
  -> controller state root available at .verself/controller
  -> site seed bundle materialized
  -> host base converged
  -> OpenBao initialized and unsealed
  -> Nomad starts with site-local OpenBao integration
  -> substrate-control-plane applies runtime secrets and workload roles
  -> Nomad deploys platform and product jobs
  -> Pomerium operator access handoff is verified
```

Only two local host state classes remain after handoff:

- `/var/lib/verself/bootstrap/openbao`: OpenBao Shamir unseal material for the
  site-local OpenBao instance. The `bao operator init` root token exists only
  in memory during this state transition and is revoked before the bootstrap
  task exits.
- `/var/lib/verself/access/pomerium`: operator-access key material needed by
  Pomerium and sshd before an authenticated operator can reach the host through
  Pomerium.
