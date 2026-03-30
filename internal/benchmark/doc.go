// Package benchmark runs real CI workloads on ZFS clones to generate
// pressure and identify bottlenecks on bare-metal nodes.
//
// It replaces the shell-script benchmark infrastructure
// (scripts/benchmark-reprovision.sh, ansible/playbooks/benchmark-reprovision.yml)
// with a Go library that runs actual builds against real open-source projects,
// emitting [clickhouse.CIEvent] wide events with real timing, cgroup resource
// stats, and ZFS accounting data.
//
// # Architecture
//
// Two layers:
//
//   - Public: [Runner] + [Config] manage concurrent job dispatch with runtime
//     reconfiguration. [Workload] defines what to build.
//   - Private: runJob handles the single-job lifecycle on a ZFS clone:
//     allocate → git clone → phases → metrics → emit → release.
//
// # Runtime reconfiguration
//
// Config is stored as an [atomic.Pointer] and swapped atomically via
// [Runner.Reconfigure]. The dispatch loop reads the config once per
// iteration — in-flight jobs continue with their snapshot. This gives
// zero-contention reads on the hot path (no mutex) while allowing the
// operator to change workload mix, concurrency, or iteration count
// while the benchmark is running.
//
// # Workload model
//
// Each [Workload] is a real git repository with explicit phase commands.
// The default catalog includes small (next-learn), medium (taxonomy),
// and large (cal.com) Next.js projects. Phases run sequentially within
// a job but jobs run concurrently across ZFS clones. A phase failure
// (non-zero exit) does NOT stop subsequent phases — we want timing data
// for every phase regardless of outcome.
//
// # Metrics collection
//
// Three sources, mapped directly to [clickhouse.CIEvent] fields:
//
//   - ZFS harness: AllocDuration (zfs_clone_ns), WrittenBytes (zfs_written_bytes)
//   - Cgroup v2: cpu.stat (cpu_user_ms, cpu_system_ms), memory.peak
//     (memory_peak_bytes), io.stat (io_read_bytes, io_write_bytes)
//   - Phase timing: wall-clock nanoseconds per phase, exit codes
//
// Hardware and environment info (cpu_model, cores, memory_mb, disk_type,
// node_version, npm_version) are detected once at Runner startup and
// stamped onto every event.
//
// # Why real repos, not synthetic workloads
//
// Synthetic npm install + build doesn't exercise real dependency trees,
// real TypeScript compilation graphs, or real Next.js page counts. The
// bottlenecks in a 200-package monorepo are qualitatively different from
// a hello-world build. This benchmark exists to find those bottlenecks.
//
// # Why cgroup v2, not /proc/pid
//
// CI jobs spawn deep process trees (npm → node → eslint → workers).
// Cgroup v2 aggregates all processes in a scope automatically. Create
// one scope, place the root process at fork via CgroupFD, read once
// after completion. No process tree walking needed.
package benchmark
