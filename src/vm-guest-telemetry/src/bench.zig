const std = @import("std");
const builtin = @import("builtin");
const hs = @import("vm_guest_telemetry");
const guest = @import("guest_agent");

const Writer = std.Io.Writer;

const default_sample_count: usize = 21;
const default_target_sample_ns: u64 = 20 * std.time.ns_per_ms;
const default_steady_seconds: u32 = 2;
const max_sample_count: usize = 257;

const Config = struct {
    json: bool = false,
    sample_count: usize = default_sample_count,
    target_sample_ns: u64 = default_target_sample_ns,
    steady_seconds: u32 = default_steady_seconds,
    steady: bool = true,
    live: bool = true,
};

const ProcIO = struct {
    rchar: u64 = 0,
    wchar: u64 = 0,
    syscr: u64 = 0,
    syscw: u64 = 0,
    read_bytes: u64 = 0,
    write_bytes: u64 = 0,
    cancelled_write_bytes: u64 = 0,

    fn delta(after: ProcIO, before: ProcIO) ProcIO {
        return .{
            .rchar = saturatingSub(after.rchar, before.rchar),
            .wchar = saturatingSub(after.wchar, before.wchar),
            .syscr = saturatingSub(after.syscr, before.syscr),
            .syscw = saturatingSub(after.syscw, before.syscw),
            .read_bytes = saturatingSub(after.read_bytes, before.read_bytes),
            .write_bytes = saturatingSub(after.write_bytes, before.write_bytes),
            .cancelled_write_bytes = saturatingSub(after.cancelled_write_bytes, before.cancelled_write_bytes),
        };
    }
};

const Snapshot = struct {
    wall_ns: u64,
    cpu_ns: u64,
    io: ProcIO,
    max_rss_bytes: u64,
};

const Measurement = struct {
    iterations: usize,
    wall_ns: u64,
    cpu_ns: u64,
    io: ProcIO,
    max_rss_bytes: u64,

    fn wallPerIter(self: Measurement) u64 {
        return divCeil(self.wall_ns, self.iterations);
    }

    fn cpuPerIter(self: Measurement) u64 {
        return divCeil(self.cpu_ns, self.iterations);
    }
};

const Distribution = struct {
    min: u64,
    p50: u64,
    mean: u64,
    p95: u64,
    max: u64,
};

const BenchmarkResult = struct {
    name: []const u8,
    iterations_per_sample: usize,
    sample_count: usize,
    wall: Distribution,
    cpu: Distribution,
    io: ProcIO,
    max_rss_bytes: u64,
};

const BenchmarkCase = struct {
    name: []const u8,
    run: *const fn (usize) anyerror!void,
    live: bool = false,
};

const cases = [_]BenchmarkCase{
    .{ .name = "encode_hello_frame", .run = benchEncodeHelloFrame },
    .{ .name = "encode_sample_frame", .run = benchEncodeSampleFrame },
    .{ .name = "parse_proc_stat", .run = benchParseProcStat },
    .{ .name = "parse_proc_loadavg", .run = benchParseProcLoadavg },
    .{ .name = "parse_proc_meminfo", .run = benchParseProcMeminfo },
    .{ .name = "parse_proc_diskstats", .run = benchParseProcDiskstats },
    .{ .name = "parse_proc_net_dev", .run = benchParseProcNetDev },
    .{ .name = "parse_pressure", .run = benchParsePressure },
    .{ .name = "collect_sample_live_proc_cold", .run = benchCollectSampleLiveProcCold, .live = true },
    .{ .name = "collect_encode_live_proc_cold", .run = benchCollectEncodeLiveProcCold, .live = true },
    .{ .name = "collect_sample_live_proc_reuse_fds", .run = benchCollectSampleLiveProcReuseFDs, .live = true },
    .{ .name = "collect_encode_live_proc_reuse_fds", .run = benchCollectEncodeLiveProcReuseFDs, .live = true },
};

const proc_stat_fixture =
    \\cpu  4288 0 1935 238811 280 0 61 0 0 0
    \\cpu0 4288 0 1935 238811 280 0 61 0 0 0
    \\intr 1234567 0 0 0
    \\ctxt 123456
    \\btime 1700000000
    \\processes 1234
    \\procs_running 3
    \\procs_blocked 0
    \\softirq 987654 0 0 0
;

const proc_meminfo_fixture =
    \\MemTotal:        2000000 kB
    \\MemFree:          120000 kB
    \\MemAvailable:     800000 kB
    \\Buffers:           64000 kB
    \\Cached:           256000 kB
;

const proc_diskstats_fixture =
    \\   7       0 loop0 1 0 8 0 1 0 8 0 0 0 0 0 0 0 0 0
    \\ 253       0 vda 100 0 200 0 300 0 400 0 0 0 0 0 0 0 0 0
    \\ 253       1 vda1 1 0 2 0 3 0 4 0 0 0 0 0 0 0 0 0
;

const proc_net_dev_fixture =
    \\Inter-|   Receive                                                |  Transmit
    \\ face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    \\    lo: 111 1 0 0 0 0 0 0 111 1 0 0 0 0 0 0
    \\  eth0: 12345 10 0 0 0 0 0 0 67890 20 0 0 0 0 0 0
;

const proc_pressure_fixture =
    \\some avg10=2.50 avg60=1.00 avg300=0.50 total=12345
    \\full avg10=9.99 avg60=9.99 avg300=9.99 total=54321
;

pub fn main() !void {
    const cfg = try parseArgs(std.heap.page_allocator);

    var out_buf: [32 * 1024]u8 = undefined;
    var fw = std.fs.File.stdout().writer(&out_buf);
    const out = &fw.interface;

    if (cfg.json) {
        try out.print(
            "{{\"schema\":\"vm_guest_telemetry_bench_v1\",\"event\":\"run_start\",\"optimize\":\"{s}\",\"target_sample_ns\":{d},\"sample_count\":{d}}}\n",
            .{ @tagName(builtin.mode), cfg.target_sample_ns, cfg.sample_count },
        );
    } else {
        try out.print("vm-guest-telemetry benchmark ({s})\n", .{@tagName(builtin.mode)});
        try out.print("target_sample_ns={d} sample_count={d}\n\n", .{ cfg.target_sample_ns, cfg.sample_count });
        try out.print(
            "{s: <28} {s: >10} {s: >13} {s: >13} {s: >10} {s: >10} {s: >10}\n",
            .{ "case", "iters", "wall p50", "cpu p50", "syscr", "rchar", "max rss" },
        );
    }

    for (cases) |case| {
        if (case.live and !cfg.live) continue;
        const result = runBenchmark(case, cfg) catch |err| {
            try printBenchmarkError(out, cfg, case.name, err);
            try fw.interface.flush();
            continue;
        };
        try printBenchmark(out, cfg, result);
        try fw.interface.flush();
    }

    if (cfg.live and cfg.steady and cfg.steady_seconds > 0) {
        const result = runSteady60Hz(cfg.steady_seconds) catch |err| {
            try printBenchmarkError(out, cfg, "steady_60hz", err);
            try fw.interface.flush();
            return;
        };
        try printSteady(out, cfg, result, cfg.steady_seconds);
    }

    try fw.interface.flush();
}

fn parseArgs(allocator: std.mem.Allocator) !Config {
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    var cfg = Config{};
    var index: usize = 1;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--json")) {
            cfg.json = true;
            continue;
        }
        if (std.mem.eql(u8, arg, "--no-steady")) {
            cfg.steady = false;
            continue;
        }
        if (std.mem.eql(u8, arg, "--no-live")) {
            cfg.live = false;
            cfg.steady = false;
            continue;
        }
        if (std.mem.eql(u8, arg, "--samples")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            cfg.sample_count = try std.fmt.parseInt(usize, args[index], 10);
            if (cfg.sample_count == 0 or cfg.sample_count > max_sample_count) return error.InvalidSampleCount;
            continue;
        }
        if (std.mem.eql(u8, arg, "--target-ms")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            const target_ms = try std.fmt.parseInt(u64, args[index], 10);
            if (target_ms == 0) return error.InvalidTargetDuration;
            cfg.target_sample_ns = target_ms * std.time.ns_per_ms;
            continue;
        }
        if (std.mem.eql(u8, arg, "--steady-seconds")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            cfg.steady_seconds = try std.fmt.parseInt(u32, args[index], 10);
            continue;
        }
        if (std.mem.eql(u8, arg, "--help")) {
            try std.fs.File.stdout().writeAll(
                \\Usage: zig build bench -- [options]
                \\
                \\Options:
                \\  --json               Emit machine-readable JSONL
                \\  --samples N          Samples per benchmark case (default 21)
                \\  --target-ms N        Target wall time per calibrated sample (default 20)
                \\  --steady-seconds N   Run the 60Hz steady-state sampler for N seconds (default 2)
                \\  --no-steady          Skip the 60Hz steady-state sampler
                \\  --no-live            Skip benchmarks that read live /proc state
                \\  --help               Show this help text
                \\
            );
            std.process.exit(0);
        }
        return error.UnknownArgument;
    }
    return cfg;
}

fn runBenchmark(case: BenchmarkCase, cfg: Config) !BenchmarkResult {
    try case.run(1);

    var iterations: usize = 1;
    while (true) {
        const measured = try measure(case.run, iterations);
        if (measured.wall_ns >= cfg.target_sample_ns or iterations >= (1 << 31)) break;
        iterations *= 2;
    }

    var wall_samples: [max_sample_count]u64 = undefined;
    var cpu_samples: [max_sample_count]u64 = undefined;
    var io_total = ProcIO{};
    var max_rss_bytes: u64 = 0;

    var sample_index: usize = 0;
    while (sample_index < cfg.sample_count) : (sample_index += 1) {
        const measured = try measure(case.run, iterations);
        wall_samples[sample_index] = measured.wallPerIter();
        cpu_samples[sample_index] = measured.cpuPerIter();
        io_total = addIO(io_total, measured.io);
        max_rss_bytes = @max(max_rss_bytes, measured.max_rss_bytes);
    }

    return .{
        .name = case.name,
        .iterations_per_sample = iterations,
        .sample_count = cfg.sample_count,
        .wall = distribution(wall_samples[0..cfg.sample_count]),
        .cpu = distribution(cpu_samples[0..cfg.sample_count]),
        .io = io_total,
        .max_rss_bytes = max_rss_bytes,
    };
}

fn measure(run: *const fn (usize) anyerror!void, iterations: usize) !Measurement {
    const before = try snapshot();
    try run(iterations);
    const after = try snapshot();
    return .{
        .iterations = iterations,
        .wall_ns = saturatingSub(after.wall_ns, before.wall_ns),
        .cpu_ns = saturatingSub(after.cpu_ns, before.cpu_ns),
        .io = after.io.delta(before.io),
        .max_rss_bytes = @max(before.max_rss_bytes, after.max_rss_bytes),
    };
}

fn runSteady60Hz(seconds: u32) !Measurement {
    const iterations = @as(usize, seconds) * hs.guest_sample_rate_hz;
    var sampler = try guest.BenchHooks.Sampler.init();
    defer sampler.deinit();

    const before = try snapshot();
    var seq: u32 = 1;
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const loop_started_ns = try hs.monotonicNowNs();
        const frame = try sampler.collectSampleFrame(seq);
        const encoded = hs.encodeSampleFrame(frame);
        std.mem.doNotOptimizeAway(encoded);
        seq +%= 1;
        try hs.sleepToNextTick(loop_started_ns);
    }
    const after = try snapshot();
    return .{
        .iterations = iterations,
        .wall_ns = saturatingSub(after.wall_ns, before.wall_ns),
        .cpu_ns = saturatingSub(after.cpu_ns, before.cpu_ns),
        .io = after.io.delta(before.io),
        .max_rss_bytes = @max(before.max_rss_bytes, after.max_rss_bytes),
    };
}

fn snapshot() !Snapshot {
    return .{
        .wall_ns = try clockNs(.MONOTONIC),
        .cpu_ns = try clockNs(.THREAD_CPUTIME_ID),
        .io = try readProcIO(),
        .max_rss_bytes = maxRSSBytes(),
    };
}

fn clockNs(clock_id: std.posix.clockid_t) !u64 {
    const ts = try std.posix.clock_gettime(clock_id);
    std.debug.assert(ts.sec >= 0);
    std.debug.assert(ts.nsec >= 0);
    return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
}

fn readProcIO() !ProcIO {
    var buf: [4096]u8 = undefined;
    const contents = try readSmallFile("/proc/self/io", buf[0..]);

    var out = ProcIO{};
    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        const colon = std.mem.indexOfScalar(u8, line, ':') orelse continue;
        const key = std.mem.trim(u8, line[0..colon], " \t");
        const value = try parseU64(line[colon + 1 ..]);
        if (std.mem.eql(u8, key, "rchar")) out.rchar = value;
        if (std.mem.eql(u8, key, "wchar")) out.wchar = value;
        if (std.mem.eql(u8, key, "syscr")) out.syscr = value;
        if (std.mem.eql(u8, key, "syscw")) out.syscw = value;
        if (std.mem.eql(u8, key, "read_bytes")) out.read_bytes = value;
        if (std.mem.eql(u8, key, "write_bytes")) out.write_bytes = value;
        if (std.mem.eql(u8, key, "cancelled_write_bytes")) out.cancelled_write_bytes = value;
    }
    return out;
}

fn readSmallFile(path: []const u8, buf: []u8) ![]const u8 {
    const file = try std.fs.openFileAbsolute(path, .{});
    defer file.close();
    const n = try file.readAll(buf);
    if (n == buf.len) return error.BufferTooSmall;
    return buf[0..n];
}

fn maxRSSBytes() u64 {
    const usage = std.posix.getrusage(std.posix.rusage.SELF);
    if (usage.maxrss <= 0) return 0;
    return @as(u64, @intCast(usage.maxrss)) * 1024;
}

fn distribution(samples: []u64) Distribution {
    std.mem.sort(u64, samples, {}, std.sort.asc(u64));

    var total: u128 = 0;
    for (samples) |sample| {
        total += sample;
    }
    return .{
        .min = samples[0],
        .p50 = percentile(samples, 50),
        .mean = @as(u64, @intCast(total / samples.len)),
        .p95 = percentile(samples, 95),
        .max = samples[samples.len - 1],
    };
}

fn percentile(sorted_samples: []const u64, comptime pct: u8) u64 {
    std.debug.assert(sorted_samples.len > 0);
    const index = ((sorted_samples.len - 1) * pct) / 100;
    return sorted_samples[index];
}

fn printBenchmark(out: *Writer, cfg: Config, result: BenchmarkResult) !void {
    if (cfg.json) {
        try out.print(
            "{{\"event\":\"benchmark\",\"name\":\"{s}\",\"iterations_per_sample\":{d},\"samples\":{d},\"wall_ns_per_iter_min\":{d},\"wall_ns_per_iter_p50\":{d},\"wall_ns_per_iter_mean\":{d},\"wall_ns_per_iter_p95\":{d},\"wall_ns_per_iter_max\":{d},\"cpu_ns_per_iter_min\":{d},\"cpu_ns_per_iter_p50\":{d},\"cpu_ns_per_iter_mean\":{d},\"cpu_ns_per_iter_p95\":{d},\"cpu_ns_per_iter_max\":{d},\"rchar\":{d},\"wchar\":{d},\"syscr\":{d},\"syscw\":{d},\"read_bytes\":{d},\"write_bytes\":{d},\"cancelled_write_bytes\":{d},\"max_rss_bytes\":{d}}}\n",
            .{
                result.name,
                result.iterations_per_sample,
                result.sample_count,
                result.wall.min,
                result.wall.p50,
                result.wall.mean,
                result.wall.p95,
                result.wall.max,
                result.cpu.min,
                result.cpu.p50,
                result.cpu.mean,
                result.cpu.p95,
                result.cpu.max,
                result.io.rchar,
                result.io.wchar,
                result.io.syscr,
                result.io.syscw,
                result.io.read_bytes,
                result.io.write_bytes,
                result.io.cancelled_write_bytes,
                result.max_rss_bytes,
            },
        );
        return;
    }

    try out.print(
        "{s: <28} {d: >10} {d: >13} {d: >13} {d: >10} {d: >10} {d: >10}\n",
        .{
            result.name,
            result.iterations_per_sample,
            result.wall.p50,
            result.cpu.p50,
            result.io.syscr,
            result.io.rchar,
            result.max_rss_bytes,
        },
    );
}

fn printBenchmarkError(out: *Writer, cfg: Config, name: []const u8, err: anyerror) !void {
    if (cfg.json) {
        try out.print(
            "{{\"event\":\"benchmark_error\",\"name\":\"{s}\",\"error\":\"{s}\"}}\n",
            .{ name, @errorName(err) },
        );
        return;
    }

    try out.print(
        "{s: <28} error: {s}\n",
        .{ name, @errorName(err) },
    );
}

fn printSteady(out: *Writer, cfg: Config, result: Measurement, seconds: u32) !void {
    const cpu_ns_per_sec = divCeil(result.cpu_ns, seconds);
    const rchar_per_sec = divCeil(result.io.rchar, seconds);
    const syscr_per_sec = divCeil(result.io.syscr, seconds);

    if (cfg.json) {
        try out.print(
            "{{\"event\":\"steady_60hz\",\"seconds\":{d},\"samples\":{d},\"wall_ns\":{d},\"cpu_ns\":{d},\"cpu_ns_per_sec\":{d},\"cpu_duty_ppm\":{d},\"rchar\":{d},\"rchar_per_sec\":{d},\"syscr\":{d},\"syscr_per_sec\":{d},\"read_bytes\":{d},\"write_bytes\":{d},\"max_rss_bytes\":{d}}}\n",
            .{
                seconds,
                result.iterations,
                result.wall_ns,
                result.cpu_ns,
                cpu_ns_per_sec,
                divCeil(cpu_ns_per_sec * 1_000_000, std.time.ns_per_s),
                result.io.rchar,
                rchar_per_sec,
                result.io.syscr,
                syscr_per_sec,
                result.io.read_bytes,
                result.io.write_bytes,
                result.max_rss_bytes,
            },
        );
        return;
    }

    try out.print(
        "\nsteady_60hz seconds={d} samples={d} wall_ns={d} cpu_ns={d} cpu_duty_ppm={d} rchar_per_sec={d} syscr_per_sec={d} read_bytes={d} write_bytes={d} max_rss={d}\n",
        .{
            seconds,
            result.iterations,
            result.wall_ns,
            result.cpu_ns,
            divCeil(cpu_ns_per_sec * 1_000_000, std.time.ns_per_s),
            rchar_per_sec,
            syscr_per_sec,
            result.io.read_bytes,
            result.io.write_bytes,
            result.max_rss_bytes,
        },
    );
}

fn benchEncodeHelloFrame(iterations: usize) !void {
    var frame = hs.HelloFrame{
        .mono_ns = 1,
        .wall_ns = 2,
        .boot_id = try hs.parseUuid("5691d566-f1a6-4342-8604-205e83785b21"),
        .mem_total_kb = 2048 * 1024,
    };
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        frame.seq +%= 1;
        const encoded = hs.encodeHelloFrame(frame);
        std.mem.doNotOptimizeAway(encoded);
    }
}

fn benchEncodeSampleFrame(iterations: usize) !void {
    var frame = sampleFrame();
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        frame.seq +%= 1;
        const encoded = hs.encodeSampleFrame(frame);
        std.mem.doNotOptimizeAway(encoded);
    }
}

fn benchParseProcStat(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        var frame = hs.SampleFrame{};
        try guest.BenchHooks.parseProcStatContents(&frame, proc_stat_fixture);
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchParseProcLoadavg(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        var frame = hs.SampleFrame{};
        try guest.BenchHooks.parseProcLoadavgContents(&frame, "0.47 0.21 0.08 1/142 3578\n");
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchParseProcMeminfo(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        var frame = hs.SampleFrame{};
        try guest.BenchHooks.parseProcMeminfoContents(&frame, proc_meminfo_fixture);
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchParseProcDiskstats(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        var frame = hs.SampleFrame{};
        try guest.BenchHooks.parseProcDiskstatsContents(&frame, proc_diskstats_fixture);
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchParseProcNetDev(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        var frame = hs.SampleFrame{};
        try guest.BenchHooks.parseProcNetDevContents(&frame, proc_net_dev_fixture);
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchParsePressure(iterations: usize) !void {
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const value = try guest.BenchHooks.parsePressurePct100Contents(proc_pressure_fixture);
        std.mem.doNotOptimizeAway(value);
    }
}

fn benchCollectSampleLiveProcCold(iterations: usize) !void {
    var seq: u32 = 1;
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const frame = try guest.BenchHooks.collectSampleFrame(seq);
        seq +%= 1;
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchCollectEncodeLiveProcCold(iterations: usize) !void {
    var seq: u32 = 1;
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const frame = try guest.BenchHooks.collectSampleFrame(seq);
        const encoded = hs.encodeSampleFrame(frame);
        seq +%= 1;
        std.mem.doNotOptimizeAway(encoded);
    }
}

fn benchCollectSampleLiveProcReuseFDs(iterations: usize) !void {
    var sampler = try guest.BenchHooks.Sampler.init();
    defer sampler.deinit();

    var seq: u32 = 1;
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const frame = try sampler.collectSampleFrame(seq);
        seq +%= 1;
        std.mem.doNotOptimizeAway(frame);
    }
}

fn benchCollectEncodeLiveProcReuseFDs(iterations: usize) !void {
    var sampler = try guest.BenchHooks.Sampler.init();
    defer sampler.deinit();

    var seq: u32 = 1;
    var index: usize = 0;
    while (index < iterations) : (index += 1) {
        const frame = try sampler.collectSampleFrame(seq);
        const encoded = hs.encodeSampleFrame(frame);
        seq +%= 1;
        std.mem.doNotOptimizeAway(encoded);
    }
}

fn sampleFrame() hs.SampleFrame {
    return .{
        .seq = 1,
        .mono_ns = 11,
        .wall_ns = 22,
        .cpu_user_ticks = 33,
        .cpu_system_ticks = 44,
        .cpu_idle_ticks = 55,
        .load1_centis = 66,
        .load5_centis = 77,
        .load15_centis = 88,
        .procs_running = 9,
        .procs_blocked = 10,
        .mem_available_kb = 222,
        .io_read_bytes = 333,
        .io_write_bytes = 444,
        .net_rx_bytes = 555,
        .net_tx_bytes = 666,
        .psi_cpu_pct100 = 77,
        .psi_mem_pct100 = 88,
        .psi_io_pct100 = 99,
    };
}

fn addIO(a: ProcIO, b: ProcIO) ProcIO {
    return .{
        .rchar = a.rchar + b.rchar,
        .wchar = a.wchar + b.wchar,
        .syscr = a.syscr + b.syscr,
        .syscw = a.syscw + b.syscw,
        .read_bytes = a.read_bytes + b.read_bytes,
        .write_bytes = a.write_bytes + b.write_bytes,
        .cancelled_write_bytes = a.cancelled_write_bytes + b.cancelled_write_bytes,
    };
}

fn parseU64(text: []const u8) !u64 {
    return std.fmt.parseInt(u64, std.mem.trim(u8, text, " \t"), 10);
}

fn divCeil(value: u64, divisor: anytype) u64 {
    const d = @as(u64, @intCast(divisor));
    return (value + d - 1) / d;
}

fn saturatingSub(after: u64, before: u64) u64 {
    if (after < before) return 0;
    return after - before;
}
