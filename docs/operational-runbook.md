
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
content-addressed artifacts to the private Garage origin, resolves each Nomad
job, submits the resulting payloads to Nomad, and emits ClickHouse evidence for
each job decision. Changed jobs are not reported healthy until Nomad rollout
health and the selected component-owned post-deploy canaries pass.

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
