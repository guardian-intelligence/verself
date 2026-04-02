const std = @import("std");
const hs = @import("homestead_smelter");

const default_jailer_root = "/srv/jailer/firecracker";
const snapshot_probe_message = "snapshot from host";

const usage =
    \\Usage:
    \\  homestead-smelter-host serve --listen-uds PATH [--jailer-root PATH] [--port PORT]
    \\  homestead-smelter-host ping --control-uds PATH
    \\  homestead-smelter-host snapshot --control-uds PATH
    \\  homestead-smelter-host probe-guest --uds-path PATH [--port PORT] [--message TEXT]
    \\
    \\`serve` runs the long-lived host agent and exposes a local Unix socket.
    \\`ping` checks that the host agent is running.
    \\`snapshot` asks the host agent for its current Firecracker guest view.
    \\`probe-guest` connects to Firecracker's vsock Unix bridge, issues
    \\CONNECT <port>, sends one hello line to the guest agent, and prints the reply.
    \\
    \\Options:
    \\  --listen-uds PATH   Host-agent control socket path
    \\  --control-uds PATH  Host-agent control socket path
    \\  --jailer-root PATH  Firecracker jail root to scan (default: /srv/jailer/firecracker)
    \\  --uds-path PATH     Firecracker vsock bridge socket path
    \\  --port PORT         Guest vsock port (default: 10790)
    \\  --message TEXT      Line to send to the guest
    \\  --help            Show this help text
    \\
;

const Mode = enum {
    serve,
    ping,
    snapshot,
    probe_guest,
};

const Config = struct {
    mode: Mode,
    uds_path: []const u8 = "",
    control_uds: []const u8 = "",
    jailer_root: []const u8 = default_jailer_root,
    port: u32 = hs.default_guest_port,
    message: []const u8 = "hello from host",
};

const VMObservation = struct {
    job_id: []const u8,
    uds_path: []const u8,
    status: []const u8,
    message: []const u8,
    observed_at_unix_ms: i64,
};

pub fn main() !void {
    var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa_state.deinit();
    const gpa = gpa_state.allocator();

    const args = try std.process.argsAlloc(gpa);
    defer std.process.argsFree(gpa, args);

    const config = parseArgs(args) catch |err| switch (err) {
        error.ShowUsage => {
            try std.fs.File.stdout().writeAll(usage);
            return;
        },
        else => return err,
    };

    switch (config.mode) {
        .serve => try serve(gpa, config.control_uds, config.jailer_root, config.port),
        .ping => try ping(gpa, config.control_uds),
        .snapshot => try snapshot(gpa, config.control_uds),
        .probe_guest => try probeGuest(gpa, config.uds_path, config.port, config.message),
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
        if (std.mem.eql(u8, arg, "--uds-path")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.uds_path = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--port")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.port = try hs.parsePort(args[index]);
            continue;
        }
        if (std.mem.eql(u8, arg, "--message")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.message = args[index];
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
        .probe_guest => {
            if (config.uds_path.len == 0) {
                std.log.err("--uds-path is required", .{});
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
    if (std.mem.eql(u8, value, "probe-guest")) return .probe_guest;
    return null;
}

fn serve(allocator: std.mem.Allocator, control_uds: []const u8, jailer_root: []const u8, guest_port: u32) !void {
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
        handleControlConnection(allocator, stream, jailer_root, guest_port) catch |err| {
            std.log.err("control connection failed: {s}", .{@errorName(err)});
        };
    }
}

fn handleControlConnection(base_allocator: std.mem.Allocator, stream: std.net.Stream, jailer_root: []const u8, guest_port: u32) !void {
    defer stream.close();

    var arena_state = std.heap.ArenaAllocator.init(base_allocator);
    defer arena_state.deinit();
    const allocator = arena_state.allocator();

    const line = try hs.readLineAlloc(allocator, stream, hs.max_line_bytes);

    if (std.mem.eql(u8, line, "PING")) {
        try hs.writeLine(stream, "PONG homestead-smelter-host");
        return;
    }
    if (std.mem.eql(u8, line, "SNAPSHOT")) {
        try writeSnapshotResponse(allocator, stream, jailer_root, guest_port);
        return;
    }

    try hs.writeLine(stream, "ERR unsupported command");
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

fn probeGuest(gpa: std.mem.Allocator, uds_path: []const u8, port: u32, message: []const u8) !void {
    const response = try probeGuestResponse(gpa, uds_path, port, message);
    defer gpa.free(response);
    try std.fs.File.stdout().writeAll(response);
    try std.fs.File.stdout().writeAll("\n");
}

fn probeGuestResponse(gpa: std.mem.Allocator, uds_path: []const u8, port: u32, message: []const u8) ![]u8 {
    var address = try std.net.Address.initUnix(uds_path);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());

    const stream = std.net.Stream{ .handle = fd };
    defer stream.close();

    const bridge_cmd = try std.fmt.allocPrint(gpa, "CONNECT {d}\n", .{port});
    defer gpa.free(bridge_cmd);
    try stream.writeAll(bridge_cmd);

    const bridge_ack = try hs.readLineAlloc(gpa, stream, hs.max_line_bytes);
    defer gpa.free(bridge_ack);

    const expected_ack = try std.fmt.allocPrint(gpa, "OK {d}", .{port});
    defer gpa.free(expected_ack);
    if (!std.mem.eql(u8, bridge_ack, expected_ack)) {
        std.log.err("unexpected Firecracker bridge reply: {s}", .{bridge_ack});
        return error.InvalidBridgeReply;
    }

    try hs.writeLine(stream, message);
    return try hs.readLineAlloc(gpa, stream, hs.max_line_bytes);
}

fn writeSnapshotResponse(allocator: std.mem.Allocator, stream: std.net.Stream, jailer_root: []const u8, guest_port: u32) !void {
    const observations = try collectObservations(allocator, jailer_root, guest_port);
    const payload = try buildSnapshotJSON(allocator, jailer_root, guest_port, observations);
    try hs.writeLine(stream, payload);
}

fn collectObservations(allocator: std.mem.Allocator, jailer_root: []const u8, guest_port: u32) ![]VMObservation {
    var observations = try std.ArrayList(VMObservation).initCapacity(allocator, 4);

    var root_dir = std.fs.openDirAbsolute(jailer_root, .{ .iterate = true }) catch |err| switch (err) {
        error.FileNotFound => return observations.toOwnedSlice(allocator),
        else => return err,
    };
    defer root_dir.close();

    var it = root_dir.iterate();
    while (try it.next()) |entry| {
        if (entry.kind != .directory) continue;

        const uds_path = try std.fmt.allocPrint(allocator, "{s}/{s}/root/run/forge-control.sock", .{ jailer_root, entry.name });
        std.fs.accessAbsolute(uds_path, .{}) catch |err| switch (err) {
            error.FileNotFound => continue,
            else => return err,
        };

        const observed_at_unix_ms = std.time.milliTimestamp();
        const job_id = try allocator.dupe(u8, entry.name);
        const probe_result = probeGuestResponse(allocator, uds_path, guest_port, snapshot_probe_message) catch |err| {
            const err_message = try std.fmt.allocPrint(allocator, "{s}", .{@errorName(err)});
            try observations.append(allocator, .{
                .job_id = job_id,
                .uds_path = uds_path,
                .status = "error",
                .message = err_message,
                .observed_at_unix_ms = observed_at_unix_ms,
            });
            continue;
        };

        try observations.append(allocator, .{
            .job_id = job_id,
            .uds_path = uds_path,
            .status = "ok",
            .message = probe_result,
            .observed_at_unix_ms = observed_at_unix_ms,
        });
    }

    return observations.toOwnedSlice(allocator);
}

fn buildSnapshotJSON(allocator: std.mem.Allocator, jailer_root: []const u8, guest_port: u32, observations: []const VMObservation) ![]u8 {
    var out = try std.ArrayList(u8).initCapacity(allocator, 512);

    try out.appendSlice(allocator, "{\"schema_version\":1,\"jailer_root\":");
    try appendJSONString(&out, allocator, jailer_root);
    try out.appendSlice(allocator, ",\"guest_port\":");
    try appendInt(&out, allocator, guest_port);
    try out.appendSlice(allocator, ",\"observed_at_unix_ms\":");
    try appendInt(&out, allocator, std.time.milliTimestamp());
    try out.appendSlice(allocator, ",\"vms\":[");

    for (observations, 0..) |observation, index| {
        if (index > 0) {
            try out.appendSlice(allocator, ",");
        }
        try out.appendSlice(allocator, "{\"job_id\":");
        try appendJSONString(&out, allocator, observation.job_id);
        try out.appendSlice(allocator, ",\"uds_path\":");
        try appendJSONString(&out, allocator, observation.uds_path);
        try out.appendSlice(allocator, ",\"status\":");
        try appendJSONString(&out, allocator, observation.status);
        try out.appendSlice(allocator, ",\"message\":");
        try appendJSONString(&out, allocator, observation.message);
        try out.appendSlice(allocator, ",\"observed_at_unix_ms\":");
        try appendInt(&out, allocator, observation.observed_at_unix_ms);
        try out.appendSlice(allocator, "}");
    }

    try out.appendSlice(allocator, "]}");
    return out.toOwnedSlice(allocator);
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
