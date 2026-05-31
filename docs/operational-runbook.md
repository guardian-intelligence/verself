
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

## Cloudflare R2 Deployment Credentials

R2 backup buckets and deployment artifact buckets use separate credentials.
Backup buckets hold recovery data and may use retention or bucket-lock policy.
Deployment artifact buckets hold rebuildable, content-addressed Nomad artifacts.
Do not reuse backup credentials for deploy artifact publishing or runtime reads.

Provision R2 credentials per site:

1. Create or select the Cloudflare account that owns the site. Enable R2 and
   billing if the account has not used R2 before. Record the Cloudflare account
   ID as non-secret site metadata.
2. Create or confirm the site deployment artifact bucket:
   `nomad-artifacts-<site>`, for example `nomad-artifacts-gamma`. Keep backup
   buckets such as `verself-dev-backups` separate.
3. Create the R2 provisioner/admin credential only when infrastructure must
   create buckets, bucket locks, lifecycle policy, or subordinate credentials.
   Scope it to the Cloudflare account with the minimum account-level R2 admin
   permission needed for those operations. Store it in the controller secret
   plane or an operator session. Do not install it on hosts and do not load it
   into routine deploy shells.
4. If Cloudflare API tokens will be created by API, first create the initial
   Cloudflare "Create additional tokens" token from the dashboard. Store it as a
   bootstrap/provisioning secret, restrict it by source IP or TTL where possible,
   and do not grant it unrelated data-plane permissions.
5. Create the deploy publisher R2 S3 credential. Use an R2 API token scoped to
   the specific deployment artifact bucket with `Object Read & Write`. Store the
   returned Access Key ID and Secret Access Key in the controller secret plane.
   For the R2 deploy path, expose them to the deploy controller as:

   ```shell
   export VERSELF_CLOUDFLARE_ACCOUNT_ID=<32-hex-account-id>
   export VERSELF_NOMAD_ARTIFACTS_R2_ACCESS_KEY_ID=<publisher-access-key-id>
   export VERSELF_NOMAD_ARTIFACTS_R2_SECRET_ACCESS_KEY=<publisher-secret-access-key>
   ```

6. Create the runtime getter R2 S3 credential only if Nomad needs static S3
   credentials to download artifacts. Scope it to the same deployment artifact
   bucket with `Object Read only`. Put the values in the ignored site seed
   material as `nomad_artifact_getter_s3_access_key_id` and
   `nomad_artifact_getter_s3_secret_access_key`; host convergence writes the
   `/etc/nomad/nomad-artifacts.env` file consumed by Nomad.
7. Prefer temporary or presigned read access for runtime artifact downloads when
   the deploy path supports it. Cloudflare R2 temporary credentials can be
   scoped to prefixes or exact objects and have bounded TTLs; use them to avoid
   long-lived read credentials on hosts.
8. Validate the site seed and host install without printing secret values:

   ```shell
   aspect site validate-seed --site=<site>
   aspect site materialize-seed --site=<site>
   aspect site converge-host --site=<site>
   aspect deploy --site=<site> --sha="$(git rev-parse HEAD)"
   ```

9. Rotate by creating a replacement credential, updating the controller or site
   seed material, validating a deploy, and revoking the old credential. Rotate
   the publisher and getter credentials independently.

Reference:

- Cloudflare R2 API tokens:
  https://developers.cloudflare.com/r2/api/tokens/
- Cloudflare R2 temporary credentials:
  https://developers.cloudflare.com/api/resources/r2/subresources/temporary_credentials/
- Cloudflare API token creation by API:
  https://developers.cloudflare.com/fundamentals/api/how-to/create-via-api/


## Gamma Wipe + Bootstrap

If the Latitude box is already freshly reinstalled to Ubuntu and you
have the new root password, start at step 2. If it still has old gamma
state, first wipe it by reinstalling the OS from Latitude, then use
the new IP/root password from that reinstall.

cd /home/ubuntu/Projects/verself-sh
git pull --ff-only

aspect site seed-template --site=gamma --force

Fill .verself/site-bootstrap/gamma/seed.yml with only the requested
provider values. No SOPS file is created.

install -m 700 -d .verself/site-bootstrap/gamma
printf '%s\n' '<gamma-root-password>' > .verself/site-bootstrap/gamma/
root-password.txt
chmod 600 .verself/site-bootstrap/gamma/root-password.txt

Prefer pinned host key verification:

aspect site root-handoff \
--site=gamma \
--host=<gamma-public-ip> \
--root-password-file=.verself/site-bootstrap/gamma/root-password.txt
\
--host-key-sha256='<SHA256:fingerprint>' \
--force-inventory

If you do not have the host key fingerprint yet, use TOFU explicitly:

aspect site root-handoff \
--site=gamma \
--host=<gamma-public-ip> \
--root-password-file=.verself/site-bootstrap/gamma/root-password.txt
\
--trust-first-use \
--force-inventory

Then materialize, converge, publish DNS, and deploy:

aspect site validate-seed --site=gamma
aspect site materialize-seed --site=gamma
aspect site converge-host --site=gamma
aspect integrations cloudflare-dns --site=gamma --dry-run
aspect integrations cloudflare-dns --site=gamma
aspect deploy --site=gamma --sha="$(git rev-parse HEAD)" --post-
deploy-checks=medium
