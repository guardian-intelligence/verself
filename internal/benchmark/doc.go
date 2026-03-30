// Package benchmark runs real CI workloads on ZFS clones to generate
// pressure and identify bottlenecks on bare-metal nodes.
//
// # Architecture
//
// Two layers:
//
//   - Public: [Runner] + [Config] manage concurrent job dispatch with runtime
//     reconfiguration. [Workload] defines what to build.
//   - Private: runJob handles the single-job lifecycle on a ZFS clone:
//     allocate -> git clone -> phases -> metrics -> emit -> release.
//
// # Workload configuration
//
// Workloads are defined in a TOML file (config/workloads.toml) with per-workload
// weights controlling frequency in the mix. [LoadWorkloads] parses the file;
// [DefaultWorkloads] provides a built-in fallback. Optional top-level settings
// (concurrency, job_timeout) can also live in the file.
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
// The CLI wires SIGHUP to reload the workloads file and call Reconfigure.
// Edit the TOML, send "kill -HUP <pid>", and the new mix takes effect
// on the next dispatch iteration.
//
// # Workload model
//
// Each [Workload] is a real git repository with explicit phase commands
// and a weight controlling dispatch frequency. The default catalog
// includes small (next-learn), medium (taxonomy), and large (cal.com)
// Next.js projects. Phases run sequentially within a job but jobs run
// concurrently across ZFS clones. A phase failure (non-zero exit) does
// NOT stop subsequent phases — we want timing data for every phase
// regardless of outcome.
//
// # Weighted dispatch
//
// [BuildSelectionTable] expands workloads by weight into an index table.
// The dispatch loop cycles through this table, so a workload with weight 3
// runs 3x as often as one with weight 1. The table is rebuilt atomically
// on every Reconfigure call.
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
// # Live progress and summary
//
// A background ticker logs throughput (jobs/min), in-flight count, and
// failure rate every 10 seconds. At the end of a run, a per-workload
// summary reports count, p50/p99 durations, and total ZFS bytes written.
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
// CI jobs spawn deep process trees (npm -> node -> eslint -> workers).
// Cgroup v2 aggregates all processes in a scope automatically. Create
// one scope, place the root process at fork via CgroupFD, read once
// after completion. No process tree walking needed.
package benchmark
