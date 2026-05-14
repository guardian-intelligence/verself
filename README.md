# Verself

Verself is two things:

1. Prima facie, a PaaS selling stateful, suspendable compute with near-serverless economics via fast-launching Firecracker VMs with hot-swappable filesystems via `zfs clone`.

2. The "golden image" of a self-contained self-replicating software company that can clone itself via an API call to `source-code-hosting-service`, which clones the repo with the user's configured company name, founder details, domain name, Resend API key, Stripe API key, etc. and uses all that to configure the repository for the caller, culminating in a download link. The user can then download their white-labelled clone configured for their providers and execute a shell script to bootstrap a replica of Verself for themselves. IOW: technology that converts any bare metal into structured, useful general purpose compute with economically valuable systems already set up for the user like auth, email, billing/payments, CI, observability, and a fully-functioning end-to-end revenue-generating product.

The main product offering is a (hopefully) better Blacksmith.sh: a GitHub App where we run customer's GitHub actions on our bare metal. We ship a custom GitHub action that replaces the standard `actions/checkout`. The action:

1. Runs the customer's workload on our bare metal, inside a firecracker VM that boots with a composed set of zvols mounted at user-configured directories + the repo's main branch checked out and ready before the CI job even starts. Once the CI Job starts, our custom `checkout` action applies the TIP of the `github.event.pull_request.head.sha` against the base branch.

2. When default branch's latest commit's CI goes green, we promote the repo file system post-CI (`GITHUB_WORKSPACE`) + durable VM directories outside the `GITHUB_WORKSPACE` such as installed binaries, Bazel caches, DB files, and so on. The next PR that executes CI will now have its VM mount its starting file-system from the golden ZFS volumes composed prior to the job running. 

2a. `getRepoZvolForPR` is approximately `(organization, project, repo, target-branch, workflow-id, job-id, matrix-key)` for GitHub. (Forgejo/Codeberg/GitLab support pending)

* Verself does not host customer applications as managed long-lived services (yet, but as you may imagine by the `verself` branding we are poised to do that soon via Open vSwitch or something similar). 

The software offerings are layered as follows:

a. Internal Core -- Infrastructure, bootstrap configuration, integration with 3p APIs, privileged processes

b. Services -- Smithy-modeled HTTP APIs implemented by product services. Public projections feed SDKs and facades; internal projections use SPIFFE mTLS for repo-owned cross-service calls. OpenAPI is generated for documentation and ecosystem tooling.

c. SDK -- Customer-facing multi-language resource API. The SDK shape drives public API design and wraps SDK-owned transport cores, using generated code only where OpenAPI tooling is reliable. See [`docs/architecture/sdk-api-surface.md`](docs/architecture/sdk-api-surface.md).

d. Clients -- websites, mobile apps, CLI. Call the SDK under the hood. See [`docs/verself-cli.md`](docs/verself-cli.md) for more on the CLI.

(Note on above, the structure is still WIP and we are at maybe 5% parity in terms of having even just a golang SDK over our services.)

The web app lives at `https://<domain>` (console, public docs, and policy in one TanStack Start app). Public service APIs use per-service origins such as `https://billing.api.<domain>`, `https://sandbox.api.<domain>`, and `https://iam.api.<domain>`. Protocol origins include `git.<domain>`, `auth.<domain>`, `mail.<domain>`, and `dashboard.<domain>`. See [`docs/architecture/public-origins.md`](docs/architecture/public-origins.md).

Per-task documentation lives in `aspect <task> --help`.

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

`src/tools/dev/bootstrap/bootstrap-linux-amd64` and `src/tools/dev/bootstrap/bootstrap-darwin-arm64` are the only sanctioned shell scripts in the repo. Everything else is done through `aspect` and `bazelisk`. The two scripts just get any fresh developer/agent environment set up.
