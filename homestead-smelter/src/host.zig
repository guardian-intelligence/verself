const std = @import("std");
const hs = @import("homestead_smelter");

const usage =
    \\Usage:
    \\  homestead-smelter-host serve --listen-uds PATH
    \\  homestead-smelter-host ping --control-uds PATH
    \\  homestead-smelter-host probe-guest --uds-path PATH [--port PORT] [--message TEXT]
    \\
    \\`serve` runs the long-lived host agent and exposes a local Unix socket.
    \\`ping` checks that the host agent is running.
    \\`probe-guest` connects to Firecracker's vsock Unix bridge, issues
    \\CONNECT <port>, sends one hello line to the guest agent, and prints the reply.
    \\
    \\Options:
    \\  --listen-uds PATH   Host-agent control socket path
    \\  --control-uds PATH  Host-agent control socket path
    \\  --uds-path PATH     Firecracker vsock bridge socket path
    \\  --port PORT         Guest vsock port (default: 10790)
    \\  --message TEXT      Line to send to the guest
    \\  --help            Show this help text
    \\
;

const Mode = enum {
    serve,
    ping,
    probe_guest,
};

const Config = struct {
    mode: Mode,
    uds_path: []const u8 = "",
    control_uds: []const u8 = "",
    port: u32 = hs.default_guest_port,
    message: []const u8 = "hello from host",
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
        .serve => try serve(gpa, config.control_uds),
        .ping => try ping(gpa, config.control_uds),
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
        .serve, .ping => {
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
    if (std.mem.eql(u8, value, "probe-guest")) return .probe_guest;
    return null;
}

fn serve(allocator: std.mem.Allocator, control_uds: []const u8) !void {
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
        handleControlConnection(allocator, stream) catch |err| {
            std.log.err("control connection failed: {s}", .{@errorName(err)});
        };
    }
}

fn handleControlConnection(allocator: std.mem.Allocator, stream: std.net.Stream) !void {
    defer stream.close();

    const line = try hs.readLineAlloc(allocator, stream, hs.max_line_bytes);
    defer allocator.free(line);

    if (std.mem.eql(u8, line, "PING")) {
        try hs.writeLine(stream, "PONG homestead-smelter-host");
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

fn probeGuest(gpa: std.mem.Allocator, uds_path: []const u8, port: u32, message: []const u8) !void {
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
    const response = try hs.readLineAlloc(gpa, stream, hs.max_line_bytes);
    defer gpa.free(response);
    try std.fs.File.stdout().writeAll(response);
    try std.fs.File.stdout().writeAll("\n");
}
