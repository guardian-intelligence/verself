# forge-metal

Repo is for a turnkey "company in a box": fully self-hosted bare-metal platform with Forgejo, Fast CI via ZFS deep optimizations, ClickStack observability.

Performance-first CI on bare metal. ZFS golden image clones (~28ms), gVisor sandboxing, ClickHouse wide events, and HyperDX for real-time observability. Designed for 1000+ globally distributed nodes.

The goal is for turnkey bootstrap from 0 -> bare metal instance -> forgejo + click stack + 2 deployed frontend apps reading/writing off the same DB. 

Hard requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups (done through `zfs send`, not LINSTOR + DRBD)
Required - Domain Registar (Cloudflare only for now)
Required - Compute Provider (Latitude.sh only for now)
Required (not implemented) - Email Delivery (Resend only in the future)


## Quick Start

### 1. Clone and enter dev shell

```bash
nix develop  # gives you Go, Terraform, Ansible, protoc, clickhouse-client, etc.
```

### 2. Provision bare metal

```bash
# Set your Latitude.sh API token
export LATITUDESH_AUTH_TOKEN="your-token-here"

# Create your tfvars (one-time)
cp terraform/terraform.tfvars.example.json terraform/terraform.tfvars.json
# Edit terraform/terraform.tfvars.json — set project_id to your Latitude.sh project

# Provision server + generate Ansible inventory
make provision
```

This provisions a bare metal server via OpenTofu and auto-generates `ansible/inventory/hosts.ini` from the outputs.

### 3. Build the golden image and deploy

```bash
make e2e
```

This does:

1. **Builds the Nix server profile** -- every service (ClickHouse, MongoDB, Caddy, OTel Collector, Node.js, containerd, gVisor, Forgejo, etc.) packaged into a single content-addressed closure. All versions pinned by `flake.lock`.
2. **Installs Nix on the remote host** and pushes the closure over SSH.
3. **Configures services** via Ansible (thin roles: just config templates + systemd enablement).
4. **Health checks** -- asserts 6 services active, HTTPS works, admin seeded.
5. **Verifies** -- inserts test wide events, queries ClickHouse, validates compression codecs, sends OTLP trace.

If it exits 0, the stack is healthy.

For subsequent deploys without wiping state:

```bash
make deploy  # idempotent, no wipe
```

### 4. Log in

```bash
# Admin credentials (generated on first deploy)
ssh ubuntu@<ip> 'sudo cat /etc/clickstack/admin-credentials.txt'
```

Open `https://<ip>` in your browser (self-signed cert for IP addresses, auto Let's Encrypt for domains).

### TLS with a real domain (Cloudflare)

```bash
make setup-domain DOMAIN=anveio.com
make deploy
```

`setup-domain` walks you through everything: initializes secrets if needed, guides you through creating a Cloudflare API token, validates it, and writes the config. Then `deploy` creates DNS records and provisions TLS.

Services get subdomains automatically:

| Subdomain | Service |
|-----------|---------|
| `admin.<domain>` | ClickStack dashboard |
| `git.<domain>` | Forgejo (when enabled) |

To route traffic through Cloudflare's CDN/WAF instead of direct-to-origin, set `cloudflare_proxied: true` in `ansible/group_vars/all/main.yml`.

### Deprovision

```bash
make deprovision  # destroys server + SSH key via OpenTofu, removes inventory
```

This runs `tofu destroy` and cleans up the generated Ansible inventory. DNS records (if any) must be removed separately via the Cloudflare dashboard or API.

## How It Works

### Nix Golden Image

All server software is packaged in a single Nix closure (`flake.nix` -> `server-profile`). This means:

- **Reproducible**: `flake.lock` pins every dependency transitively. Same lock = same binaries, always.
- **Fast deploys**: The closure is pushed once over SSH. No apt repos, no GitHub downloads, no `yarn install`.
- **Atomic updates**: `nix flake update` + `make deploy` upgrades everything. Rollback = `git revert flake.lock`.
- **No apt, no get_url, no build-from-source at deploy time** (except HyperDX, which builds from source using Nix-provided Node.js).

The only `apt install` that remains is `zfsutils-linux` (kernel-dependent, must match running kernel).

### Version Updates

```bash
nix flake update           # update all packages
make deploy --limit canary # deploy to one node
make e2e --limit canary    # verify
make deploy                # roll out fleet-wide
```

### Architecture

```
nix build .#server-profile --> content-addressed closure (~2GB)
nix copy --to ssh://host   --> push closure to bare metal
Ansible                    --> configure + enable services
```

| Component | Port | Purpose |
|-----------|------|---------|
| Caddy | 443, 80 | Reverse proxy with automatic TLS |
| Forgejo | 3000 | Git server + CI runner |
| Verdaccio | 4873 | Sealed npm registry mirror |
| ClickHouse | 9000, 8123 | Wide event storage with optimized codecs |
| HyperDX | 8080, 8000 | Observability UI + API |
| OTel Collector | 4317, 4318 | OTLP telemetry ingestion |
| MongoDB | 27017 | HyperDX app state |

### Wide Events

Every CI job produces one denormalized row in `ci_events` with ~50 columns. No JOINs needed.

Compression codecs per column type:
- Timestamps: `DoubleDelta + ZSTD(3)`
- Durations (Int64): `Delta(8) + ZSTD(3)`
- Byte counters: `T64 + ZSTD(3)`
- Low-cardinality strings: `LowCardinality + ZSTD(3)`
- Floats: `Gorilla + ZSTD(3)`

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make provision` | Provision bare metal via OpenTofu, generate Ansible inventory |
| `make deprovision` | Destroy all bare metal infrastructure |
| `make setup-domain` | Configure Cloudflare domain (interactive wizard) |
| `make server-profile` | Build Nix server profile (golden image closure) |
| `make deploy` | Deploy to all nodes (idempotent, no wipe) |
| `make e2e` | Full wipe + reprovision + test |
| `make benchmark` | Benchmark wipe+reprovision (3 iterations) |
| `make build` | Build bmci Go binary locally |
| `make test` | Run Go tests |

## Project Structure

```
forge-metal/
├── cmd/bmci/              # CLI entry point (doctor, setup-domain)
├── internal/
│   ├── clickhouse/        # ClickHouse client, wide event struct
│   ├── cloudflare/        # Cloudflare API client (DNS, zone lookup)
│   ├── config/            # Layered TOML config
│   ├── doctor/            # Dev environment health checks
│   └── domain/            # Domain setup wizard
├── ansible/
│   ├── playbooks/         # ci-e2e, dev-single-node, site, golden-refresh, security-patch
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry)
│       ├── cloudflare_dns/ # Cloudflare DNS A record management
│       ├── clickstack/    # ClickHouse, HyperDX, OTel, Caddy, MongoDB (config only)
│       ├── zfs/           # Pool creation, golden/ci datasets
│       ├── containerd/    # containerd + gVisor runsc (config only)
│       ├── verdaccio/     # Sealed npm registry mirror (config only)
│       ├── wireguard/     # Mesh networking (config only)
│       ├── golden_image/  # ZFS snapshot with warm caches
│       └── forgejo/       # Git server + CI runner (config only)
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── scripts/               # Security scripts, benchmark runner
├── config/default.toml    # Embedded defaults
└── flake.nix              # Dev shell + server profile (golden image)
```

## Output Contract

* When proposing solutions, think from the perspective of the user of the system. The user is a sole operator of a single-person software company.

## License

MIT
