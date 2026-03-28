# forge-metal

Self-hosted bare-metal CI platform with ClickStack observability.

Performance-first CI on bare metal. ZFS golden image clones (~28ms), gVisor sandboxing, ClickHouse wide events, and HyperDX for real-time observability. Designed for 1000+ globally distributed nodes.

## Architecture

```
Terraform --> provision Latitude.sh bare metal
Ansible   --> configure nodes (ZFS, containerd, gVisor, Verdaccio, Forgejo, ClickStack)
bmci agent    --> execute CI jobs in gVisor sandboxes, write wide events to ClickHouse
bmci controller --> schedule jobs, track nodes, receive Forgejo webhooks

Observability (ClickStack):
  OTel Collector (4317/4318) --> ClickHouse (9000) <-- HyperDX UI (8080)
  Agents emit OTLP telemetry --> wide events stored in ci_events table
```

### ClickStack (Native)

The observability stack runs natively (no Docker):

| Component | Port | Purpose |
|-----------|------|---------|
| Caddy | 443, 80 | Reverse proxy with automatic Let's Encrypt TLS |
| ClickHouse | 9000 (native), 8123 (HTTP) | Wide event storage with optimized codecs |
| HyperDX UI | 8080 (internal) | Search, visualize, and explore CI events |
| HyperDX API | 8000 (internal) | Backend for the UI, connects to ClickHouse + MongoDB |
| OTel Collector | 4317 (gRPC), 4318 (HTTP) | Ingests OTLP telemetry from agents |
| MongoDB | 27017 (internal) | HyperDX app state (dashboards, saved searches, users) |

### Wide Events

Every CI job produces one denormalized row in `ci_events` with ~50 columns:
identity, git metadata, per-phase nanosecond timing, exit codes, cgroup resource usage,
cache effectiveness, hardware info, and timestamps. No JOINs needed.

Compression codecs per column type:
- Timestamps: `DoubleDelta + ZSTD(3)`
- Durations (Int64): `Delta(8) + ZSTD(3)`
- Byte counters: `T64 + ZSTD(3)`
- Low-cardinality strings: `LowCardinality + ZSTD(3)`
- Floats: `Gorilla + ZSTD(3)`

## Quick Start

### Prerequisites

- [Latitude.sh](https://latitude.sh) account (or any bare-metal provider)
- [Terraform](https://terraform.io) >= 1.6
- [Ansible](https://docs.ansible.com/) >= 2.16
- SSH key pair
- Go 1.23+ (to build) or [Nix](https://nixos.org/) (`nix develop`)

### Build

```bash
# With Nix (recommended)
nix develop
make build

# Without Nix
go build -o bmci ./cmd/bmci
```

### Provision & Configure

```bash
# 1. Provision bare metal
cd terraform
terraform init
terraform apply -var cluster_name=prod -var project_id=proj_xxx \
  -var ssh_public_key_path=~/.ssh/id_ed25519.pub

# 2. Configure nodes with Ansible
cd ../ansible
# Edit inventory/hosts.ini with IPs from terraform output
# Set your domain in group_vars/infra/main.yml:
#   clickstack_domain: ci.example.com
ansible-playbook playbooks/site.yml
```

### TLS

Set `clickstack_domain` in your Ansible vars:

```yaml
# ansible/group_vars/infra/main.yml
clickstack_domain: ci.example.com  # Real domain → automatic Let's Encrypt
# clickstack_domain: 192.168.1.1  # IP address → automatic self-signed (dev)
```

Point your DNS A record to the server IP. Caddy handles everything else:
Let's Encrypt issuance, renewal, HTTP->HTTPS redirect, OCSP stapling.

### Verify

```bash
# Check services
ssh ubuntu@<node-ip> 'for svc in clickhouse-server mongod otelcol hyperdx-api hyperdx-app caddy; do
  echo "$svc: $(systemctl is-active $svc)"; done'

# Query wide events
ssh ubuntu@<node-ip> 'clickhouse-client --database forge_metal --query "
  SELECT pr_author, branch, round(total_e2e_ns/1e9, 1) as e2e_s
  FROM ci_events ORDER BY created_at DESC LIMIT 10"'

# Admin credentials (generated on first deploy)
ssh ubuntu@<node-ip> 'sudo cat /etc/clickstack/admin-credentials.txt'

# Open HyperDX UI
open https://ci.example.com
```

## Commands

```bash
bmci controller    # Run the job scheduler
bmci agent join    # Register a node with the controller
bmci agent run     # Start the agent daemon
```

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
├── ansible/               # Node configuration (8 roles, 3 playbooks)
├── terraform/             # Latitude.sh provisioning
├── migrations/            # ClickHouse schema (MergeTree + Replicated)
├── scripts/security/      # Registry sealing, network policy, tarball inspection
├── config/default.toml    # Embedded defaults
└── flake.nix              # Nix dev shell + reproducible builds
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Provisioning | Terraform + Latitude.sh |
| Node config | Ansible (8 idempotent roles) |
| Container runtime | containerd + gVisor (runsc) |
| CI engine | Forgejo Actions |
| Workspace | ZFS golden image clones |
| Observability | ClickStack (ClickHouse + HyperDX + OTel Collector + Caddy) |
| Package mirror | Verdaccio (sealed) |
| Networking | WireGuard mesh |
| Orchestration | gRPC + SQLite job queue |
| Builds | Nix flakes |

## License

MIT
