const std = @import("std");
const hs = @import("homestead_smelter");
const linux = std.os.linux;

const vmaddr_cid_any = std.math.maxInt(u32);

const usage =
    \\Usage: homestead-smelter-guest [--port PORT]
    \\
    \\Listen on an AF_VSOCK port inside a Firecracker guest and reply to one-line
    \\hello messages from the host over Firecracker's Unix-domain bridge.
    \\
    \\Options:
    \\  --port PORT   Guest vsock port (default: 10790)
    \\  --help        Show this help text
    \\
;

pub fn main() !void {
    var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa_state.deinit();
    const gpa = gpa_state.allocator();

    const port = parseArgs(gpa) catch |err| switch (err) {
        error.ShowUsage => {
            try std.fs.File.stdout().writeAll(usage);
            return;
        },
        else => return err,
    };

    const fd = try std.posix.socket(linux.AF.VSOCK, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    defer std.posix.close(fd);

    var address = linux.sockaddr.vm{
        .port = port,
        .cid = vmaddr_cid_any,
        .flags = 0,
    };
    try std.posix.bind(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
    try std.posix.listen(fd, 8);

    std.log.info("homestead-smelter guest listening on vsock port {d}", .{port});

    while (true) {
        const conn_fd = std.posix.accept(fd, null, null, std.posix.SOCK.CLOEXEC) catch |err| {
            std.log.err("accept failed: {s}", .{@errorName(err)});
            continue;
        };
        const stream = std.net.Stream{ .handle = conn_fd };
        handleConnection(gpa, stream, port) catch |err| {
            std.log.err("connection failed: {s}", .{@errorName(err)});
        };
    }
}

fn handleConnection(allocator: std.mem.Allocator, stream: std.net.Stream, port: u32) !void {
    defer stream.close();

    const request = try hs.readLineAlloc(allocator, stream, hs.max_line_bytes);
    defer allocator.free(request);

    std.log.info("received host line: {s}", .{request});

    const response = try std.fmt.allocPrint(
        allocator,
        "hello from homestead-smelter guest on port {d}: received \"{s}\"",
        .{ port, request },
    );
    defer allocator.free(response);

    try hs.writeLine(stream, response);
}

fn parseArgs(allocator: std.mem.Allocator) !u32 {
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    var port = hs.default_guest_port;

    var index: usize = 1;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--help")) {
            return error.ShowUsage;
        }
        if (std.mem.eql(u8, arg, "--port")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            port = try hs.parsePort(args[index]);
            continue;
        }

        std.log.err("unknown argument: {s}", .{arg});
        return error.ShowUsage;
    }

    return port;
}
