# BubbleTea for forge-metal's CI Orchestrator

How BubbleTea patterns map to the Firecracker+ZFS CI platform.

## Current state

`cmd/bmci/` uses Cobra + a hand-rolled `TTYPrompter` (`internal/prompt/prompt.go`).
The benchmark runner (`internal/benchmark/runner.go`) tracks concurrent jobs with
atomic counters (`completed`, `failed`, `inFlight`) and publishes `RunStats`.

## Reference architecture: Pug

Pug (`github.com/leg100/pug`) is a Terraform task manager TUI built on BubbleTea.
It is the closest analog to a CI orchestrator in the ecosystem. Key patterns:

### Task classification

Each Terraform invocation is a task that is either:
- **Non-blocking** -- runs without preventing other tasks on the same workspace
- **Blocking** -- prevents further tasks on the same module/workspace until complete
- **Exclusive** -- globally mutually exclusive (e.g. `terraform init` with plugin cache)

**forge-metal mapping:** ZFS clone = non-blocking, Firecracker VM boot = blocking
per-slot, golden image refresh = exclusive.

### Capacity-based scheduling

Max running tasks defaults to `2 * CPU cores`, configurable. Tasks flow:
pending -> queued -> running -> completed/canceled/failed.

### Real-time output

Task output renders in a viewport with vim-style navigation. The TUI package is ~5,400
lines (50% of Pug's codebase), which gives a sense of the investment required.

## Where BubbleTea fits in forge-metal

### 1. Live CI Dashboard (`bmci dashboard`)

```
┌─ Active Jobs (3/16 slots) ──────────────────────────────────┐
│ ID       Status    Clone     Boot    Run      Written  Time  │
│ job-abc  running   1.7ms    125ms   ████░░   12.4MB   4.2s  │
│ job-def  running   1.8ms    118ms   ██░░░░    3.1MB   1.8s  │
│ job-ghi  booting   1.6ms    ...     -         0B      0.3s  │
├─ Queued (2) ────────────────────────────────────────────────┤
│ job-jkl  pending   -        -       -         -        -     │
│ job-mno  pending   -        -       -         -        -     │
└──────────────────────────────────────────────────────────────┘
│ p50: 3.2s  p99: 8.7s  throughput: 42/min  pool: 73% free    │
└──────────────────────────────────────────────────────────────┘
> [j/k] navigate  [enter] view logs  [d] destroy  [r] refresh golden
```

**Components:**
- `table` for active job list with real-time row updates
- `progress` bar per job (clone -> boot -> run -> teardown)
- `viewport` for streaming job logs (selected job's stdout/stderr)
- `spinner` for queued/booting states
- Stats footer with `tea.Tick` polling

**Data bridge:** The benchmark runner's `RunStats` already emits the data. `p.Send()`
from the orchestrator goroutines bridges to the TUI event loop.

### 2. Interactive Setup Wizard (`bmci setup`)

Replace current `TTYPrompter` with Huh forms:
- Domain configuration (input + validation)
- Cloudflare API token (password field)
- Latitude.sh project selection (select from API results)
- ZFS pool configuration (multi-step with computed defaults)

Huh provides validation, theming, accessibility out of the box. The `WithAccessible(true)`
flag drops to standard prompts for screen readers -- important for making the tool
inclusive.

### 3. Doctor Command (`bmci doctor`)

Sequential health checks with `tea.Sequence` + progress:
```
✓ ZFS pool healthy (tank: 1.2TB free)
✓ ClickHouse responding (8123)
✓ Caddy TLS valid (expires 2026-06-15)
⠋ Checking Forgejo...
○ OTel Collector
○ MongoDB
```

Each step: spinner while running, checkmark/cross when complete. Failed steps show
inline error with viewport for details. Uses `tea.Println` to scroll completed checks
above the active spinner (inline mode, package-manager pattern).

### 4. SSH-accessible Monitoring (via Wish)

```bash
ssh -p 2222 operator@ci-host
```

Serve the dashboard TUI over SSH. Each session gets its own `tea.Program` with PTY
wired. BubbleTea v2's Cursed Renderer minimizes bandwidth -- critical over WAN to
Latitude.sh hosts.

No web UI needed. Terminal is the interface.

### 5. Dual-mode Operation

```go
opts := []tea.ProgramOption{tea.WithContext(ctx)}
if !isatty.IsTerminal(os.Stdout.Fd()) {
    opts = append(opts, tea.WithoutRenderer())
}
p := tea.NewProgram(model, opts...)
```

`tea.WithoutRenderer()` disables all TUI rendering. The same orchestrator state machine
runs in CI/cron contexts, logging to stdout in plain text. One codebase, two modes.
The `tui-daemon-combo` example demonstrates this pattern.

### 6. Golden Image Refresh Wizard

Interactive TUI showing the dual-pool rotation (from DBLab research):
- Which pool is active, clone count on each
- Progress of refresh operation
- Preview of changes before committing the swap
- Confirmation before destroying old pool

Maps directly to the `tea.Sequence(buildNewImage, verifyImage, swapPools, destroyOld)`
pattern with user confirmation gates.

## Component Mapping

| forge-metal Need | BubbleTea Component |
|-----------------|-------------------|
| Active job list | `table` or `list` with real-time row updates |
| Job output streaming | `viewport` with auto-scroll, `tea.Println` for completed |
| Clone/boot/run pipeline | `progress` bar per job, multi-bar via model tree |
| Waiting indicators | `spinner` for queued/booting states |
| Setup wizards | `huh` forms (replaces `TTYPrompter`) |
| Health checks | `tea.Sequence` + spinner + checkmark output |
| Remote monitoring | `wish` SSH middleware |
| Batch scheduling | `tea.Batch` for concurrent ops, `tea.Sequence` for pipelines |
| Non-interactive mode | `tea.WithoutRenderer()` + `tea.WithContext()` |
| External events | `p.Send()` from goroutines or channel-based commands |
| Keyboard shortcuts | `key` + `help` component |
| Styling | `lipgloss` for layout, borders, colors |

## Risks and Mitigations

### 1. Rendering cost at scale

With 100+ concurrent VMs, table re-renders could lag. BubbleTea v2's Cursed Renderer
is diff-based (only redraws changed cells) and caps at 60fps. For 100 rows updating
every second, the rendering budget is ~16ms per frame -- well within tolerance.

### 2. Log output corruption

Cannot `fmt.Println()` while BubbleTea owns the terminal. All output must go through
`tea.Println`/`tea.Printf` or the message system. The current `slog.Logger` in the
benchmark runner needs routing through `tea.LogToFile()` or a custom `slog.Handler`
that sends messages.

### 3. Message ordering

Commands via `tea.Batch` have no ordering guarantees. If the dashboard shows "clone
complete" before "clone started", it's a bug in model state management, not BubbleTea.
The model's `Update()` must handle out-of-order messages gracefully -- use per-job
state machines with defined transitions.

### 4. Testing

Use teatest for golden-file integration tests. Use direct model testing for state
machine logic. Force `lipgloss.SetColorProfile(termenv.Ascii)` in test init to avoid
CI/local divergence.

## Real-world tools on BubbleTea relevant to this domain

| Tool | What it does | Relevant Pattern |
|------|-------------|-----------------|
| **Pug** | Terraform task manager | Task classification, capacity scheduling, output viewport |
| **Soft-Serve** | Git server with SSH TUI | Model tree, task manager, Wish integration |
| **eks-node-viewer** | EKS cluster visualization | Live-updating resource dashboard |
| **container-canary** | Container validator | CI-oriented validation with progress |
| **wander** | Nomad TUI | Job/allocation lifecycle |
| **docker-dash** | Docker management | Real-time container metrics table |
| **Daytona** | Dev environment manager | Provisioning wizards |
| **Atmos** | Terraform orchestration | Full Charm stack for deployment wizards |
