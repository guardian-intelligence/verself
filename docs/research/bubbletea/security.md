# BubbleTea Security

## CVE History

BubbleTea and Wish themselves have **no CVEs**. However, sibling Charm projects
and upstream dependencies have significant vulnerabilities.

### Charm ecosystem CVEs

| CVE | Project | Severity | Description |
|-----|---------|----------|-------------|
| CVE-2022-29180 | charm (server) | Critical | HTTP request forgery -- access/delete anything in charm data dir (fixed v0.12.1) |
| CVE-2023-43809 | soft-serve | High | Public key auth bypass when keyboard-interactive auth enabled (fixed v0.6.2) |
| CVE-2024-41956 | soft-serve | High | Arbitrary code execution via crafted git-lfs requests (fixed v0.7.5) |
| CVE-2025-58355 | soft-serve | High | Arbitrary file writing through SSH API |
| CVE-2026-30832 | soft-serve | Medium | SSRF via unvalidated LFS endpoint in repo import |
| CVE-2026-33353 | soft-serve | Medium | Authenticated repo import clones server-local private repos (fixed v0.11.6) |

### Critical upstream CVE

**CVE-2024-45337** in `golang.org/x/crypto/ssh`: attacker sends public keys A and B,
authenticates with A, but application makes authorization decisions based on B. Affects
all Go SSH servers including Wish. **Fixed in `x/crypto v0.31.0`**; `gliderlabs/ssh
v0.3.8` bumps to this. Any Wish deployment must pin `x/crypto >= v0.31.0`.

## Escape Sequence Injection

The most significant attack surface for any TUI served over SSH.

**BubbleTea does NOT sanitize user-provided content before rendering.** The `View()`
function returns content that BubbleTea renders directly to the terminal. If that
content contains user-controlled data with embedded ANSI escape sequences, they will be
interpreted by the client's terminal emulator.

### Attack vectors (from published research, 10+ CVEs in terminal emulators since 2022)

| Attack | Technique | Impact |
|--------|-----------|--------|
| Invisible text | Set foreground = background color | Hide malicious content from humans |
| Cursor repositioning | Overwrite previously rendered content | Spoofing, phishing |
| Screen clearing | `\x1B[1;1H\x1B[0J` | Destroy evidence of injection |
| Deceptive hyperlinks | OSC 8 displays one URL, links another | Phishing |
| Title injection | ConEmu CVE-2022-46387, CVE-2023-39150 | Command execution via window title |
| Echoback attacks | DECRQSS/OSC 50 query reflection | Stdin injection (iTerm2 CVE-2022-45872, xterm CVE-2022-45063) |
| DNS leaks | OSC 7 (working directory) | Triggers DNS lookups to attacker infra |

### Mitigation

`charmbracelet/x/ansi` provides `Strip()` which removes all ANSI escape sequences.
All user-controlled content must pass through `ansi.Strip()` before inclusion in
`View()` output. Simplest defense: replace any `0x1b` byte with a placeholder, since
all escape sequences start with ESC.

## Wish Session Isolation

### Architecture

Wish is built on `charmbracelet/ssh` (fork of `gliderlabs/ssh`), wrapping
`golang.org/x/crypto/ssh`. No OpenSSH, no shell daemon, no PAM, no `/etc/passwd`.

Key security property: *"There's no risk of accidentally sharing a shell because
there's no default behavior that does that."* Each SSH connection runs a handler in
a goroutine.

### Isolation is goroutine-level, NOT process-level

All sessions share one Go process, one memory space, same file descriptors:
- No OS-level process isolation between sessions
- No memory limits per session
- No cgroup or namespace separation
- No seccomp filtering

A panic in one session's handler can crash the entire server. Unbounded memory
allocation in a handler can OOM the entire process.

### Authentication defaults are permissive

By default, Wish **accepts all connections** (both password and public key). Developers
must explicitly configure `WithPublicKeyAuth()`, `WithPasswordAuth()`,
`WithAuthorizedKeys()`, or `WithTrustedUserCAKeys()`.

### Rate limiting architectural flaw (Wish issue #325)

Authentication handlers execute **before** the rate limiter middleware. An attacker
can make unlimited auth attempts without triggering rate limiting. Fix requires
implementing limiting at `ConnCallback` level, not middleware.

## Resource Exhaustion

### Goroutine leak via slow/dead clients

Known `golang.org/x/crypto/ssh` issue #16287: `ssh.DiscardRequests` ranges over a
channel that never closes when the connection dies. Slow-reading clients cause `Write`
calls to block indefinitely, accumulating stuck goroutines.

### No per-session resource limits

Since sessions are goroutines in a shared process, there are no memory caps, CPU time
limits, or file descriptor limits per session. A single malicious client sending a
massive paste or triggering expensive rendering degrades all concurrent sessions.

### Input corruption under rapid delivery

When input arrives faster than BubbleTea can process it (pasted text, SSH piped input,
scripted automation), data corruption and loss occur. The ~200ms vulnerability window
during shutdown means messages queued during `tea.Quit` processing are lost.

## Terminal State Corruption

| Issue | Problem |
|-------|---------|
| #1459 (open) | Panic/crash leaves terminal in raw mode, echo disabled. Requires `stty sane`. |
| #1627 (closed) | Short-lived programs: capability query responses arrive after exit, printing escape sequences to shell prompt |
| #1590 (open) | Early quit prints garbage characters |

## Threat Model for SSH-Served BubbleTea

| Threat | Severity | Mitigation |
|--------|----------|------------|
| Escape injection via user content in `View()` | **High** | `ansi.Strip()` on all untrusted data |
| Auth brute-force (rate limiter fires after auth) | **Medium** | `ConnCallback`-level limiting |
| Goroutine/memory exhaustion from malicious clients | **Medium** | `WithIdleTimeout()`, `WithMaxTimeout()`, `GOMEMLIMIT` |
| CVE-2024-45337 pubkey auth bypass | **High** | Pin `x/crypto >= v0.31.0` |
| Panic crashes entire server | **Medium** | `recover()` in handlers |
| Large paste DoS | **Low-Medium** | Input size limits in `Update()` |
| Cross-session data leakage via shared memory | **Low** | Careful coding; no framework isolation |

## Recommendations for forge-metal

1. **Sanitize all user-controlled content** with `ansi.Strip()` before `View()`
2. **Pin `golang.org/x/crypto >= v0.31.0`** (CVE-2024-45337)
3. **Set `WithIdleTimeout()` and `WithMaxTimeout()`** on Wish server
4. **Implement `ConnCallback`-level rate limiting** (not just middleware)
5. **Explicitly configure authentication** -- never rely on default "accept all"
6. **Wrap session handlers in `recover()`** to isolate panics
7. **Limit input buffer sizes** in `Update()` handlers
8. **Set `GOMEMLIMIT`** and monitor goroutine counts per server instance
9. **Run Wish under a dedicated system user** with minimal privileges
10. For high-security: consider **process-per-session** (fork/exec per SSH connection)
    instead of goroutine-per-session
