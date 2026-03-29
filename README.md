# forge-metal

Self-hosted bare-metal CI platform with ClickStack observability.

Performance-first CI on bare metal. ZFS golden image clones (~28ms), gVisor sandboxing, ClickHouse wide events, and HyperDX for real-time observability. Designed for 1000+ globally distributed nodes.

The goal is for turnkey bootstrap from 0 -> latitude.sh bare metal instance -> forgejo + click stack + 2 deployed frontend apps reading/writing off the same DB. 

Hard requirement: everything must be self-hosted.

Exceptions:

Optional - Backblaze B2, Cloudflare R2, AWS S3 for backups


## Quick Start

### Prerequisites

- A bare-metal server running Ubuntu 24.04 (e.g. [Latitude.sh](https://latitude.sh))
- SSH access (`ssh ubuntu@<ip>` works)
- [Nix](https://nixos.org/download/) on your workstation (provides all tooling)

### 1. Clone and enter dev shell

```bash
git clone https://github.com/forge-metal/forge-metal.git
cd forge-metal
nix develop  # gives you Go, Terraform, Ansible, protoc, clickhouse-client, etc.
```

### 2. Configure inventory

```bash
cd ansible
cat > inventory/hosts.ini << 'EOF'
[workers]
my-node ansible_host=<YOUR_IP>

[infra]
my-node ansible_host=<YOUR_IP>

[all:vars]
ansible_user=ubuntu
ansible_python_interpreter=/usr/bin/python3
EOF
```

### 3. Build the golden image and deploy

```bash
# Build the Nix server profile (first time downloads/builds everything, cached after)
cd ..
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
| `make setup-domain` | Configure Cloudflare domain (interactive wizard) |
| `make server-profile` | Build Nix server profile (golden image closure) |
| `make deploy` | Deploy to all nodes (idempotent, no wipe) |
| `make e2e` | Full wipe + reprovision + test |
| `make benchmark` | Benchmark wipe+reprovision (3 iterations) |
| `make build` | Build bmci Go binary locally |
| `make test` | Run Go tests |
| `make proto` | Generate gRPC code from proto |

## Project Structure

```
forge-metal/
├── cmd/bmci/              # CLI entry point (controller, agent)
├── internal/
│   ├── agent/             # Worker agent (heartbeat, job execution)
│   ├── controller/        # Job scheduler, node registry
│   ├── config/            # Layered TOML config
│   ├── clickhouse/        # ClickHouse client, wide event struct
│   ├── sandbox/           # gVisor/containerd integration
│   ├── network/           # WireGuard config generation
│   └── proto/v1/          # gRPC protobuf (AgentService)
├── ansible/
│   ├── playbooks/         # ci-e2e, dev-single-node, site, golden-refresh, security-patch
│   └── roles/
│       ├── nix_deploy/    # Install Nix + push server profile closure
│       ├── base/          # System config (ZFS, users, npm registry)
│       ├── cloudflare_dns/ # Cloudflare DNS A record management
│       ├── clickstack/    # ClickHouse, HyperDX, OTel, Caddy, MongoDB (config only)
│       ├── zfs/           # Pool creation, golden/ci datasets
│       ├── containerd/    # containerd + gVisor runsc (config only)
│       ├── verdaccio/     # Sealed npm mirror (config only)
│       ├── wireguard/     # Mesh networking (config only)
│       ├── golden_image/  # ZFS snapshot with warm caches
│       ├── forgejo/       # Git server + CI runner (config only)
│       └── agent/         # bmci agent (config only)
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── scripts/               # Security scripts, benchmark runner
├── config/default.toml    # Embedded defaults
└── flake.nix              # Dev shell + server profile (golden image)
```

## License

MIT
