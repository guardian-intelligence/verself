# Firecracker Go SDK for CI Orchestration

> Programmatic VM management from Go, directly relevant to forge-metal's orchestrator.
>
> Repo: [firecracker-microvm/firecracker-go-sdk](https://github.com/firecracker-microvm/firecracker-go-sdk)
> SDK Version: 1.0.0, API target: v1.4.1, Go 1.24+
> Researched 2026-03-29.
>
> **Staleness warning:** The SDK targets Firecracker API v1.4.1. Firecracker is now
> at v1.15.0. The SDK has had no feature release since 2022-09-07 and is community-maintained
> only. Use the hybrid approach described below: SDK for process lifecycle, thin HTTP
> client for missing endpoints.

## SDK staleness and hybrid approach

The Firecracker Go SDK is **functionally stale**:

| Property | Value |
|----------|-------|
| SDK version | v1.0.0 (released 2022-09-07, 3.5 years ago) |
| Swagger API target | **v1.4.1** |
| Current Firecracker | **v1.15.0** |
| Last human commit | 2025-12-24 (dep bump, not feature work) |
| Core maintainers active | No -- only community maintenance |
| Release planned | No -- [issue #590](https://github.com/firecracker-microvm/firecracker-go-sdk/issues/590) unanswered |

**Missing from the SDK:**
- `network_overrides` on snapshot load (v1.12+)
- virtio-mem hotplug (v1.14+)
- virtio-pmem (v1.14+)
- Balloon free_page_reporting/hinting (v1.14+)
- Serial output path (v1.14+)

**What still works:** PCI transport is a CLI flag (`--enable-pci`), passable via
`VMCommandBuilder.AddArgs()`. VMClock is automatic (no API call). The SDK's process
lifecycle management (jailer, socket, CNI, signal handling, cleanup) is ~4,000 lines
of tested code worth keeping.

**Decision: hybrid approach.** Use the SDK for process lifecycle. Use a thin HTTP
client over the Unix socket for missing API endpoints:

```go
// SDK manages lifecycle
m, _ := firecracker.NewMachine(ctx, cfg)
m.Start(ctx)

// Supplemental HTTP client for endpoints the SDK doesn't cover
fc := &http.Client{Transport: &http.Transport{
    DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
        return net.Dial("unix", socketPath)
    },
}}
fc.Post("http://localhost/memory-hotplug", "application/json", body)
```

Source: [SDK issues #590, #690, #694, #707](https://github.com/firecracker-microvm/firecracker-go-sdk/issues)

## Architecture

Three core types:

| Type | Role |
|------|------|
| `Config` | All user-configurable VM settings (socket, kernel, drives, network, jailer, MMDS, vsock) |
| `Machine` | Central orchestration object: holds config, HTTP client, exec.Cmd, handler chains, cleanup stack |
| `Client` | HTTP client wrapping go-swagger-generated API client, communicates over Unix domain socket |

The `Machine` struct manages the full lifecycle: process startup, API configuration,
boot, signal forwarding, shutdown, and cleanup.

Source: `machine.go:258-281`, `firecracker.go:58-76`

## VM lifecycle

### Construction

```go
m, err := firecracker.NewMachine(ctx, cfg, opts...)
```

`NewMachine` generates a random UUID, sets default handlers (validation + init chain),
builds the VMM or jailer command, creates the API client, and configures signal
forwarding (default: SIGINT, SIGQUIT, SIGTERM, SIGHUP, SIGUSR1, SIGUSR2, SIGABRT).

### Starting

```go
err := m.Start(ctx)
```

Uses `sync.Once` to guarantee single invocation. Runs the handler chain sequentially:

1. `SetupNetwork` -- CNI invocation, netns creation
2. `SetupKernelArgs` -- inject `ip=` boot param from static IP config
3. `StartVMM` -- exec the firecracker/jailer process, wait for socket
4. `CreateLogFiles` -- create FIFOs for logs/metrics
5. `BootstrapLogging` -- configure logging via API
6. `CreateMachine` -- `PUT /machine-config`
7. `CreateBootSource` -- `PUT /boot-source`
8. `AttachDrives` -- `PUT /drives/{id}` for each drive
9. `CreateNetworkInterfaces` -- `PUT /network-interfaces/{id}`
10. `AddVsocks` -- `PUT /vsock`
11. `ConfigMmds` -- `PUT /mmds/config`

Then calls `startInstance(ctx)` which sends `InstanceStart` via API.
On any error, runs `doCleanup()` (LIFO cleanup stack).

Source: `handlers.go:304-316`, `machine.go:433-461`

### Stopping

```go
m.Shutdown(ctx)  // Ctrl+Alt+Del on x86 (graceful), SIGTERM on arm64
m.StopVMM()      // SIGTERM to firecracker process, waits for cleanup
m.Wait(ctx)      // Blocks until VMM exits
```

Context cancellation also triggers shutdown -- `context.WithTimeout` acts as a hard
deadline for the entire VM lifecycle.

Source: `machine.go:464-474`, `machine.go:640-652`

## Drive configuration (ZFS zvol)

```go
zvolPath := fmt.Sprintf("/dev/zvol/pool/ci/%s", jobID)
drives := firecracker.NewDrivesBuilder(zvolPath).Build()
```

`DrivesBuilder` accepts a root drive path and optional additional drives. The root
drive gets `IsRootDevice: true, IsReadOnly: false` automatically. For ZFS zvol block
devices, pass the `/dev/zvol/...` path directly -- Firecracker treats it as a block
device and the guest sees `/dev/vda` with ext4.

Available `DriveOpt` options:
- `WithDriveID(id)` -- custom drive ID
- `WithReadOnly(bool)` -- read-only flag
- `WithRateLimiter(limiter)` -- I/O rate limiting
- `WithCacheType(type)` -- `"Unsafe"` or `"Writeback"`
- `WithIoEngine(engine)` -- `"Sync"` (default) or `"Async"` (io_uring, developer preview)

Source: `drives.go:22-122`

## Jailer integration

```go
cfg.JailerCfg = &firecracker.JailerConfig{
    UID:            firecracker.Int(1000),
    GID:            firecracker.Int(1000),
    ID:             jobID,
    ExecFile:       "/usr/bin/firecracker",
    JailerBinary:   "/usr/bin/jailer",
    ChrootBaseDir:  "/srv/jailer",
    ChrootStrategy: firecracker.NewNaiveChrootStrategy("/path/to/kernel"),
    CgroupVersion:  "2",
    CgroupArgs:     []string{"cpu.shares=10"},
    ParentCgroup:   "ci-jobs",
    Stdout:         logFile,
    Stderr:         logFile,
}
```

When `JailerCfg` is set, `NewMachine` calls `jail()` which builds the jailer command,
computes the chroot workspace (`{ChrootBaseDir}/{basename(ExecFile)}/{ID}/root/`),
and applies the `ChrootStrategy` to adapt handlers.

### ZFS zvol + jailer: the hard-link problem

**Critical:** The `NaiveChrootStrategy` injects a `LinkFilesHandler` that uses
`os.Link()` (hard links) to copy drives into the chroot. **Block devices cannot be
hard-linked.** For ZFS zvol integration, you need one of:

1. **Custom ChrootStrategy** that uses `mknod` to create device nodes inside the chroot
   (see [jailer-security.md](jailer-security.md#jailer--zfs-zvol-how-to-pass-block-devices-into-the-jail))
2. **Bind-mount** the zvol into the chroot before jailer invocation
3. **Skip the jailer** -- Firecracker + gVisor inside VM provides equivalent isolation
   for CI workloads

A custom strategy implements `HandlersAdapter`:
```go
type ZvolChrootStrategy struct {
    KernelImagePath string
}

func (s *ZvolChrootStrategy) AdaptHandlers(handlers *firecracker.Handlers) error {
    handlers.FcInit = handlers.FcInit.AppendAfter(
        firecracker.CreateLogFilesHandlerName,
        firecracker.Handler{
            Name: "zvol.MknodDrives",
            Fn: func(ctx context.Context, m *firecracker.Machine) error {
                // mknod the zvol device node inside the chroot
                // Hard-link the kernel image (regular file, link works)
                return nil
            },
        },
    )
    return nil
}
```

Source: `jailer.go:45-103`, `jailer.go:354-418`, `jailer.go:506-534`

## Network configuration

Two approaches, mutually exclusive per interface:

### Static (pre-created TAP)

```go
firecracker.NetworkInterface{
    StaticConfiguration: &firecracker.StaticNetworkConfiguration{
        MacAddress:  "AA:FC:00:00:00:01",
        HostDevName: "tap0",
        IPConfiguration: &firecracker.IPConfiguration{
            IPAddr:      net.IPNet{IP: net.ParseIP("172.16.0.2"), Mask: net.CIDRMask(24, 32)},
            Gateway:     net.ParseIP("172.16.0.1"),
            Nameservers: []string{"8.8.8.8"},
            IfName:      "eth0",
        },
    },
    AllowMMDS: true,
}
```

### CNI (auto-creates TAP)

```go
firecracker.NetworkInterface{
    CNIConfiguration: &firecracker.CNIConfiguration{
        NetworkName: "fcnet",
        IfName:      "veth0",
        VMIfName:    "eth0",
        ConfDir:     "/etc/cni/conf.d",
        BinPath:     []string{"/opt/cni/bin"},
    },
}
```

The CNI flow: creates/reuses a network namespace, deletes pre-existing CNI network
(crash recovery), calls `cniPlugin.AddNetworkList()`, parses result to extract TAP
name/MAC/IP. Recommended plugin chain: `ptp` + `host-local` + `tc-redirect-tap`.

See [networking.md](networking.md) for detailed patterns.

Source: `network.go:96-161`

## MMDS for job config injection

```go
cfg.MmdsAddress = net.ParseIP("169.254.169.254")
cfg.MmdsVersion = firecracker.MMDSv2

// After start:
m.SetMetadata(ctx, map[string]interface{}{
    "job_id": jobID, "repo": repoURL, "commit": sha,
})
m.UpdateMetadata(ctx, patch)  // PATCH /mmds

// Read back (host side):
var result map[string]interface{}
m.GetMetadata(ctx, &result)
```

Guest accesses via HTTP: `curl http://169.254.169.254/job_id`. MMDS V2 requires a
session token (IMDSv2-compatible). Data store clears on snapshot restore by design.

See [api-and-internals.md](api-and-internals.md#mmds-microvm-metadata-service) for
MMDS details.

## Vsock for guest communication

```go
// Config
cfg.VsockDevices = []firecracker.VsockDevice{{
    ID: "v0", Path: fmt.Sprintf("/tmp/vsock-%s.sock", jobID), CID: 3,
}}

// Host dials into guest agent
conn, err := vsock.Dial(cfg.VsockDevices[0].Path, 1024,
    vsock.WithDialTimeout(200*time.Millisecond),
    vsock.WithRetryTimeout(30*time.Second),
)
// conn implements net.Conn
```

Dial protocol: connect to UDS, write `"CONNECT {port}\n"`, read `"OK {port}\n"`.
CID must be >= 3 (0, 1, 2 are reserved). Guest uses `github.com/mdlayher/vsock`
for `AF_VSOCK` sockets.

Source: `vsock/dial.go`, `vsock/listener.go`

## Snapshot create/load

### Creating

```go
err = m.PauseVM(ctx)
err = m.CreateSnapshot(ctx, "/path/to/mem", "/path/to/snap")
err = m.ResumeVM(ctx)  // if VM should continue
```

### Loading (in a fresh Machine)

```go
m, err := firecracker.NewMachine(ctx, cfg,
    firecracker.WithSnapshot("/path/to/mem", "/path/to/snap"),
)
err = m.Start(ctx)
err = m.ResumeVM(ctx)
```

`WithSnapshot` modifies the handler chain: removes cold-boot handlers (CreateMachine,
CreateBootSource, AttachDrives, etc.) and appends `LoadSnapshotHandler` instead.

For UFFD memory backend:
```go
firecracker.WithSnapshot("", snapshotPath,
    firecracker.WithMemoryBackend(models.MemoryBackendBackendTypeUffd, "uffd.sock"))
```

Source: `machine.go:1170-1183`, `opts.go:65-100`, `snapshot.go:18-33`

## Rate limiter

```go
bandwidth := firecracker.TokenBucketBuilder{}.
    WithBucketSize(100 * 1024 * 1024).
    WithRefillDuration(1 * time.Second).
    WithInitialSize(100 * 1024 * 1024).
    Build()

ops := firecracker.TokenBucketBuilder{}.
    WithBucketSize(1000).
    WithRefillDuration(1 * time.Second).
    Build()

limiter := firecracker.NewRateLimiter(bandwidth, ops)
```

Apply to network interfaces (`InRateLimiter`, `OutRateLimiter`) or drives
(`WithRateLimiter`). Update at runtime via
`m.UpdateGuestNetworkInterfaceRateLimit(ctx, ifaceID, rateLimiterSet)`.

Source: `rate_limiter.go`

## Handler system (extension mechanism)

Handlers are `{Name string, Fn func(context.Context, *Machine) error}`.
Two ordered lists: Validation and FcInit.

```go
m.Handlers.FcInit = m.Handlers.FcInit.AppendAfter(
    firecracker.StartVMMHandlerName,
    firecracker.Handler{
        Name: "custom.InjectSecrets",
        Fn: func(ctx context.Context, m *firecracker.Machine) error {
            // Custom logic after VMM starts but before boot
            return nil
        },
    },
)
```

Operations: `Append`, `Prepend`, `AppendAfter`, `Swap`, `Swappend`, `Remove`,
`Clear`, `Has`, `Run`.

Source: `handlers.go:348-351`

## Process management internals

**Startup** (`machine.go:556-664`):
1. If NetNS set (no jailer), starts in network namespace via `ns.WithNetNSPath()`
2. `m.cmd.Start()` (non-blocking)
3. Goroutine calls `m.cmd.Wait()`, then runs LIFO cleanup
4. `setupSignals()` for signal forwarding
5. `waitForSocket()` -- polls every 10ms for socket + API readiness (default 3s timeout,
   configurable via `FIRECRACKER_GO_SDK_INIT_TIMEOUT_SECONDS`)

**Cleanup stack**: LIFO, runs exactly once via `sync.Once`. Includes socket removal,
FIFO removal, CNI network deletion, netns unmount.

**Timeouts**: Each API call wraps context with 500ms timeout (configurable via
`FIRECRACKER_GO_SDK_REQUEST_TIMEOUT_MILLISECONDS`).

## Mock testing

```go
mockClient := &fctesting.MockClient{
    PutMachineConfigurationFn: func(params *ops.PutMachineConfigurationParams) (...) {
        return &ops.PutMachineConfigurationNoContent{}, nil
    },
}
m, _ := firecracker.NewMachine(ctx, cfg,
    firecracker.WithClient(
        firecracker.NewClient("", nil, false,
            firecracker.WithOpsClient(mockClient))))
```

Source: `fctesting/mock_client.go`

## Forge-metal CI orchestrator pattern

```go
func runCIJob(ctx context.Context, jobID, repoURL, commitSHA string) error {
    zvolPath := fmt.Sprintf("/dev/zvol/pool/ci/%s", jobID)

    cfg := firecracker.Config{
        SocketPath:      fmt.Sprintf("/tmp/fc-%s.sock", jobID),
        KernelImagePath: "/var/lib/ci/vmlinux",
        KernelArgs:      "console=ttyS0 root=/dev/vda rw init=/sbin/ci-init",
        Drives:          firecracker.NewDrivesBuilder(zvolPath).Build(),
        MachineCfg: models.MachineConfiguration{
            VcpuCount:  firecracker.Int64(2),
            MemSizeMib: firecracker.Int64(512),
        },
        NetworkInterfaces: []firecracker.NetworkInterface{{
            CNIConfiguration: &firecracker.CNIConfiguration{
                NetworkName: "ci-net",
                IfName:      "veth0",
                VMIfName:    "eth0",
            },
            AllowMMDS: true,
        }},
        MmdsAddress: net.ParseIP("169.254.169.254"),
        MmdsVersion: firecracker.MMDSv2,
        VsockDevices: []firecracker.VsockDevice{{
            ID: "v0", Path: fmt.Sprintf("/tmp/vsock-%s.sock", jobID), CID: 3,
        }},
        MetricsPath: fmt.Sprintf("/run/fc/%s/metrics.fifo", jobID),
    }

    m, err := firecracker.NewMachine(ctx, cfg)
    if err != nil {
        return err
    }
    if err := m.Start(ctx); err != nil {
        return err
    }

    // Inject job metadata via MMDS
    m.SetMetadata(ctx, map[string]interface{}{
        "job_id": jobID, "repo_url": repoURL, "commit": commitSHA,
    })

    // Wait for VM to exit (job completes or context timeout)
    if err := m.Wait(ctx); err != nil {
        m.StopVMM()
    }

    // Collect final metrics (FlushMetrics was called before exit)
    // Parse metrics.fifo, merge with zfs get written, INSERT into ClickHouse
    return nil
}
```

**Note:** This example skips the jailer for simplicity. For jailed execution with
ZFS zvols, implement a custom `ChrootStrategy` using `mknod` (see above).

## Sources

- [firecracker-go-sdk](https://github.com/firecracker-microvm/firecracker-go-sdk) -- `machine.go`, `handlers.go`, `jailer.go`, `drives.go`, `network.go`, `snapshot.go`, `opts.go`, `vsock/`
- [examples/cmd/snapshotting/](https://github.com/firecracker-microvm/firecracker-go-sdk/tree/main/examples/cmd/snapshotting) -- full snapshot lifecycle demo
- [fctesting/mock_client.go](https://github.com/firecracker-microvm/firecracker-go-sdk/blob/main/fctesting/mock_client.go) -- mock client for unit tests
- [tc-redirect-tap](https://github.com/awslabs/tc-redirect-tap) -- CNI plugin for TAP creation
