# BubbleTea (Charmbracelet)

Go TUI framework based on The Elm Architecture. Functional, message-driven,
single-threaded event loop. v2.0.0 released 2026-02-24; import path moved to
`charm.land/bubbletea/v2`.

**Stars:** 41,049 | **Forks:** 1,149 | **Dependents:** 18,000+ | **License:** MIT

| Document | Focus |
|----------|-------|
| [Architecture](architecture.md) | Elm Architecture in Go, Program lifecycle, source code internals, renderer diff algorithm |
| [Ecosystem](ecosystem.md) | Bubbles, Lip Gloss (compositor, color downsampling, Cassowary), Huh, Wish, company |
| [Alternatives](alternatives.md) | tview, tcell, gocui, termbox-go, termui -- architecture and tradeoff comparison |
| [Patterns & Gotchas](patterns.md) | Composition, concurrency, testing, v1→v2 migration pain points, open regressions |
| [Security](security.md) | CVE history, escape injection, Wish isolation model, threat model for SSH-served TUI |
| [Performance](performance.md) | Table/list scaling, v2 renderer regression, memory, BubbleTea vs Ratatui benchmarks |
| [CI Orchestrator Application](ci-orchestrator.md) | How BubbleTea applies to forge-metal's Firecracker+ZFS CI platform |

## Why this research exists

forge-metal's `cmd/bmci/` CLI currently uses Cobra + a hand-rolled `TTYPrompter`.
The benchmark runner tracks concurrent jobs with atomic counters. As the orchestrator
grows (live VM dashboard, setup wizards, SSH-accessible monitoring), a proper TUI
framework becomes necessary. BubbleTea is the leading candidate because:

1. **Inline mode** -- renders within terminal flow, doesn't take over screen. Only Go TUI framework with this.
2. **Huh forms** -- drop-in replacement for the current `TTYPrompter` with validation, theming, accessibility.
3. **Testability** -- `Update()` is a pure function; state transitions unit-testable without a terminal.
4. **Wish SSH** -- serve the same TUI over SSH to bare-metal hosts. No web UI needed.
5. **Headless mode** -- `tea.WithoutRenderer()` runs the same state machine in CI/cron contexts.

## Key numbers

| Metric | Value |
|--------|-------|
| v2.0.0 release | 2026-02-24 |
| Latest patch | v2.0.2 (2026-03-09) |
| Go requirement | 1.25.0+ |
| Default FPS | 60 (max 120) |
| Primary maintainer | aymanbagabas (747 commits, Charm employee) |
| Co-founders | meowgorithm (Christian Rocha, 545 commits), muesli (Christian Muehlhaeuser, 123 commits) |
| Funding | $9.9M from Alphabet/Gradient Ventures, Firestreak, Niche Capital |

## Notable production users

CockroachDB, AWS (eks-node-viewer), Microsoft Azure (Aztify), NVIDIA, MinIO,
Ubuntu (Authd), Daytona, Truffle Security (TruffleHog), Teleport, GitLab CLI
(migrating FROM tview TO BubbleTea, epic #19748).
