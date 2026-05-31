
SSH access is tied to identity via Pomerium using Zitadel as its OIDC.

If you are doing work that involves pulling logs or interacting with infrastructure you may be presented a URL to log in to Pomerium. If that happens, please pause and present the URL to the user.

```shell
ssh ubuntu@prod@access.verself.sh
```

- access.verself.sh: the Pomerium SSH listener.
- prod: the Pomerium SSH route name.
- ubuntu: the upstream Linux account Pomerium is allowed to request from sshd.

During first bootstrap before IAM, Zitadel, Pomerium, and WireGuard are healthy,
use direct host SSH only as the temporary bootstrap path. After the operator
access handoff, public SSH is Pomerium-only and fallback access is WireGuard:

```shell
ssh -p 2222 ubuntu@10.66.66.1
```

Run `aspect observe` to discover available telemetry, run `aspect db ch query`/`aspect db pg query` wrappers to easily query ClickHouse/PG with fewer shell string escaping issues, deploy playbooks and correlation model (`deploy_run_key`, `deploy_id`, `traceparent`), TLS via Cloudflare, the host configuration, Ansible playbooks table.

Before testing the authenticated console against the production website, read the agent-browser login runbook in `src/viteplus-monorepo/apps/verself-web/AGENTS.md`.

Nomad deploys are driven directly by the checked-in `nomad_component` targets for the requested SHA:

```shell
aspect deploy --site=prod --sha=HEAD
```

`aspect deploy` builds the Bazel-discovered descriptors, uploads missing
content-addressed artifacts to the site's private Cloudflare R2 artifact
bucket, resolves each Nomad job, submits the resulting payloads to Nomad, and
emits ClickHouse evidence for each job decision. Changed jobs are not reported
healthy until Nomad rollout health and the selected component-owned post-deploy
canaries pass.

Medium canaries run by default:

```shell
aspect deploy --site=prod --sha=HEAD --post-deploy-checks=medium
```

Use `--post-deploy-checks=large` or `--post-deploy-checks=all` when a release
requires deeper browser/CLI checks in the deploy rollback window. Use
`--post-deploy-checks=none` only for bootstrap or incident procedures where the
canary dependency is known to be unavailable. If a canary fails after a changed
Nomad job becomes healthy, `verself-deploy` reverts the job to the prior Nomad
version; first deploys with no prior version are deregistered.

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
provider values, including the scoped Nomad artifact getter R2 keypair. No SOPS
file is created. The Cloudflare account ID is checked into site metadata. Keep
the parent R2 admin credential in controller OpenBao or an operator-only
environment, and verify the deployment artifact bucket before deploy:

```shell
aspect integrations cloudflare-r2-control-plane \
  --site=gamma \
  --credential-source=openbao
```

Until the deploy publisher consumes the R2 control-plane temporary credential
handoff directly, any controller publisher credential used for `aspect deploy`
must be bucket-scoped to the site's artifact bucket. Do not use the parent R2
admin credential outside the R2 control plane.

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
`.verself`, git, logs, or generated artifacts. Disaster recovery may require a
temporary root token generated from recovery material during a dedicated
recovery exercise.

```shell
aspect site validate-seed --site=gamma
aspect site materialize-seed --site=gamma
aspect site converge-host --site=gamma
aspect integrations cloudflare-dns --site=gamma --dry-run
aspect integrations cloudflare-dns --site=gamma
aspect deploy --site=gamma --sha="$(git rev-parse HEAD)" --post-deploy-checks=medium
```
