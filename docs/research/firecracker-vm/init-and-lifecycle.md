# Custom Init Binary and VM Lifecycle Protocol

> Designing the guest-side agent (PID 1) for forge-metal's Firecracker CI VMs.
> Covers vsock protocol, SIGCHLD reaping, credential injection, and job lifecycle.
>
> Researched 2026-03-29.

## Reference implementations

| Project | Language | Communication | Protocol |
|---------|----------|---------------|----------|
| [superfly/init-snapshot](https://github.com/superfly/init-snapshot) | Rust | vsock port 10000 | HTTP/WebSocket via warp |
| [firecracker-containerd agent](https://github.com/firecracker-microvm/firecracker-containerd/tree/main/agent) | Go | vsock port 10789 | ttrpc (lightweight gRPC) |
| [E2B envd](https://github.com/e2b-dev/infra) | Go | virtio-net port 49983 | HTTP REST |
| [ForgeVM agent](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k) | Go | vsock | Length-prefixed JSON |

## Vsock protocol design

### How vsock works in Firecracker

Guest connects to CID 2 (host) on a port; Firecracker creates an AF_UNIX socket at
`{uds_path}_{port}` on the host. Host connects to the UDS, sends `CONNECT {port}\n`,
receives `OK {port}\n`, and the connection is bridged to the guest's AF_VSOCK listener.

No TCP/IP stack required. Pure kernel-to-kernel channel. Available immediately when
the guest's vsock driver initializes (~kernel boot time).

Source: [`docs/vsock.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)

### Protocol choices

**Fly.io (init-snapshot):** HTTP + WebSocket over vsock via warp. Endpoints:
- `GET /v1/status` -- health check
- `GET /v1/exit_code` -- blocks until main process exits, returns `{code, oom_killed}`
- `POST /v1/signals` -- send signal to child
- `POST /v1/exec` -- execute command, returns `{exit_code, stdout, stderr}`
- `WS /v1/ws/exec` -- WebSocket with PTY for bidirectional streaming

**firecracker-containerd:** ttrpc (containerd's lightweight gRPC alternative) over vsock.
Implements the full containerd `TaskService` interface. Each container's stdio gets its
own vsock port carried in protobuf `ExtraData` fields (`StdinPort`, `StdoutPort`,
`StderrPort`).

**ForgeVM (28ms blog):** Length-prefixed JSON over vsock. Each message: 4 bytes (length
in network byte order) + JSON. The author explicitly regrets this choice: "the protocol
should have been gRPC, not custom JSON. gRPC over vsock would have given streaming, error
codes, and code generation for free."

### Recommendation: Connect RPC over vsock

Connect RPC (what Forgejo/Gitea Actions uses) over vsock gives:
- Protobuf code generation for Go
- Bidirectional streaming for logs
- Error codes
- Future compatibility with Forgejo runner protocol

The init's protocol is simpler than a full runner -- it receives commands from the host
orchestrator, not from Forgejo directly:

**Host -> Guest messages:**

| Message | Purpose |
|---------|---------|
| `Init{secrets, env_vars, job_id}` | Bootstrap: inject plaintext secrets (unwrapped on host), set up environment |
| `Exec{command, args, workdir, env}` | Run the CI step |
| `Signal{signal}` | Send signal to running process |
| `Shutdown{}` | Clean shutdown |

**Guest -> Host messages:**

| Message | Purpose |
|---------|---------|
| `Ready{}` | Init is ready to receive commands |
| `Log{stream, data}` | Stdout/stderr chunks (streamed) |
| `ExitStatus{code, oom_killed}` | Process exit |
| `Metrics{rss_bytes, written_bytes}` | Resource usage |

### Forgejo runner integration

The forge-metal orchestrator **implements the RunnerService protocol directly**
rather than delegating to act_runner. The protocol has only 5 RPCs:

| RPC | Purpose |
|-----|---------|
| `Register` | One-time registration (name, token, labels) |
| `Declare` | Declare capabilities at startup |
| `FetchTask` | Poll for work (returns one job's YAML + context + secrets) |
| `UpdateTask` | Report step state transitions |
| `UpdateLog` | Stream log lines back |

The architecture is:
```
Forgejo server
    |  (Connect RPC, protobuf)
forge-metal orchestrator
    |
    ├── FetchTask() every 2s
    |     └── Receives: Task{workflow_payload (YAML), context, secrets}
    |
    ├── On task:
    |     1. Parse YAML with act/model.ReadWorkflow()
    |     2. zfs clone pool/golden-zvol@ready pool/ci/job-{id}
    |     3. Boot Firecracker VM
    |     4. For each step: exec via vsock, stream logs
    |     5. UpdateTask + UpdateLog every 1s
    |     6. VM exits → zfs destroy → final UpdateTask
    |
    └── Go dependencies:
          code.gitea.io/actions-proto-go
          connectrpc.com/connect
          github.com/nektos/act/pkg/model  (YAML parsing only)
```

**Why not fork act_runner?** Adding a Firecracker executor requires forking both
act_runner and `nektos/act`, maintaining the fork, and pulling in Docker dependencies.
The protocol is simple enough (~500 lines of Go) that implementing it directly is
less maintenance burden.

**Why not run act_runner inside the VM?** This works for GitHub Actions (actuated's
approach) because the official runner binary exists. Forgejo uses a different protocol.
You'd need act_runner inside the VM, meaning networking from VM to Forgejo, larger
golden image, and polling latency from inside the VM.

**Secrets, artifacts, cache:** Secrets delivered in `Task.Secrets`. Artifacts use
Forgejo's API directly (runner sets `ACTIONS_RUNTIME_URL` env var). Cache is optional.

Source: [Gitea Actions design](https://docs.gitea.com/usage/actions/design),
[actions-proto-go](https://pkg.go.dev/code.gitea.io/actions-proto-go),
[Forgejo discussion #152](https://codeberg.org/forgejo/discussions/issues/152)

## SIGCHLD reaping (zombie prevention)

PID 1 must reap all orphaned children. When a process's parent dies, it is reparented
to PID 1. When that orphan exits, PID 1 receives SIGCHLD and must call `waitpid()`.
Without reaping, zombies accumulate in the process table. npm/node spawn many child
processes, so this matters for CI.

### Go pattern (from go-reaper, containerd)

```go
func reapLoop() {
    sigCh := make(chan os.Signal, 3)
    signal.Notify(sigCh, syscall.SIGCHLD)
    for range sigCh {
        for {
            var ws syscall.WaitStatus
            pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
            for err == syscall.EINTR {
                pid, err = syscall.Wait4(-1, &ws, syscall.WNOHANG, nil)
            }
            if err == syscall.ECHILD || pid <= 0 {
                break // no more children to reap
            }
            if pid == mainChildPID {
                mainExitCode = ws.ExitStatus()
                // trigger VM shutdown
            }
        }
    }
}
```

**Critical Go detail:** `signal.Notify` coalesces multiple SIGCHLD into one notification.
The reap loop must call `Wait4(-1, WNOHANG)` until ECHILD, not just once per signal.

### Additional requirement: PR_SET_CHILD_SUBREAPER

```go
syscall.RawSyscall(syscall.SYS_PRCTL, unix.PR_SET_CHILD_SUBREAPER, 1, 0)
```

This ensures the init adopts orphaned processes from npm scripts that double-fork.
firecracker-containerd and tini both use this.

### Race prevention (Fly.io pattern)

Fly.io's init uses a `waitpid_mutex` to prevent a race: the exec handler locks the
mutex before calling `command.output()`, and the zombie reaper also locks it. Without
this, the reaper could collect a child's exit status before the exec handler reads it.

Sources: [tini](https://github.com/krallin/tini), [dumb-init](https://github.com/Yelp/dumb-init),
[go-reaper](https://github.com/ramr/go-reaper),
[containerd reaper](https://pkg.go.dev/github.com/containerd/containerd/sys/reaper)

## Credential injection: MMDS vs vsock vs file

### Comparison for OpenBao wrapping token injection

| Property | MMDS | vsock | File on zvol |
|----------|------|-------|-------------|
| **Timing** | After network interface init | After vsock driver loads (~boot) | Available at first instruction |
| **Network required** | Yes (virtio-net + IP route) | No | No |
| **Touches disk** | No (in Firecracker process memory) | No (in-memory transfer) | **Yes** (written to ext4 on zvol) |
| **Snapshot behavior** | Data store cleared on restore (good) | N/A | Token persists in clone (bad) |
| **Security** | MMDS V2 requires session token dance | Cannot be intercepted by guest network | Token on disk until `zfs destroy` |
| **Complexity** | Medium (HTTP client in guest, IP route) | Low (vsock listener, first message) | Lowest (read file) |
| **Guest requires** | Network stack, HTTP client | AF_VSOCK socket | Nothing |

### E2B's hybrid approach

E2B uses MMDS for bootstrap identity (sandbox ID, access token hash) and HTTP POST
for actual secrets. The MMDS hash acts as authentication -- the orchestrator's POST
includes the real token, and envd validates against the hash.

### Recommendation: unwrap on host, push secrets via vsock

The guest **cannot reach OpenBao** -- it binds to `127.0.0.1:8200` on the host, and
the guest is in a network namespace with NAT to the internet only. Routing OpenBao
to the TAP gateway (`172.16.0.1:8200`) would expose it on a non-localhost interface,
widening the attack surface.

**Decision: orchestrator unwraps on host side, pushes plaintext secrets via vsock.**

```
1. Orchestrator: bao write -wrap-ttl=120s -f auth/approle/role/ci/secret-id
2. Orchestrator: unwrap → get actual secrets (DB_PASSWORD, API_KEY, etc.)
3. Orchestrator: boot Firecracker VM
4. Orchestrator → Guest (vsock): Init{secrets: {DB_PASSWORD: "...", ...}, job_id, ...}
5. Guest init: inject secrets as env vars into CI process
```

**Trust model:** Secrets are plaintext in the orchestrator's memory (they always were
during the unwrap call). The vsock channel is kernel-to-kernel with no network exposure.
The guest never needs to contact OpenBao at all.

Source: [OpenBao response wrapping](https://openbao.org/docs/concepts/response-wrapping/)

## Decision summary

| Concern | Decision | Rationale |
|---------|----------|-----------|
| Host-guest protocol | Connect RPC over vsock | ForgeVM regret, Forgejo precedent |
| Vsock port | Single port (e.g., 10000) | Fly.io pattern |
| Zombie reaping | `signal.Notify(SIGCHLD)` + `Wait4(-1, WNOHANG)` loop | go-reaper, containerd |
| Credential injection | Plaintext secrets over vsock (unwrapped on host) | OpenBao unreachable from guest |
| Secret unwrap | Orchestrator unwraps on host side | Guest never contacts OpenBao |
| Log streaming | Bidirectional streaming over vsock | firecracker-containerd pattern |
| Subreaper | `prctl(PR_SET_CHILD_SUBREAPER, 1)` | npm child process adoption |
| VM shutdown | `syscall.Reboot(LINUX_REBOOT_CMD_RESTART)` | Fly.io pattern |

## Sources

- [superfly/init-snapshot](https://github.com/superfly/init-snapshot)
- [firecracker-containerd agent](https://github.com/firecracker-microvm/firecracker-containerd/tree/main/agent)
- [E2B infra](https://github.com/e2b-dev/infra)
- [ForgeVM 28ms blog](https://dev.to/adwitiya/how-i-built-sandboxes-that-boot-in-28ms-using-firecracker-snapshots-i0k)
- [Firecracker vsock.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)
- [tini](https://github.com/krallin/tini), [dumb-init](https://github.com/Yelp/dumb-init), [go-reaper](https://github.com/ramr/go-reaper)
- [Gitea Actions design](https://docs.gitea.com/usage/actions/design)
- [actions-proto-go](https://pkg.go.dev/code.gitea.io/actions-proto-go)
- [Forgejo discussion #152](https://codeberg.org/forgejo/discussions/issues/152)
- [OpenBao response wrapping](https://openbao.org/docs/concepts/response-wrapping/)
