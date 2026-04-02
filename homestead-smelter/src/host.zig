const std = @import("std");
const hs = @import("homestead_smelter");

const default_jailer_root = "/srv/jailer/firecracker";
const discovery_period_ms: u64 = 250;
const reconnect_backoff_ms: u64 = 200;
const schema_version: u32 = 2;
const max_bridge_line_bytes: usize = 128;

const usage =
    \\Usage:
    \\  homestead-smelter-host serve --listen-uds PATH [--jailer-root PATH] [--port PORT]
    \\  homestead-smelter-host ping --control-uds PATH
    \\  homestead-smelter-host snapshot --control-uds PATH
    \\
    \\`serve` runs the long-lived host agent, discovers Firecracker VMs, opens one
    \\binary telemetry stream per guest, and exposes a local Unix socket.
    \\`ping` checks that the host agent is running.
    \\`snapshot` returns the current in-memory view of live Firecracker guests.
    \\
    \\Options:
    \\  --listen-uds PATH   Host-agent control socket path
    \\  --control-uds PATH  Host-agent control socket path
    \\  --jailer-root PATH  Firecracker jail root to scan (default: /srv/jailer/firecracker)
    \\  --port PORT         Guest vsock port (default: 10790)
    \\  --help              Show this help text
    \\
;

const Mode = enum {
    serve,
    ping,
    snapshot,
};

const Config = struct {
    mode: Mode,
    control_uds: []const u8 = "",
    jailer_root: []const u8 = default_jailer_root,
    port: u32 = hs.default_guest_port,
};

const ControlCommand = union(enum) {
    ping,
    snapshot,

    fn parse(line: []const u8) ?ControlCommand {
        if (std.mem.eql(u8, line, "PING")) return .ping;
        if (std.mem.eql(u8, line, "SNAPSHOT")) return .snapshot;
        return null;
    }
};

const DiscoveredVM = struct {
    job_id: []const u8,
    uds_path: []const u8,
};

const VMState = struct {
    uds_path: []const u8,
    present: bool = false,
    worker_active: bool = false,
    connected: bool = false,
    last_update_unix_ms: i64 = 0,
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
    vms: std.StringHashMap(VMState),
    observed_at_unix_ms: i64 = 0,
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

        self.observed_at_unix_ms = std.time.milliTimestamp();

        var it = self.vms.iterator();
        while (it.next()) |entry| {
            entry.value_ptr.present = false;
        }

        for (found) |item| {
            if (self.findCanonicalJobIDLocked(item.job_id)) |canonical_job_id| {
                const vm = self.vms.getPtr(canonical_job_id).?;
                vm.present = true;
                if (!std.mem.eql(u8, vm.uds_path, item.uds_path)) {
                    vm.uds_path = try self.allocator.dupe(u8, item.uds_path);
                }
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

            try self.vms.put(owned_job_id, .{
                .uds_path = owned_uds_path,
                .present = true,
                .worker_active = true,
            });
            try spawns.append(temp_allocator, owned_job_id);
        }

        return spawns.toOwnedSlice(temp_allocator);
    }

    fn currentTarget(self: *AgentState, job_id: []const u8) ?[]const u8 {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return null;
        if (!vm.present) return null;
        return vm.uds_path;
    }

    fn recordConnected(self: *AgentState, job_id: []const u8) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.connected = true;
        vm.last_update_unix_ms = std.time.milliTimestamp();
        vm.clearError();
    }

    fn recordHello(self: *AgentState, job_id: []const u8, hello: hs.HelloFrame) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.connected = true;
        vm.hello = hello;
        vm.last_update_unix_ms = std.time.milliTimestamp();
        vm.clearError();
    }

    fn recordSample(self: *AgentState, job_id: []const u8, sample: hs.SampleFrame) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.connected = true;
        vm.sample = sample;
        vm.last_update_unix_ms = std.time.milliTimestamp();
        vm.clearError();
    }

    fn recordDisconnect(self: *AgentState, job_id: []const u8, message: []const u8) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.connected = false;
        vm.last_update_unix_ms = std.time.milliTimestamp();
        vm.setError(message);
    }

    fn markWorkerStopped(self: *AgentState, job_id: []const u8) void {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return;
        vm.worker_active = false;
        vm.connected = false;
    }

    fn findCanonicalJobIDLocked(self: *AgentState, job_id: []const u8) ?[]const u8 {
        var it = self.vms.iterator();
        while (it.next()) |entry| {
            if (std.mem.eql(u8, entry.key_ptr.*, job_id)) {
                return entry.key_ptr.*;
            }
        }
        return null;
    }
};

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
        .ping => try ping(allocator, config.control_uds),
        .snapshot => try snapshot(allocator, config.control_uds),
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

        std.log.err("unknown argument: {s}", .{arg});
        return error.ShowUsage;
    }

    switch (config.mode) {
        .serve, .ping, .snapshot => {
            if (config.control_uds.len == 0) {
                std.log.err("--listen-uds/--control-uds is required", .{});
                return error.ShowUsage;
            }
        },
    }

    return config;
}

fn switchMode(value: []const u8) ?Mode {
    if (std.mem.eql(u8, value, "serve")) return .serve;
    if (std.mem.eql(u8, value, "ping")) return .ping;
    if (std.mem.eql(u8, value, "snapshot")) return .snapshot;
    return null;
}

fn serve(allocator: std.mem.Allocator, control_uds: []const u8, jailer_root: []const u8, guest_port: u32) !void {
    var state = AgentState.init(allocator, jailer_root, guest_port);
    defer state.deinit();

    const discovery_thread = try std.Thread.spawn(.{}, discoveryLoop, .{&state});
    discovery_thread.detach();

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

fn handleControlConnection(base_allocator: std.mem.Allocator, stream: std.net.Stream, state: *AgentState) !void {
    defer stream.close();

    var line_buf: [hs.max_line_bytes]u8 = undefined;
    const line = try hs.readLineInto(stream, line_buf[0..]);
    const command = ControlCommand.parse(line) orelse {
        try hs.writeLine(stream, "ERR unsupported command");
        return;
    };

    switch (command) {
        .ping => try hs.writeLine(stream, "PONG homestead-smelter-host"),
        .snapshot => try writeSnapshotResponse(base_allocator, stream, state),
    }
}

fn ping(gpa: std.mem.Allocator, control_uds: []const u8) !void {
    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());

    const stream = std.net.Stream{ .handle = fd };
    defer stream.close();

    try hs.writeLine(stream, "PING");
    const response = try hs.readLineAlloc(gpa, stream, hs.max_line_bytes);
    defer gpa.free(response);

    if (!std.mem.eql(u8, response, "PONG homestead-smelter-host")) {
        std.log.err("unexpected host-agent reply: {s}", .{response});
        return error.InvalidHostReply;
    }

    try std.fs.File.stdout().writeAll(response);
    try std.fs.File.stdout().writeAll("\n");
}

fn snapshot(gpa: std.mem.Allocator, control_uds: []const u8) !void {
    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());

    const stream = std.net.Stream{ .handle = fd };
    defer stream.close();

    try hs.writeLine(stream, "SNAPSHOT");
    const response = try hs.readLineAlloc(gpa, stream, hs.max_snapshot_bytes);
    defer gpa.free(response);

    try std.fs.File.stdout().writeAll(response);
    try std.fs.File.stdout().writeAll("\n");
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
        const worker = try std.Thread.spawn(.{}, vmWorkerMain, .{ state, job_id });
        worker.detach();
    }
}

fn vmWorkerMain(state: *AgentState, job_id: []const u8) void {
    defer state.markWorkerStopped(job_id);

    while (true) {
        const uds_path = state.currentTarget(job_id) orelse return;
        const stream = connectGuestBridge(uds_path, state.guest_port) catch |err| {
            state.recordDisconnect(job_id, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        };

        state.recordConnected(job_id);
        readTelemetryStream(state, job_id, stream) catch |err| {
            state.recordDisconnect(job_id, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        };

        state.recordDisconnect(job_id, "bridge_closed");
        std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
    }
}

fn connectGuestBridge(uds_path: []const u8, guest_port: u32) !std.net.Stream {
    var address = try std.net.Address.initUnix(uds_path);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());

    const stream = std.net.Stream{ .handle = fd };
    errdefer stream.close();

    var cmd_buf: [32]u8 = undefined;
    const cmd = try std.fmt.bufPrint(cmd_buf[0..], "CONNECT {d}\n", .{guest_port});
    try stream.writeAll(cmd);

    var ack_buf: [max_bridge_line_bytes]u8 = undefined;
    const ack = try hs.readLineInto(stream, ack_buf[0..]);

    if (!std.mem.startsWith(u8, ack, "OK ")) {
        return error.InvalidBridgeReply;
    }

    return stream;
}

fn readTelemetryStream(state: *AgentState, job_id: []const u8, stream: std.net.Stream) !void {
    defer stream.close();

    var buf: [hs.frame_size]u8 = undefined;
    while (true) {
        try readFrame(stream, &buf);
        switch (try hs.decodeFrameKind(&buf)) {
            .hello => {
                const hello = try hs.decodeHelloFrame(&buf);
                state.recordHello(job_id, hello);
            },
            .sample => {
                const sample = try hs.decodeSampleFrame(&buf);
                state.recordSample(job_id, sample);
            },
        }
    }
}

fn readFrame(stream: std.net.Stream, buf: *[hs.frame_size]u8) !void {
    var read_count: usize = 0;
    while (read_count < buf.len) {
        const n = try stream.read(buf[read_count..]);
        if (n == 0) return error.EndOfStream;
        read_count += n;
    }
}

fn writeSnapshotResponse(allocator: std.mem.Allocator, stream: std.net.Stream, state: *AgentState) !void {
    const payload = try buildSnapshotJSON(allocator, state);
    defer allocator.free(payload);
    try hs.writeLine(stream, payload);
}

fn buildSnapshotJSON(allocator: std.mem.Allocator, state: *AgentState) ![]u8 {
    var out = try std.ArrayList(u8).initCapacity(allocator, 1024);

    state.mutex.lock();
    defer state.mutex.unlock();

    try out.appendSlice(allocator, "{\"schema_version\":");
    try appendInt(&out, allocator, schema_version);
    try out.appendSlice(allocator, ",\"jailer_root\":");
    try appendJSONString(&out, allocator, state.jailer_root);
    try out.appendSlice(allocator, ",\"guest_port\":");
    try appendInt(&out, allocator, state.guest_port);
    try out.appendSlice(allocator, ",\"sample_period_ms\":");
    try appendInt(&out, allocator, hs.default_sample_period_ms);
    try out.appendSlice(allocator, ",\"observed_at_unix_ms\":");
    try appendInt(&out, allocator, state.observed_at_unix_ms);
    try out.appendSlice(allocator, ",\"vms\":[");

    var first = true;
    var it = state.vms.iterator();
    while (it.next()) |entry| {
        const vm = entry.value_ptr.*;
        if (!vm.present) continue;

        if (!first) try out.appendSlice(allocator, ",");
        first = false;

        try out.appendSlice(allocator, "{\"job_id\":");
        try appendJSONString(&out, allocator, entry.key_ptr.*);
        try out.appendSlice(allocator, ",\"uds_path\":");
        try appendJSONString(&out, allocator, vm.uds_path);
        try out.appendSlice(allocator, ",\"present\":true");
        try out.appendSlice(allocator, ",\"worker_active\":");
        try appendBool(&out, allocator, vm.worker_active);
        try out.appendSlice(allocator, ",\"connected\":");
        try appendBool(&out, allocator, vm.connected);
        try out.appendSlice(allocator, ",\"last_update_unix_ms\":");
        try appendInt(&out, allocator, vm.last_update_unix_ms);
        try out.appendSlice(allocator, ",\"last_error\":");
        if (vm.lastError()) |message| {
            try appendJSONString(&out, allocator, message);
        } else {
            try out.appendSlice(allocator, "null");
        }
        try out.appendSlice(allocator, ",\"hello\":");
        if (vm.hello) |hello| {
            try appendHelloJSON(&out, allocator, hello);
        } else {
            try out.appendSlice(allocator, "null");
        }
        try out.appendSlice(allocator, ",\"sample\":");
        if (vm.sample) |sample| {
            try appendSampleJSON(&out, allocator, sample);
        } else {
            try out.appendSlice(allocator, "null");
        }
        try out.appendSlice(allocator, "}");
    }

    try out.appendSlice(allocator, "]}");
    return out.toOwnedSlice(allocator);
}

fn appendHelloJSON(out: *std.ArrayList(u8), allocator: std.mem.Allocator, hello: hs.HelloFrame) !void {
    try out.appendSlice(allocator, "{\"seq\":");
    try appendInt(out, allocator, hello.seq);
    try out.appendSlice(allocator, ",\"flags\":");
    try appendInt(out, allocator, hello.flags);
    try out.appendSlice(allocator, ",\"mono_ns\":");
    try appendInt(out, allocator, hello.mono_ns);
    try out.appendSlice(allocator, ",\"wall_ns\":");
    try appendInt(out, allocator, hello.wall_ns);
    try out.appendSlice(allocator, ",\"sample_period_ms\":");
    try appendInt(out, allocator, hello.sample_period_ms);
    try out.appendSlice(allocator, ",\"guest_port\":");
    try appendInt(out, allocator, hello.guest_port);
    try out.appendSlice(allocator, ",\"boot_id\":");
    try appendJSONString(out, allocator, hs.trimPaddedString(hello.boot_id[0..]));
    try out.appendSlice(allocator, ",\"net_iface\":");
    try appendJSONString(out, allocator, hs.trimPaddedString(hello.net_iface[0..]));
    try out.appendSlice(allocator, ",\"block_dev\":");
    try appendJSONString(out, allocator, hs.trimPaddedString(hello.block_dev[0..]));
    try out.appendSlice(allocator, "}");
}

fn appendSampleJSON(out: *std.ArrayList(u8), allocator: std.mem.Allocator, sample: hs.SampleFrame) !void {
    try out.appendSlice(allocator, "{\"seq\":");
    try appendInt(out, allocator, sample.seq);
    try out.appendSlice(allocator, ",\"flags\":");
    try appendInt(out, allocator, sample.flags);
    try out.appendSlice(allocator, ",\"mono_ns\":");
    try appendInt(out, allocator, sample.mono_ns);
    try out.appendSlice(allocator, ",\"wall_ns\":");
    try appendInt(out, allocator, sample.wall_ns);
    try out.appendSlice(allocator, ",\"cpu_user_ticks\":");
    try appendInt(out, allocator, sample.cpu_user_ticks);
    try out.appendSlice(allocator, ",\"cpu_system_ticks\":");
    try appendInt(out, allocator, sample.cpu_system_ticks);
    try out.appendSlice(allocator, ",\"cpu_idle_ticks\":");
    try appendInt(out, allocator, sample.cpu_idle_ticks);
    try out.appendSlice(allocator, ",\"load1_centis\":");
    try appendInt(out, allocator, sample.load1_centis);
    try out.appendSlice(allocator, ",\"load5_centis\":");
    try appendInt(out, allocator, sample.load5_centis);
    try out.appendSlice(allocator, ",\"load15_centis\":");
    try appendInt(out, allocator, sample.load15_centis);
    try out.appendSlice(allocator, ",\"procs_running\":");
    try appendInt(out, allocator, sample.procs_running);
    try out.appendSlice(allocator, ",\"procs_blocked\":");
    try appendInt(out, allocator, sample.procs_blocked);
    try out.appendSlice(allocator, ",\"mem_total_kb\":");
    try appendInt(out, allocator, sample.mem_total_kb);
    try out.appendSlice(allocator, ",\"mem_available_kb\":");
    try appendInt(out, allocator, sample.mem_available_kb);
    try out.appendSlice(allocator, ",\"io_read_bytes\":");
    try appendInt(out, allocator, sample.io_read_bytes);
    try out.appendSlice(allocator, ",\"io_write_bytes\":");
    try appendInt(out, allocator, sample.io_write_bytes);
    try out.appendSlice(allocator, ",\"net_rx_bytes\":");
    try appendInt(out, allocator, sample.net_rx_bytes);
    try out.appendSlice(allocator, ",\"net_tx_bytes\":");
    try appendInt(out, allocator, sample.net_tx_bytes);
    try out.appendSlice(allocator, ",\"psi_cpu_pct100\":");
    try appendInt(out, allocator, sample.psi_cpu_pct100);
    try out.appendSlice(allocator, ",\"psi_mem_pct100\":");
    try appendInt(out, allocator, sample.psi_mem_pct100);
    try out.appendSlice(allocator, ",\"psi_io_pct100\":");
    try appendInt(out, allocator, sample.psi_io_pct100);
    try out.appendSlice(allocator, "}");
}

fn appendBool(out: *std.ArrayList(u8), allocator: std.mem.Allocator, value: bool) !void {
    try out.appendSlice(allocator, if (value) "true" else "false");
}

fn appendInt(out: *std.ArrayList(u8), allocator: std.mem.Allocator, value: anytype) !void {
    var buf: [32]u8 = undefined;
    const text = try std.fmt.bufPrint(&buf, "{d}", .{value});
    try out.appendSlice(allocator, text);
}

fn appendJSONString(out: *std.ArrayList(u8), allocator: std.mem.Allocator, value: []const u8) !void {
    try out.append(allocator, '"');
    for (value) |byte| {
        switch (byte) {
            '"' => try out.appendSlice(allocator, "\\\""),
            '\\' => try out.appendSlice(allocator, "\\\\"),
            '\n' => try out.appendSlice(allocator, "\\n"),
            '\r' => try out.appendSlice(allocator, "\\r"),
            '\t' => try out.appendSlice(allocator, "\\t"),
            else => {
                if (byte < 0x20) {
                    var buf: [6]u8 = undefined;
                    const escaped = try std.fmt.bufPrint(&buf, "\\u{x:0>4}", .{@as(u16, byte)});
                    try out.appendSlice(allocator, escaped);
                } else {
                    try out.append(allocator, byte);
                }
            },
        }
    }
    try out.append(allocator, '"');
}
