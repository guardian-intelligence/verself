# Verself

Verself is two things:

1. Prima facie, a PaaS selling stateful, suspendable compute with near-serverless economics via fast-launching Firecracker VMs with hot-swappable filesystems via `zfs clone`.

2. [PLANNED ONLY] See docs/product/future-state.md

The main product offering is a (hopefully) better Blacksmith.sh: a GitHub App where we run customer's GitHub actions on our bare metal. We ship a custom GitHub action that replaces the standard `actions/checkout`. The action does the following:

1. Runs the customer's workload on our bare metal, inside a firecracker VM that boots with a composed set of zvols mounted at user-configured directories + the repo's main branch checked out and ready before the CI job even starts. Once the CI Job starts, our custom `checkout` action applies the TIP of the `github.event.pull_request.head.sha` against the base branch.

2. When default branch's latest commit's CI goes green, we promote the repo file system post-CI (`GITHUB_WORKSPACE`) + durable VM directories outside the `GITHUB_WORKSPACE` such as installed binaries, Bazel caches, DB files, and so on. The next PR that executes CI will now have its VM mount its starting file-system from the golden ZFS volumes composed prior to the job running. 

2a. `getRepoZvolForPR` is approximately `(organization, project, repo, target-branch, workflow-id, job-id, matrix-key)` for GitHub. (Forgejo/Codeberg/GitLab support pending)

* Verself does not host customer applications as managed long-lived services (yet, but as you may imagine by the `verself` branding we are poised to do that soon via Open vSwitch or something similar). 

The software offerings are layered as follows:

a. Internal Core -- Infrastructure, bootstrap configuration, integration with 3p APIs, privileged processes

b. Services -- Smithy-modeled HTTP APIs implemented by product services. Public projections feed SDKs and facades; internal projections use SPIFFE mTLS for repo-owned cross-service calls. OpenAPI is generated for documentation and ecosystem tooling.

c. SDK -- Customer-facing multi-language resource API. The SDK shape drives public API design and wraps public transport implementations, using generated code only where OpenAPI tooling is reliable. See [`docs/architecture/sdk-api-surface.md`](docs/architecture/sdk-api-surface.md).

d. Clients -- websites, mobile apps, CLI. Call the SDK under the hood. See [`docs/verself-cli.md`](docs/verself-cli.md) for more on the CLI.

(Note on above, the structure is still WIP and we are at maybe 5% parity in terms of having even just a golang SDK over our services.)

The web app lives at `https://<domain>` (console, public docs, and policy in one TanStack Start app). Public service APIs use per-service origins such as `https://billing.api.<domain>`, `https://sandbox.api.<domain>`, and `https://iam.api.<domain>`. Protocol origins include `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`.

Per-task documentation lives in `aspect <task> --help`.

## GitHub App

Prod GitHub App name: Verself Runner https://github.com/organizations/guardian-intelligence/settings/apps/verself-runner

Homepage url: https://verself.sh
Callback URL: https://verself.sh/github/installations/callback (needs to be updated)
Webhook URL: https://sandbox.api.verself.sh/webhooks/github/actions

## Quickstart

Choose the controller platform that is running the repo commands.

### Linux x86_64 controller

```bash
# 1. Toolchain (one time per controller).
./src/tools/dev/bootstrap/bootstrap-linux-amd64
bazelisk mod tidy
```

### macOS Apple Silicon controller

```bash
# 1. Toolchain (one time per controller).
./src/tools/dev/bootstrap/bootstrap-darwin-arm64
bazelisk mod tidy
```

### (optional) Cloning this repo onto your own infrastructure

```bash
# 2. Tell OpenTofu where to provision (one time per environment).
cp src/tools/provisioning/terraform/terraform.tfvars.example.json \
   src/tools/provisioning/terraform/terraform.tfvars.json
$EDITOR src/tools/provisioning/terraform/terraform.tfvars.json   # set project_id

# 3. Provision bare metal + render inventory.
aspect dev sops-init
aspect provision apply

# 4. Deploy. Idempotent; safe to repeat.
aspect deploy

# 5. Mint a persona env file and start working.
aspect persona assume platform-admin
```


`src/tools/dev/bootstrap/bootstrap-linux-amd64` and
`src/tools/dev/bootstrap/bootstrap-darwin-arm64` are the only sanctioned shell
scripts in the repo. Everything else is done through `aspect` and `bazelisk`.
The two scripts just get any fresh developer/agent environment set up. They
install into `${HOME}/.cache/verself/bootstrap-bin` by default and
automatically add that directory to GitHub Actions via `GITHUB_PATH` when that
file is present. Set `BOOTSTRAP_INSTALL_DIR` to opt into a different install
directory.
