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
            if (self.vms.getKey(item.job_id)) |canonical_job_id| {
                const vm = self.vms.getPtr(item.job_id).?;
                vm.present = true;
                if (!std.mem.eql(u8, vm.uds_path, item.uds_path)) {
                    const owned_uds_path = try self.allocator.dupe(u8, item.uds_path);
                    self.allocator.free(vm.uds_path);
                    vm.uds_path = owned_uds_path;
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

    fn copyCurrentTarget(self: *AgentState, job_id: []const u8, target_buf: []u8) !?[]const u8 {
        self.mutex.lock();
        defer self.mutex.unlock();

        const vm = self.vms.getPtr(job_id) orelse return null;
        if (!vm.present) return null;
        if (vm.uds_path.len > target_buf.len) return error.PathTooLong;
        @memcpy(target_buf[0..vm.uds_path.len], vm.uds_path);
        return target_buf[0..vm.uds_path.len];
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

    var uds_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    while (true) {
        const uds_path = state.copyCurrentTarget(job_id, uds_path_buf[0..]) catch |err| {
            state.recordDisconnect(job_id, @errorName(err));
            std.Thread.sleep(reconnect_backoff_ms * std.time.ns_per_ms);
            continue;
        } orelse return;
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

const SnapshotView = struct {
    schema_version: u32,
    jailer_root: []const u8,
    guest_port: u32,
    sample_period_ms: u32,
    observed_at_unix_ms: i64,
    vms: []const VM,

    const VM = struct {
        job_id: []const u8,
        uds_path: []const u8,
        present: bool,
        worker_active: bool,
        connected: bool,
        last_update_unix_ms: i64,
        last_error: ?[]const u8,
        hello: ?Hello,
        sample: ?Sample,
    };

    const Hello = struct {
        seq: u32,
        flags: u32,
        mono_ns: u64,
        wall_ns: u64,
        sample_period_ms: u32,
        guest_port: u32,
        boot_id: []const u8,
        net_iface: []const u8,
        block_dev: []const u8,
    };

    const Sample = hs.SampleFrame;
};

fn buildSnapshotJSON(allocator: std.mem.Allocator, state: *AgentState) ![]u8 {
    var vms = try std.ArrayList(SnapshotView.VM).initCapacity(allocator, state.vms.count());
    defer vms.deinit(allocator);

    state.mutex.lock();
    defer state.mutex.unlock();

    var it = state.vms.iterator();
    while (it.next()) |entry| {
        const vm = entry.value_ptr.*;
        if (!vm.present) continue;

        try vms.append(allocator, .{
            .job_id = entry.key_ptr.*,
            .uds_path = vm.uds_path,
            .present = true,
            .worker_active = vm.worker_active,
            .connected = vm.connected,
            .last_update_unix_ms = vm.last_update_unix_ms,
            .last_error = vm.lastError(),
            .hello = if (vm.hello) |hello| .{
                .seq = hello.seq,
                .flags = hello.flags,
                .mono_ns = hello.mono_ns,
                .wall_ns = hello.wall_ns,
                .sample_period_ms = hello.sample_period_ms,
                .guest_port = hello.guest_port,
                .boot_id = hs.trimPaddedString(hello.boot_id[0..]),
                .net_iface = hs.trimPaddedString(hello.net_iface[0..]),
                .block_dev = hs.trimPaddedString(hello.block_dev[0..]),
            } else null,
            .sample = vm.sample,
        });
    }

    const view = SnapshotView{
        .schema_version = schema_version,
        .jailer_root = state.jailer_root,
        .guest_port = state.guest_port,
        .sample_period_ms = hs.default_sample_period_ms,
        .observed_at_unix_ms = state.observed_at_unix_ms,
        .vms = vms.items,
    };
    return std.json.Stringify.valueAlloc(allocator, view, .{});
}

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

    state.recordDisconnect("job-a", "ConnectionRefused");
    state.recordHello("job-a", .{
        .seq = 1,
        .mono_ns = 11,
        .wall_ns = 22,
        .sample_period_ms = hs.default_sample_period_ms,
        .guest_port = hs.default_guest_port,
    });

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(vm.connected);
    try std.testing.expect(vm.hello != null);
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

    state.recordHello("job-a", .{
        .seq = 1,
        .mono_ns = 11,
        .wall_ns = 22,
        .sample_period_ms = hs.default_sample_period_ms,
        .guest_port = hs.default_guest_port,
    });
    state.recordSample("job-a", .{
        .seq = 2,
        .mono_ns = 33,
        .wall_ns = 44,
        .mem_total_kb = 1024,
        .mem_available_kb = 512,
    });

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(vm.hello != null);
    try std.testing.expect(vm.sample != null);
    try std.testing.expectEqual(@as(u32, 1), vm.hello.?.seq);
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
    state.recordDisconnect("job-a", long_error);

    const vm = state.vms.get("job-a").?;
    try std.testing.expect(!vm.connected);
    try std.testing.expectEqual(@as(usize, vm.last_error.len), vm.last_error_len);
}

test "record operations ignore unknown jobs" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    state.recordConnected("missing");
    state.recordHello("missing", .{
        .seq = 1,
        .mono_ns = 1,
        .wall_ns = 1,
        .sample_period_ms = hs.default_sample_period_ms,
        .guest_port = hs.default_guest_port,
    });
    state.recordSample("missing", .{
        .seq = 1,
        .mono_ns = 1,
        .wall_ns = 1,
    });
    state.recordDisconnect("missing", "boom");
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

test "buildSnapshotJSON emits valid JSON for empty state" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const payload = try buildSnapshotJSON(allocator, &state);
    defer allocator.free(payload);

    const parsed = try std.json.parseFromSlice(SnapshotView, allocator, payload, .{});
    defer parsed.deinit();

    try std.testing.expectEqual(schema_version, parsed.value.schema_version);
    try std.testing.expectEqualStrings("/srv/jailer/firecracker", parsed.value.jailer_root);
    try std.testing.expectEqual(hs.default_guest_port, parsed.value.guest_port);
    try std.testing.expectEqual(hs.default_sample_period_ms, parsed.value.sample_period_ms);
    try std.testing.expectEqual(@as(usize, 0), parsed.value.vms.len);
}

test "buildSnapshotJSON round-trips a connected VM" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    state.recordHello("job-a", .{
        .seq = 1,
        .flags = 7,
        .mono_ns = 11,
        .wall_ns = 22,
        .sample_period_ms = hs.default_sample_period_ms,
        .guest_port = hs.default_guest_port,
        .boot_id = [_]u8{0} ** 36,
        .net_iface = [_]u8{0} ** 16,
        .block_dev = [_]u8{0} ** 16,
    });
    state.recordSample("job-a", .{
        .seq = 2,
        .flags = hs.flag_net_missing,
        .mono_ns = 33,
        .wall_ns = 44,
        .mem_total_kb = 2048,
        .mem_available_kb = 1024,
    });

    const vm_mut = state.vms.getPtr("job-a").?;
    hs.setPaddedString(vm_mut.hello.?.boot_id[0..], "boot-id");
    hs.setPaddedString(vm_mut.hello.?.net_iface[0..], "eth0");
    hs.setPaddedString(vm_mut.hello.?.block_dev[0..], "vda");

    const payload = try buildSnapshotJSON(allocator, &state);
    defer allocator.free(payload);

    const parsed = try std.json.parseFromSlice(SnapshotView, allocator, payload, .{});
    defer parsed.deinit();

    try std.testing.expectEqual(@as(usize, 1), parsed.value.vms.len);
    try std.testing.expectEqualStrings("job-a", parsed.value.vms[0].job_id);
    try std.testing.expect(parsed.value.vms[0].connected);
    try std.testing.expect(parsed.value.vms[0].hello != null);
    try std.testing.expect(parsed.value.vms[0].sample != null);
    try std.testing.expectEqualStrings("boot-id", parsed.value.vms[0].hello.?.boot_id);
    try std.testing.expectEqualStrings("eth0", parsed.value.vms[0].hello.?.net_iface);
    try std.testing.expectEqualStrings("vda", parsed.value.vms[0].hello.?.block_dev);
    try std.testing.expectEqual(@as(u64, 2048), parsed.value.vms[0].sample.?.mem_total_kb);
    try std.testing.expectEqual(@as(u64, 1024), parsed.value.vms[0].sample.?.mem_available_kb);
}

test "buildSnapshotJSON escapes quoted error messages" {
    const allocator = std.testing.allocator;
    var state = AgentState.init(allocator, "/srv/jailer/firecracker", hs.default_guest_port);
    defer state.deinit();

    const found = [_]DiscoveredVM{
        .{ .job_id = "job-a", .uds_path = "/run/job-a.sock" },
    };
    const spawns = try state.commitDiscovery(allocator, &found);
    defer allocator.free(spawns);

    state.recordDisconnect("job-a", "bad \"quote\"");

    const payload = try buildSnapshotJSON(allocator, &state);
    defer allocator.free(payload);

    const parsed = try std.json.parseFromSlice(SnapshotView, allocator, payload, .{});
    defer parsed.deinit();

    try std.testing.expectEqual(@as(usize, 1), parsed.value.vms.len);
    try std.testing.expectEqualStrings("bad \"quote\"", parsed.value.vms[0].last_error.?);
    try std.testing.expect(parsed.value.vms[0].hello == null);
    try std.testing.expect(parsed.value.vms[0].sample == null);
}
