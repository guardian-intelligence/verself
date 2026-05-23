# Verself

Verself is two things:

1. Prima facie, a PaaS selling stateful, suspendable compute with near-serverless economics via fast-launching Firecracker VMs with hot-swappable filesystems via `zfs clone`.

2. [PLANNED ONLY] See docs/product/future-state.md

The main product offering is a Blacksmith.sh-style GitHub App that runs
customer GitHub Actions jobs on Verself bare metal. Customers switch runner
labels and use the Verself checkout action. The action preserves ordinary
GitHub Actions workflow semantics while reconciling the restored workspace to
the event commit.

1. A job runs inside a Firecracker VM with a static graph of ZFS zvols mounted
   before customer steps start: the workspace, any declared durable paths, and
   platform toolchain images.

2. When a protected target-branch workflow run is green, Verself promotes one
   golden artifact per compatible job shape. A golden artifact couples the
   post-build workspace and durable zvol generations with a Firecracker
   vmstate/memory snapshot of the warm guest. Future PR jobs restore that
   artifact when compatible, then checkout advances `GITHUB_WORKSPACE` to the
   PR head SHA.


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
Webhook URL: https://github.api.verself.sh/api/v1/github/webhooks

## Quickstart

Choose the controller platform that is running the repo commands.

### Linux x86_64 controller

```bash
# 1. Toolchain (one time per controller).
./src/tools/dev/bootstrap/bootstrap-linux-amd64
export PATH="${HOME}/.cache/verself/bootstrap-bin:${PATH}"
bazelisk mod tidy
aspect dev install --install-shims --bin-dir="${HOME}/.local/bin"
export PATH="${HOME}/.local/bin:${HOME}/.local/share/verself/ruby/4.0.5/bin:${PATH}"
```

### macOS Apple Silicon controller

```bash
# 1. Toolchain (one time per controller).
./src/tools/dev/bootstrap/bootstrap-darwin-arm64
export PATH="${HOME}/.cache/verself/bootstrap-bin:${PATH}"
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
directory. Local shells need that directory on `PATH` before invoking `aspect`
or `bazelisk`.



<mission_overview>
The immediate goal of Verself is to make CI 10x faster across the industry, and to redefine how CI is thought of. Today, CI execution environments start from a clean slate, throwing out precious build artifacts that in many cases take hours to generate, and the standard GitHub golden path is to start a VM without having the repo even cloned. GitHub wins because CI takes forever and they charge by the minute. Our philosophy is that CI should start from a golden image of the deployed software: all build, lint, compilation, and intermediate artifacts intact. This means teaching developers that their CI VM should begin from a snapshot that preserves the same thing a developer machine preserves: hot Bazel server state plus the filesystem cache layout it expects.

That means:
* Stop thinking of CI like a sterile one-shot build machine.
* Focus on making the developer/agent environments as fast as possible, and then CI becomes fast by default.
* Bazel remote caches, complex S3 uploads and downloads to claw back build artifacts, and CI-specific cache commands all become obsolete.

The challenge then becomes:

* Providing the right security defaults and developer ergonomics to make this approach to CI work seamlessly for any repo of any shape or size.
* Keeping the developer and CI execution models aligned so agents and humans optimize one environment instead of separate local and CI paths.
* Communicating to the world why this approach is better for security, reliability, and performance.

This repo is constructed to provide this solution as a suite of layered software products:

* HTTP Services on top of core technological scheduling/networking/compute/storage primitives stitched together from open-source technologies + bare metal that implement those services
* An SDK that wraps the public APIs of those services for convenient programmatic usage by customers in a variety of languages.
* Prebuilt clients that call our SDK (website/mobile app/cli)
* Integrations between other services the user already has entitlements to and our software offerings (GitHub App/VSCode Extensions) usually highly specific to a particular client.

The SDK allows folks to build their own clients and platforms on top of our raw software offerings and to script usage of Verself in whatever language an agent wants to use (currently only Go/TypeScript supported, Python support soon).

Note that the SDK layer today is only 1-2% implemented to the level of rigor it should be.

The objective behind all architectural decisions are to maximize adherence to the following ideas:

* Secure by default - Default deny, defense in depth. 
* Portable by default - we should minimize the lines of code necessary to ship sweeping changes across websites, embedded applications in third party widgets (e.g. VS Code extensions, once we have one), mobile apps (once we have them)
</mission_overview>