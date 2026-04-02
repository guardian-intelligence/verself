const std = @import("std");
const hs = @import("homestead_smelter");
const hostp = @import("host_proto.zig");

const default_jailer_root = "/srv/jailer/firecracker";
const discovery_period_ms: u64 = 250;
const reconnect_backoff_ms: u64 = 200;
const max_bridge_line_bytes: usize = 128;
const usage =
    \\Usage:
    \\  homestead-smelter-host serve --listen-uds PATH [--jailer-root PATH] [--port PORT]
    \\  homestead-smelter-host ping --control-uds PATH
    \\  homestead-smelter-host snapshot --control-uds PATH
    \\  homestead-smelter-host check-live --control-uds PATH --job-id UUID
    \\
    \\`serve` runs the long-lived host agent, discovers Firecracker VMs, opens one
    \\binary telemetry stream per guest, and exposes a local Unix socket.
    \\`ping` checks that the host agent is running.
    \\`snapshot` prints the current binary host view in a human-readable format.
    \\`check-live` succeeds when the named job has both hello and sample telemetry.
    \\
    \\Options:
    \\  --listen-uds PATH   Host-agent control socket path
    \\  --control-uds PATH  Host-agent control socket path
    \\  --jailer-root PATH  Firecracker jail root to scan (default: /srv/jailer/firecracker)
    \\  --port PORT         Guest vsock port (default: 10790)
    \\  --job-id UUID       CI job UUID for `check-live`
    \\  --help              Show this help text
    \\
;

const Mode = enum {
    serve,
    ping,
    snapshot,
    check_live,
};

const Config = struct {
    mode: Mode,
    control_uds: []const u8 = "",
    jailer_root: []const u8 = default_jailer_root,
    port: u32 = hs.default_guest_port,
    job_id: []const u8 = "",
};

const DiscoveredVM = struct {
    job_id: []const u8,
    uds_path: []const u8,
};

const VMState = struct {
    uds_path: []const u8,
    present: bool = false,
    worker_active: bool = false,
    worker_done: bool = false,
    worker_thread: ?std.Thread = null,
    connected: bool = false,
    stream_generation: u32 = 0,
    hello: ?hs.HelloFrame = null,
    sample: ?hs.SampleFrame = null,
    last_error_len: usize = 0,
    last_error: [192]u8 = [_]u8{0} ** 192,

    fn setError(self: *VMState, message: []const u8) void {
        @memset(self.last_error[0..], 0);
        self.last_error_len = @min(message.len, self.last_error.len);
        @memcpy(self.last_error[0..self.last_error_len], message[0..self.last_error_len]);
    }

    fn clearError(self: *VMState) void {
        self.last_error_len = 0;
        @memset(self.last_error[0..], 0);
    }

    fn lastError(self: *const VMState) ?[]const u8 {
        if (self.last_error_len == 0) return null;
        return self.last_error[0..self.last_error_len];
    }
};

const AgentState = struct {
    allocator: std.mem.Allocator,
    jailer_root: []const u8,
    guest_port: u32,
    next_packet_seq: u64 = 1,
    vms: std.StringHashMap(VMState),
    mutex: std.Thread.Mutex = .{},

    fn init(allocator: std.mem.Allocator, jailer_root: []const u8, guest_port: u32) AgentState {
        return .{
            .allocator = allocator,
            .jailer_root = jailer_root,
            .guest_port = guest_port,
            .vms = std.StringHashMap(VMState).init(allocator),
        };
    }

    fn deinit(self: *AgentState) void {
        var it = self.vms.iterator();
        while (it.next()) |entry| {
            self.allocator.free(entry.key_ptr.*);
            self.allocator.free(entry.value_ptr.uds_path);
        }
        self.vms.deinit();
    }

    fn commitDiscovery(self: *AgentState, temp_allocator: std.mem.Allocator, found: []const DiscoveredVM) ![]const []const u8 {
        var spawns = try std.ArrayList([]const u8).initCapacity(temp_allocator, found.len);

        self.mutex.lock();
        defer self.mutex.unlock();

        var it = self.vms.iterator();
        while (it.next()) |entry| {
            entry.value_ptr.present = false;
        }

        for (found) |item| {
            if (self.vms.getKey(item.job_id)) |canonical_job_id| {
                const vm = self.vms.getPtr(item.job_id).?;
                const was_present = vm.present;
                const path_changed = !std.mem.eql(u8, vm.uds_path, item.uds_path);
                vm.present = true;
                if (path_changed) {
                    const owned_uds_path = try self.allocator.dupe(u8, item.uds_path);
                    self.allocator.free(vm.uds_path);
                    vm.uds_path = owned_uds_path;
                }
                if (path_changed or !was_present or !vm.worker_active) vm.clearError();
                if (!vm.worker_active) {
                    vm.worker_active = true;
                    try spawns.append(temp_allocator, canonical_job_id);
                }
                continue;
            }

            const owned_job_id = try self.allocator.dupe(u8, item.job_id);
            errdefer self.allocator.free(owned_job_id);
            const owned_uds_path = try self.allocator.dupe(u8, item.uds_path);
            errdefer self.allocator.free(owned_uds_path);

            const vm = VMState{
                .uds_path = owned_uds_path,
                .present = true,
                .worker_active = true,
            };
            try self.vms.put(owned_job_id, vm);
            try spawns.append(temp_allocator, owned_job_id);
        }

        return spawns.toOwnedSlice(temp_allocator);
    }

    fn copyCurrentTarget(self: *AgentState, job_id: []const u8, target_buf: []u8) !?[]const u8 {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return null;
        if (!vm.present) return null;
        if (vm.uds_path.len > target_buf.len) return error.PathTooLong;
        @memcpy(target_buf[0..vm.uds_path.len], vm.uds_path);
        return target_buf[0..vm.uds_path.len];
    }

    fn beginStream(self: *AgentState, job_id: []const u8) ?u32 {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return null;
        vm.connected = false;
        vm.hello = null;
        vm.sample = null;
        vm.clearError();
        if (vm.stream_generation < std.math.maxInt(u32)) {
            vm.stream_generation += 1;
        }
        return vm.stream_generation;
    }

    fn recordConnected(self: *AgentState, job_id: []const u8, generation: u32) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        if (generation < vm.stream_generation) return;
        vm.stream_generation = generation;
        vm.connected = true;
        vm.clearError();
    }

    fn recordHello(self: *AgentState, job_id: []const u8, generation: u32, hello: hs.HelloFrame) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        if (generation < vm.stream_generation) return;
        vm.stream_generation = generation;
        vm.connected = true;
        vm.hello = hello;
        vm.clearError();
    }

    fn recordSample(self: *AgentState, job_id: []const u8, generation: u32, sample: hs.SampleFrame) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        if (generation < vm.stream_generation) return;
        vm.stream_generation = generation;
        vm.connected = true;
        vm.sample = sample;
        vm.clearError();
    }

    fn recordDisconnect(self: *AgentState, job_id: []const u8, generation: u32, message: []const u8) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        if (generation < vm.stream_generation) return;
        vm.stream_generation = generation;
        vm.connected = false;
        vm.setError(message);
    }

    fn recordWorkerThread(self: *AgentState, job_id: []const u8, thread: std.Thread) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.worker_thread = thread;
    }

    fn takeCompletedWorkers(self: *AgentState, temp_allocator: std.mem.Allocator) ![]const std.Thread {
        var completed = try std.ArrayList(std.Thread).initCapacity(temp_allocator, self.vms.count());

        self.mutex.lock();
        defer self.mutex.unlock();

        var it = self.vms.iterator();
        while (it.next()) |entry| {
            const vm = entry.value_ptr;
            if (!vm.worker_done) continue;
            vm.worker_done = false;
            if (vm.worker_thread) |thread| {
                vm.worker_thread = null;
                try completed.append(temp_allocator, thread);
            }
        }

        return completed.toOwnedSlice(temp_allocator);
    }

    fn takeCompletedWorker(self: *AgentState, job_id: []const u8) ?std.Thread {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return null;
        if (!vm.worker_done) return null;

        vm.worker_done = false;
        if (vm.worker_thread) |thread| {
            vm.worker_thread = null;
            return thread;
        }
        return null;
    }

    fn markWorkerStopped(self: *AgentState, job_id: []const u8) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.worker_active = false;
        vm.worker_done = true;
        vm.connected = false;
    }

    fn snapshotPackets(self: *AgentState, allocator: std.mem.Allocator) ![]hostp.Packet {
        var packets = try std.ArrayList(hostp.Packet).initCapacity(allocator, self.vms.count() * 2 + 1);
        errdefer packets.deinit(allocator);

        self.mutex.lock();
        defer self.mutex.unlock();

        var it = self.vms.iterator();
        while (it.next()) |entry| {
            const vm = entry.value_ptr.*;
            if (!vm.present or !vm.connected) continue;

            const job_id = try hs.parseUuid(entry.key_ptr.*);
            if (vm.hello) |hello| {
                try packets.append(allocator, self.makePacketLocked(
                    .hello,
                    job_id,
                    vm.stream_generation,
                    hostp.packet_flag_snapshot,
                    hs.encodeHelloFrame(hello),
                ));
            }
            if (vm.sample) |sample| {
                try packets.append(allocator, self.makePacketLocked(
                    .sample,
                    job_id,
                    vm.stream_generation,
                    hostp.packet_flag_snapshot,
                    hs.encodeSampleFrame(sample),
                ));
            }
        }

        try packets.append(allocator, self.makePacketLocked(
            .snapshot_end,
            [_]u8{0} ** 16,
            0,
            0,
            hostp.zeroPayload(),
        ));
        return packets.toOwnedSlice(allocator);
    }

    fn pongPacket(self: *AgentState) hostp.Packet {
        self.mutex.lock();
        defer self.mutex.unlock();

        return self.makePacketLocked(.pong, [_]u8{0} ** 16, 0, 0, hostp.zeroPayload());
    }

    fn makePacketLocked(
        self: *AgentState,
        kind: hostp.PacketKind,
        job_id: [16]u8,
        stream_generation: u32,
        flags: u32,
        payload: [hs.frame_size]u8,
    ) hostp.Packet {
        const packet = hostp.Packet{
            .header = .{
                .kind = kind,
                .host_seq = self.next_packet_seq,
                .observed_wall_ns = hs.realtimeNowNs() catch 0,
                .job_id = job_id,
                .stream_generation = stream_generation,
                .flags = flags,
            },
            .payload = payload,
        };
        self.next_packet_seq += 1;
        return packet;
    }
};

pub const BenchmarkResult = struct {
    stream_count: usize,
    total_samples: usize,
    elapsed_ns: u64,
    samples_per_second: u64,
};

const BenchmarkReaderArgs = struct {
    state: *AgentState,
    job_id: []const u8,
    generation: u32,
    stream: std.net.Stream,
};

const BenchmarkWriterArgs = struct {
    stream: std.net.Stream,
    stream_index: usize,
    samples_per_stream: usize,
};

pub fn benchmarkIngest(allocator: std.mem.Allocator, stream_count: usize, samples_per_stream: usize) !BenchmarkResult {
    if (stream_count == 0 or samples_per_stream == 0) return error.InvalidBenchmarkConfig;

    var state = AgentState.init(allocator, default_jailer_root, hs.default_guest_port);
    defer state.deinit();

    var found = try std.ArrayList(DiscoveredVM).initCapacity(allocator, stream_count);
    defer {
        for (found.items) |item| {
            allocator.free(item.job_id);
            allocator.free(item.uds_path);
        }
        found.deinit(allocator);
    }

    for (0..stream_count) |index| {
        const job_id = try std.fmt.allocPrint(allocator, "00000000-0000-0000-0000-{x:0>12}", .{index + 1});
        const uds_path = try std.fmt.allocPrint(allocator, "/tmp/bench-{d}.sock", .{index + 1});
        try found.append(allocator, .{
            .job_id = job_id,
            .uds_path = uds_path,
        });
    }

    const spawns = try state.commitDiscovery(allocator, found.items);
    defer allocator.free(spawns);
    if (spawns.len != stream_count) return error.InvalidBenchmarkState;

    const reader_threads = try allocator.alloc(std.Thread, stream_count);
    defer allocator.free(reader_threads);
    const writer_threads = try allocator.alloc(std.Thread, stream_count);
    defer allocator.free(writer_threads);

    var timer = try std.time.Timer.start();

    for (spawns, 0..) |job_id, index| {
        const generation = state.beginStream(job_id) orelse return error.InvalidBenchmarkState;
        const fds = try benchmarkSocketPair();
        reader_threads[index] = try std.Thread.spawn(.{}, benchmarkReaderMain, .{BenchmarkReaderArgs{
            .state = &state,
            .job_id = job_id,
            .generation = generation,
            .stream = .{ .handle = fds[0] },
        }});
        writer_threads[index] = try std.Thread.spawn(.{}, benchmarkWriterMain, .{BenchmarkWriterArgs{
            .stream = .{ .handle = fds[1] },
            .stream_index = index,
            .samples_per_stream = samples_per_stream,
        }});
    }

    for (writer_threads) |thread| thread.join();
    for (reader_threads) |thread| thread.join();

    const elapsed_ns = timer.read();
    const total_samples = stream_count * samples_per_stream;
    return .{
        .stream_count = stream_count,
        .total_samples = total_samples,
        .elapsed_ns = elapsed_ns,
        .samples_per_second = if (elapsed_ns == 0) 0 else @intCast((@as(u128, total_samples) * std.time.ns_per_s) / elapsed_ns),
    };
}

pub fn main() !void {
    const allocator = std.heap.page_allocator;
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    const config = parseArgs(args) catch |err| switch (err) {
        error.ShowUsage => {
            try std.fs.File.stdout().writeAll(usage);
            return;
        },
        else => return err,
    };

    switch (config.mode) {
        .serve => try serve(allocator, config.control_uds, config.jailer_root, config.port),
        .ping => try ping(config.control_uds),
        .snapshot => try snapshot(allocator, config.control_uds),
        .check_live => try checkLive(allocator, config.control_uds, config.job_id),
    }
}

fn parseArgs(args: []const []const u8) !Config {
    if (args.len < 2) return error.ShowUsage;

    var config = Config{
        .mode = switchMode(args[1]) orelse return error.ShowUsage,
    };

    var index: usize = 2;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--help")) {
            return error.ShowUsage;
        }
        if (std.mem.eql(u8, arg, "--listen-uds")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.control_uds = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--control-uds")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.control_uds = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--jailer-root")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.jailer_root = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--port")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.port = try hs.parsePort(args[index]);
            continue;
        }
        if (std.mem.eql(u8, arg, "--job-id")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.job_id = args[index];
            continue;
        }

        std.log.err("unknown argument: {s}", .{arg});
        return error.ShowUsage;
    }

    switch (config.mode) {
        .serve, .ping, .snapshot, .check_live => {
            if (config.control_uds.len == 0) {
                std.log.err("--listen-uds/--control-uds is required", .{});
                return error.ShowUsage;
            }
        },
    }
    if (config.mode == .check_live and config.job_id.len == 0) {
        std.log.err("--job-id is required for check-live", .{});
        return error.ShowUsage;
    }

    return config;
}

fn switchMode(value: []const u8) ?Mode {
    if (std.mem.eql(u8, value, "serve")) return .serve;
    if (std.mem.eql(u8, value, "ping")) return .ping;
    if (std.mem.eql(u8, value, "snapshot")) return .snapshot;
    if (std.mem.eql(u8, value, "check-live")) return .check_live;
    return null;
}

fn serve(allocator: std.mem.Allocator, control_uds: []const u8, jailer_root: []const u8, guest_port: u32) !void {
    var state = AgentState.init(allocator, jailer_root, guest_port);
    defer state.deinit();

    if (std.fs.path.dirname(control_uds)) |parent| {
        try std.fs.cwd().makePath(parent);
    }

    std.fs.deleteFileAbsolute(control_uds) catch |err| switch (err) {
        error.FileNotFound => {},
        else => return err,
    };

    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.bind(fd, &address.any, address.getOsSockLen());
    try std.posix.listen(fd, 16);

    std.log.info("homestead-smelter host agent listening on {s}", .{control_uds});

    const discovery_thread = try std.Thread.spawn(.{}, discoveryLoop, .{&state});
    discovery_thread.detach();

    while (true) {
        const conn_fd = std.posix.accept(fd, null, null, std.posix.SOCK.CLOEXEC) catch |err| {
            std.log.err("control accept failed: {s}", .{@errorName(err)});
            continue;
        };
        const stream = std.net.Stream{ .handle = conn_fd };
        handleControlConnection(allocator, stream, &state) catch |err| {
            std.log.err("control connection failed: {s}", .{@errorName(err)});
        };
    }
}

fn handleControlConnection(allocator: std.mem.Allocator, stream: std.net.Stream, state: *AgentState) !void {
    defer stream.close();

    var request_buf: [hostp.request_size]u8 = undefined;
    try readExact(stream, request_buf[0..]);
    const request = try hostp.decodeRequest(&request_buf);

    switch (request.kind) {
        .ping => try writePacket(stream, state.pongPacket()),
        .snapshot => {
            const packets = try state.snapshotPackets(allocator);
            defer allocator.free(packets);
            for (packets) |packet| {
                try writePacket(stream, packet);
            }
        },
    }
}

fn ping(control_uds: []const u8) !void {
    var stream = try openControlStream(control_uds);
    defer stream.close();

    try writeRequest(stream, .ping);
    const packet = try readPacket(stream);
    if (packet.header.kind != .pong) return error.InvalidHostReply;

    try std.fs.File.stdout().writeAll("PONG homestead-smelter-host\n");
}

fn snapshot(allocator: std.mem.Allocator, control_uds: []const u8) !void {
    const packets = try requestSnapshotPackets(allocator, control_uds);
    defer allocator.free(packets);

    for (packets) |packet| {
        try writeSnapshotLine(packet);
    }
}

fn checkLive(allocator: std.mem.Allocator, control_uds: []const u8, job_id_text: []const u8) !void {
    const job_id = try hs.parseUuid(job_id_text);
    const packets = try requestSnapshotPackets(allocator, control_uds);
    defer allocator.free(packets);

    var saw_hello = false;
    var saw_sample = false;
    for (packets) |packet| {
        if (!std.mem.eql(u8, packet.header.job_id[0..], job_id[0..])) continue;
        switch (packet.header.kind) {
            .hello => saw_hello = true,
            .sample => saw_sample = true,
            else => {},
        }
    }

    if (!saw_hello or !saw_sample) return error.JobNotLive;
    var buf: [96]u8 = undefined;
    const line = try std.fmt.bufPrint(&buf, "LIVE {s}\n", .{job_id_text});
    try std.fs.File.stdout().writeAll(line);
}

fn openControlStream(control_uds: []const u8) !std.net.Stream {
    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());
    return .{ .handle = fd };
}

fn requestSnapshotPackets(allocator: std.mem.Allocator, control_uds: []const u8) ![]hostp.Packet {
    var stream = try openControlStream(control_uds);
    defer stream.close();

    try writeRequest(stream, .snapshot);

    var packets = try std.ArrayList(hostp.Packet).initCapacity(allocator, 8);
    defer packets.deinit(allocator);

    while (true) {
        const packet = try readPacket(stream);
        try packets.append(allocator, packet);
        if (packet.header.kind == .snapshot_end) break;
    }

    return packets.toOwnedSlice(allocator);
}

fn writeRequest(stream: std.net.Stream, kind: hostp.RequestKind) !void {
    const request = hostp.encodeRequest(.{ .kind = kind });
    try stream.writeAll(request[0..]);
}

fn writePacket(stream: std.net.Stream, packet: hostp.Packet) !void {
    const encoded = hostp.encodePacket(packet);
    try stream.writeAll(encoded[0..]);
}

fn readPacket(stream: std.net.Stream) !hostp.Packet {
    var buf: [hostp.packet_size]u8 = undefined;
    try readExact(stream, buf[0..]);
    return try hostp.decodePacket(&buf);
}

fn readExact(stream: std.net.Stream, buf: []u8) !void {
    var read_count: usize = 0;
    while (read_count < buf.len) {
        const n = try stream.read(buf[read_count..]);
        if (n == 0) return error.EndOfStream;
        read_count += n;
    }
}

fn writeSnapshotLine(packet: hostp.Packet) !void {
    var line_buf: [512]u8 = undefined;
    switch (packet.header.kind) {
        .pong => {
            const line = try std.fmt.bufPrint(&line_buf, "PONG host_seq={d}\n", .{packet.header.host_seq});
            try std.fs.File.stdout().writeAll(line);
        },
        .hello => {
            const hello = try hs.decodeHelloFrame(&packet.payload);
            const job_id = hs.formatUuid(packet.header.job_id);
            const boot_id = hs.formatUuid(hello.boot_id);
            const line = try std.fmt.bufPrint(&line_buf,
                "HELLO job_id={s} stream_generation={d} host_seq={d} guest_seq={d} boot_id={s} mem_total_kb={d}\n",
                .{ job_id[0..], packet.header.stream_generation, packet.header.host_seq, hello.seq, boot_id[0..], hello.mem_total_kb },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .sample => {
            const sample = try hs.decodeSampleFrame(&packet.payload);
            const job_id = hs.formatUuid(packet.header.job_id);
            const line = try std.fmt.bufPrint(&line_buf,
                "SAMPLE job_id={s} stream_generation={d} host_seq={d} guest_seq={d} mem_available_kb={d} cpu_user_ticks={d}\n",
                .{ job_id[0..], packet.header.stream_generation, packet.header.host_seq, sample.seq, sample.mem_available_kb, sample.cpu_user_ticks },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .snapshot_end => {
            const line = try std.fmt.bufPrint(&line_buf, "SNAPSHOT_END host_seq={d}\n", .{packet.header.host_seq});
            try std.fs.File.stdout().writeAll(line);
        },
    }
}

fn discoveryLoop(state: *AgentState) void {
    while (true) {
        discoverOnce(state) catch |err| {
            std.log.err("discovery loop failed: {s}", .{@errorName(err)});
        };
        std.Thread.sleep(discovery_period_ms * std.time.ns_per_ms);
    }
}

fn discoverOnce(state: *AgentState) !void {
    var arena_state = std.heap.ArenaAllocator.init(state.allocator);
    defer arena_state.deinit();
    const allocator = arena_state.allocator();

    const completed_workers = try state.takeCompletedWorkers(allocator);
    for (completed_workers) |worker| {
        worker.join();
    }

    var found = try std.ArrayList(DiscoveredVM).initCapacity(allocator, 4);

    var root_dir = std.fs.openDirAbsolute(state.jailer_root, .{ .iterate = true }) catch |err| switch (err) {
        error.FileNotFound => {
            _ = try state.commitDiscovery(allocator, found.items);
            return;
        },
        else => return err,
    };
    defer root_dir.close();

    var it = root_dir.iterate();
    while (try it.next()) |entry| {
        if (entry.kind != .directory) continue;

        var path_buf: [std.fs.max_path_bytes]u8 = undefined;
        const uds_path = try std.fmt.bufPrint(
            path_buf[0..],
            "{s}/{s}/root/run/forge-control.sock",
            .{ state.jailer_root, entry.name },
        );

        std.fs.accessAbsolute(uds_path, .{}) catch |err| switch (err) {
            error.FileNotFound => continue,
            else => return err,
        };

        try found.append(allocator, .{
            .job_id = try allocator.dupe(u8, entry.name),
            .uds_path = try allocator.dupe(u8, uds_path),
        });
    }

    const spawns = try state.commitDiscovery(allocator, found.items);
    for (spawns) |job_id| {
        try spawnWorker(state, job_id);
    }
}

fn spawnWorker(state: *AgentState, job_id: []const u8) !void {
    if (state.takeCompletedWorker(job_id)) |worker| {
        worker.join();
    }

    const worker = std.Thread.spawn(.{}, vmWorkerMain, .{ state, job_id }) catch |err| {
        state.markWorkerStopped(job_id);
        return err;
    };
    state.recordWorkerThread(job_id, worker);
}

fn vmWorkerMain(state: *AgentState, job_id: []const u8) void {
    defer state.markWorkerStopped(job_id);

    var uds_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    while (true) {
        const uds_path = state.copyCurrentTarget(job_id, uds_path_buf[0..]) catch |err| {
            const generation = state.beginStream(job_id) orelse return;
            state.recordDisconnect(job_id, generation, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        } orelse return;

        const generation = state.beginStream(job_id) orelse return;
        const stream = connectGuestBridge(uds_path, state.guest_port) catch |err| {
            state.recordDisconnect(job_id, generation, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        };

        state.recordConnected(job_id, generation);
        readTelemetryStream(state, job_id, generation, stream) catch |err| {
            state.recordDisconnect(job_id, generation, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        };

        state.recordDisconnect(job_id, generation, "bridge_closed");
        std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
    }
}

fn connectGuestBridge(uds_path: []const u8, guest_port: u32) !std.net.Stream {
    var address = try std.net.Address.initUnix(uds_path);
    const stream = blk: {
        const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
        errdefer std.posix.close(fd);
        try std.posix.connect(fd, &address.any, address.getOsSockLen());
        break :blk std.net.Stream{ .handle = fd };
    };
    errdefer stream.close();

    var cmd_buf: [32]u8 = undefined;
    const cmd = try std.fmt.bufPrint(cmd_buf[0..], "CONNECT {d}\n", .{guest_port});
    try stream.writeAll(cmd);

    var ack_buf: [max_bridge_line_bytes]u8 = undefined;
    const ack = try hs.readLineInto(stream, ack_buf[0..]);
    if (!std.mem.startsWith(u8, ack, "OK ")) return error.InvalidBridgeReply;

    return stream;
}

fn readTelemetryStream(state: *AgentState, job_id: []const u8, generation: u32, stream: std.net.Stream) !void {
    defer stream.close();

    var buf: [hs.frame_size]u8 = undefined;
    while (true) {
        try readExact(stream, buf[0..]);
        switch (try hs.decodeFrameKind(&buf)) {
            .hello => state.recordHello(job_id, generation, try hs.decodeHelloFrame(&buf)),
            .sample => state.recordSample(job_id, generation, try hs.decodeSampleFrame(&buf)),
        }
    }
}

fn benchmarkReaderMain(args: BenchmarkReaderArgs) void {
    args.state.recordConnected(args.job_id, args.generation);
    readTelemetryStream(args.state, args.job_id, args.generation, args.stream) catch |err| switch (err) {
        error.EndOfStream => {},
        else => std.debug.panic("benchmark reader failed: {s}", .{@errorName(err)}),
    };
}

fn benchmarkWriterMain(args: BenchmarkWriterArgs) void {
    defer args.stream.close();

    const hello = hs.encodeHelloFrame(.{
        .seq = 0,
        .mono_ns = 1,
        .wall_ns = 2,
        .boot_id = benchmarkBootId(args.stream_index),
        .mem_total_kb = 516096,
    });
    args.stream.writeAll(hello[0..]) catch |err| {
        std.debug.panic("benchmark writer failed to write hello: {s}", .{@errorName(err)});
    };

    for (0..args.samples_per_stream) |index| {
        const sample = hs.encodeSampleFrame(.{
            .seq = @intCast(index + 1),
            .mono_ns = @as(u64, index + 1),
            .wall_ns = @as(u64, index + 2),
            .cpu_user_ticks = @intCast(index + 1),
            .cpu_system_ticks = @intCast(index + 2),
            .cpu_idle_ticks = @intCast(index + 3),
            .mem_available_kb = 401232,
            .io_read_bytes = @intCast(index * 2),
            .io_write_bytes = @intCast(index * 3),
            .net_rx_bytes = @intCast(index * 4),
            .net_tx_bytes = @intCast(index * 5),
        });
        args.stream.writeAll(sample[0..]) catch |err| {
            std.debug.panic("benchmark writer failed to write sample: {s}", .{@errorName(err)});
        };
    }
}

fn benchmarkSocketPair() ![2]std.posix.fd_t {
    var fds: [2]std.posix.fd_t = undefined;
    switch (std.posix.errno(std.c.socketpair(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0, &fds))) {
        .SUCCESS => return fds,
        else => |err| return std.posix.unexpectedErrno(err),
    }
}

fn benchmarkBootId(stream_index: usize) [16]u8 {
    var boot_id = [_]u8{0} ** 16;
    const tail = @as(u64, @intCast(stream_index + 1));
    std.mem.writeInt(u64, boot_id[8..16], tail, .big);
    return boot_id;
}

// Minimal bridge peer used by the regression test: accept one client, read the
// CONNECT line, then close without sending an ACK.
fn acceptAndCloseBridgeServer(socket_path: []const u8) void {
    acceptAndCloseBridgeServerMain(socket_path) catch |err| {
        std.debug.panic("acceptAndCloseBridgeServerMain failed: {s}", .{@errorName(err)});
    };
}

fn acceptAndCloseBridgeServerMain(socket_path: []const u8) !void {
    std.fs.deleteFileAbsolute(socket_path) catch |err| switch (err) {
        error.FileNotFound => {},
        else => return err,
    };

    var address = try std.net.Address.initUnix(socket_path);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    defer std.posix.close(fd);
    try std.posix.bind(fd, &address.any, address.getOsSockLen());
    try std.posix.listen(fd, 1);

    const conn_fd = try std.posix.accept(fd, null, null, std.posix.SOCK.CLOEXEC);
    defer std.posix.close(conn_fd);

    const stream = std.net.Stream{ .handle = conn_fd };
    var cmd_buf: [max_bridge_line_bytes]u8 = undefined;
    _ = try hs.readLineInto(stream, cmd_buf[0..]);
}

// These tests cover the current thread-per-VM daemon in host.zig. The
// deterministic single-owner aggregator semantics live in host_core.zig and are
// the source of truth for the next cutover.
test "commitDiscovery registers first discovery and spawns workers" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
        .{ .job_id = "job-b", .uds_path = "/run/job-b.sock" },
    };

    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    try std.testing.expectEqual(@as(usize, 2), spawns.len);
    try std.testing.expectEqualStrings("job-a", spawns[0]);
    try std.testing.expectEqualStrings("job-b", spawns[1]);

    const vm_a = state.vms.get("job-a").?;
    const vm_b = state.vms.get("job-b").?;
    try std.testing.expect(vm_a.present);
    try std.testing.expect(vm_a.worker_active);
    try std.testing.expect(vm_b.present);
    try std.testing.expect(vm_b.worker_active);
}

test "commitDiscovery is idempotent for already active workers" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
        .{ .job_id = "job-b", .uds_path = "/run/job-b.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns_first);
    const spawns_second = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 0), spawns_second.len);
}

test "commitDiscovery marks missing VMs absent without respawn" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const first = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
        .{ .job_id = "job-b", .uds_path = "/run/job-b.sock" },
    };
    const second = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &first);
    defer allocator.free(spawns_first);
    const spawns_second = try state.commitDiscovery(allocator, &second);
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 0), spawns_second.len);
    try std.testing.expect(state.vms.get("job-a").?.present);
    try std.testing.expect(!state.vms.get("job-b").?.present);
    try std.testing.expect(state.vms.get("job-b").?.worker_active);
}

test "commitDiscovery respawns a VM after worker stops" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns_first);
    state.markWorkerStopped("job-a");

    const spawns_second = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 1), spawns_second.len);
    try std.testing.expectEqualStrings("job-a", spawns_second[0]);
    try std.testing.expect(state.vms.get("job-a").?.worker_active);
}

test "commitDiscovery updates uds_path without leaking previous path" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const first = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/old/path.sock" },
    };
    const second = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/new/path.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &first);
    defer allocator.free(spawns_first);
    const spawns_second = try state.commitDiscovery(allocator, &second);
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 0), spawns_second.len);
    try std.testing.expectEqualStrings("/new/path.sock", state.vms.get("job-a").?.uds_path);
}

test "commitDiscovery handles empty discovery on populated state" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
        .{ .job_id = "job-b", .uds_path = "/run/job-b.sock" },
        .{ .job_id = "job-c", .uds_path = "/run/job-c.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns_first);
    const spawns_second = try state.commitDiscovery(allocator, &.{});
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 0), spawns_second.len);
    try std.testing.expect(!state.vms.get("job-a").?.present);
    try std.testing.expect(!state.vms.get("job-b").?.present);
    try std.testing.expect(!state.vms.get("job-c").?.present);
}

test "recordHello clears stale disconnect error" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    const generation = state.beginStream("job-a").?;
    state.recordDisconnect("job-a", generation, "ConnectionRefused");
    state.recordHello("job-a", generation, .{
        .seq = 0,
        .mono_ns = 11,
        .wall_ns = 22,
        .boot_id = [_]u8{0} ** 16,
        .mem_total_kb = 1024,
    });

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(vm.connected);
    try std.testing.expect(vm.hello != null);
    try std.testing.expect(vm.lastError() == null);
}

test "connectGuestBridge returns EndOfStream without double-closing fd" {
    const allocator = std.testing.allocator;
    var tmp = std.testing.tmpDir(.{});
    defer tmp.cleanup();

    const dir_path = try tmp.dir.realpathAlloc(allocator, ".");
    defer allocator.free(dir_path);
    const socket_path = try std.fs.path.join(allocator, &.{ dir_path, "bridge.sock" });
    defer allocator.free(socket_path);

    const server = try std.Thread.spawn(.{}, acceptAndCloseBridgeServer, .{socket_path});
    defer server.join();

    var ready_tries: u32 = 0;
    while (ready_tries < 100) : (ready_tries += 1) {
        std.fs.accessAbsolute(socket_path, .{}) catch |err| switch (err) {
            error.FileNotFound => {
                std.Thread.sleep(10 * std.time.ns_per_ms);
                continue;
            },
            else => return err,
        };
        break;
    }

    try std.testing.expectError(error.InvalidBridgeReply, connectGuestBridge(socket_path, hs.default_guest_port));
}

fn noopWorkerThread() void {}

test "takeCompletedWorkers joins completed worker handles" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    const worker = try std.Thread.spawn(.{}, noopWorkerThread, .{});
    state.recordWorkerThread("job-a", worker);
    state.markWorkerStopped("job-a");

    const completed = try state.takeCompletedWorkers(allocator);
    defer allocator.free(completed);

    try std.testing.expectEqual(@as(usize, 1), completed.len);
    completed[0].join();

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(!vm.worker_active);
    try std.testing.expect(!vm.worker_done);
    try std.testing.expect(vm.worker_thread == null);
}

test "commitDiscovery clears stale disconnect error on path change" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const first = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const second = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a-new.sock" },
    };

    const spawns_first = try state.commitDiscovery(allocator, &first);
    defer allocator.free(spawns_first);
    const generation = state.beginStream("job-a").?;
    state.recordDisconnect("job-a", generation, "ConnectionRefused");

    const spawns_second = try state.commitDiscovery(allocator, &second);
    defer allocator.free(spawns_second);

    try std.testing.expectEqual(@as(usize, 0), spawns_second.len);
    const vm = state.vms.get("job-a").?;
    try std.testing.expectEqualStrings("/run/job-a-new.sock", vm.uds_path);
    try std.testing.expect(vm.lastError() == null);
}

test "recordSample preserves hello metadata" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    const generation = state.beginStream("job-a").?;
    state.recordHello("job-a", generation, .{
        .seq = 0,
        .mono_ns = 11,
        .wall_ns = 22,
        .boot_id = [_]u8{0} ** 16,
        .mem_total_kb = 1024,
    });
    state.recordSample("job-a", generation, .{
        .seq = 2,
        .mono_ns = 33,
        .wall_ns = 44,
        .mem_available_kb = 512,
    });

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(vm.hello != null);
    try std.testing.expect(vm.sample != null);
    try std.testing.expectEqual(@as(u32, 0), vm.hello.?.seq);
    try std.testing.expectEqual(@as(u32, 2), vm.sample.?.seq);
}

test "recordDisconnect truncates long error messages" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    const long_error = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";
    state.recordDisconnect("job-a", state.beginStream("job-a").?, long_error);

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(!vm.connected);
    try std.testing.expectEqual(@as(usize, vm.last_error.len), vm.last_error_len);
}

test "record operations ignore unknown jobs" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    state.recordConnected("missing", 1);
    state.recordHello("missing", 1, .{
        .seq = 0,
        .mono_ns = 1,
        .wall_ns = 1,
        .boot_id = [_]u8{0} ** 16,
        .mem_total_kb = 1,
    });
    state.recordSample("missing", 1, .{
        .seq = 1,
        .mono_ns = 1,
        .wall_ns = 1,
    });
    state.recordDisconnect("missing", 1, "boom");
    state.markWorkerStopped("missing");

    try std.testing.expectEqual(@as(usize, 0), state.vms.count());
}

test "copyCurrentTarget copies the live uds_path into caller storage" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    var buf: [std.fs.max_path_bytes]u8 = undefined;
    const target = (try state.copyCurrentTarget("job-a", buf[0..])).?;
    try std.testing.expectEqualStrings("/run/job-a.sock", target);
}

test "copyCurrentTarget returns null for missing or absent jobs" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    var buf: [std.fs.max_path_bytes]u8 = undefined;
    try std.testing.expect((try state.copyCurrentTarget("missing", buf[0..])) == null);

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);
    const empty_spawns = try state.commitDiscovery(allocator, &.{});
    defer allocator.free(empty_spawns);

    try std.testing.expect((try state.copyCurrentTarget("job-a", buf[0..])) == null);
}

test "snapshotPackets emits snapshot end for empty state" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const packets = try state.snapshotPackets(allocator);
    defer allocator.free(packets);

    try std.testing.expectEqual(@as(usize, 1), packets.len);
    try std.testing.expectEqual(hostp.PacketKind.snapshot_end, packets[0].header.kind);
    try std.testing.expectEqual(@as(u64, 1), packets[0].header.host_seq);
}

test "snapshotPackets round-trips a connected VM" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const job_id = "00000000-0000-0000-0000-00000000000a";
    const found = [_]DiscoveredVM{
        .{ .job_id = job_id, .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    const generation = state.beginStream(job_id).?;
    state.recordConnected(job_id, generation);
    state.recordHello(job_id, generation, .{
        .seq = 0,
        .flags = 7,
        .mono_ns = 11,
        .wall_ns = 22,
        .boot_id = try hs.parseUuid("5691d566-f1a6-4342-8604-205e83785b21"),
        .mem_total_kb = 2048,
    });
    state.recordSample(job_id, generation, .{
        .seq = 2,
        .flags = hs.flag_net_missing,
        .mono_ns = 33,
        .wall_ns = 44,
        .mem_available_kb = 1024,
    });

    const packets = try state.snapshotPackets(allocator);
    defer allocator.free(packets);

    try std.testing.expectEqual(@as(usize, 3), packets.len);
    try std.testing.expectEqual(hostp.PacketKind.hello, packets[0].header.kind);
    try std.testing.expectEqual(hostp.PacketKind.sample, packets[1].header.kind);
    try std.testing.expectEqual(hostp.PacketKind.snapshot_end, packets[2].header.kind);
    try std.testing.expectEqual(@as(u32, generation), packets[0].header.stream_generation);
    try std.testing.expectEqual(@as(u32, generation), packets[1].header.stream_generation);
    try std.testing.expectEqual(hostp.packet_flag_snapshot, packets[0].header.flags);
    try std.testing.expectEqual(hostp.packet_flag_snapshot, packets[1].header.flags);

    const hello = try hs.decodeHelloFrame(&packets[0].payload);
    const sample = try hs.decodeSampleFrame(&packets[1].payload);
    try std.testing.expectEqual(@as(u64, 2048), hello.mem_total_kb);
    try std.testing.expectEqualDeep(try hs.parseUuid(job_id), packets[0].header.job_id);
    try std.testing.expectEqualDeep(try hs.parseUuid("5691d566-f1a6-4342-8604-205e83785b21"), hello.boot_id);
    try std.testing.expectEqual(@as(u64, 1024), sample.mem_available_kb);
}
