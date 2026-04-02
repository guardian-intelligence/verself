const std = @import("std");
const hs = @import("homestead_smelter");
const linux = std.os.linux;

const vmaddr_cid_any = std.math.maxInt(u32);
const guest_net_iface = "eth0";
const guest_block_dev = "vda";

const usage =
    \\Usage: homestead-smelter-guest [--port PORT]
    \\
    \\Listen on an AF_VSOCK port inside a Firecracker guest and stream fixed-size
    \\diagnostic frames to the host over Firecracker's Unix-domain vsock bridge.
    \\
    \\Options:
    \\  --port PORT   Guest vsock port (default: 10790)
    \\  --help        Show this help text
    \\
;

const VsockAddress = struct {
    port: u32,
    cid: u32,

    fn bind(self: VsockAddress, fd: std.posix.fd_t) !void {
        std.debug.assert(self.port > 0);

        var address = linux.sockaddr.vm{
            .port = self.port,
            .cid = self.cid,
            .flags = 0,
        };
        try std.posix.bind(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
    }
};

pub fn main() !void {
    const port = parseArgs(std.heap.page_allocator) catch |err| switch (err) {
        error.ShowUsage => {
            try std.fs.File.stdout().writeAll(usage);
            return;
        },
        else => return err,
    };

    const fd = try std.posix.socket(linux.AF.VSOCK, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    defer std.posix.close(fd);

    const address = VsockAddress{
        .port = port,
        .cid = vmaddr_cid_any,
    };
    try address.bind(fd);
    try std.posix.listen(fd, 8);

    std.log.info("homestead-smelter guest listening on vsock port {d}", .{port});

    while (true) {
        const conn_fd = std.posix.accept(fd, null, null, std.posix.SOCK.CLOEXEC) catch |err| {
            std.log.err("accept failed: {s}", .{@errorName(err)});
            continue;
        };
        const stream = std.net.Stream{ .handle = conn_fd };
        handleConnection(stream, port) catch |err| {
            std.log.err("guest stream failed: {s}", .{@errorName(err)});
        };
    }
}

fn handleConnection(stream: std.net.Stream, port: u32) !void {
    defer stream.close();
    std.debug.assert(port > 0);

    const hello = try collectHelloFrame(port);
    var hello_bytes = hs.encodeHelloFrame(hello);
    try stream.writeAll(hello_bytes[0..]);

    var seq = hello.seq + 1;
    while (true) {
        const loop_started_ns = try hs.monotonicNowNs();
        const sample = try collectSampleFrame(seq);
        var sample_bytes = hs.encodeSampleFrame(sample);
        try stream.writeAll(sample_bytes[0..]);
        seq += 1;
        try hs.sleepToNextTick(loop_started_ns);
    }
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

fn collectHelloFrame(port: u32) !hs.HelloFrame {
    var frame = hs.HelloFrame{
        .seq = 0,
        .mono_ns = try hs.monotonicNowNs(),
        .wall_ns = try hs.realtimeNowNs(),
        .sample_period_ms = hs.default_sample_period_ms,
        .guest_port = port,
    };

    const boot_id = try readBootID();
    hs.setPaddedString(frame.boot_id[0..], boot_id);
    hs.setPaddedString(frame.net_iface[0..], guest_net_iface);
    hs.setPaddedString(frame.block_dev[0..], guest_block_dev);
    return frame;
}

fn collectSampleFrame(seq: u32) !hs.SampleFrame {
    var frame = hs.SampleFrame{
        .seq = seq,
        .mono_ns = try hs.monotonicNowNs(),
        .wall_ns = try hs.realtimeNowNs(),
    };

    try parseProcStat(&frame);
    try parseProcLoadavg(&frame);
    try parseProcMeminfo(&frame);

    parseProcDiskstats(&frame) catch |err| switch (err) {
        error.FileNotFound, error.DeviceNotFound => frame.flags |= hs.flag_disk_missing,
        else => return err,
    };

    parseProcNetDev(&frame) catch |err| switch (err) {
        error.FileNotFound, error.InterfaceNotFound => frame.flags |= hs.flag_net_missing,
        else => return err,
    };

    frame.psi_cpu_pct100 = parsePressurePct100("/proc/pressure/cpu") catch |err| switch (err) {
        error.FileNotFound => blk: {
            frame.flags |= hs.flag_psi_cpu_missing;
            break :blk 0;
        },
        else => return err,
    };
    frame.psi_mem_pct100 = parsePressurePct100("/proc/pressure/memory") catch |err| switch (err) {
        error.FileNotFound => blk: {
            frame.flags |= hs.flag_psi_mem_missing;
            break :blk 0;
        },
        else => return err,
    };
    frame.psi_io_pct100 = parsePressurePct100("/proc/pressure/io") catch |err| switch (err) {
        error.FileNotFound => blk: {
            frame.flags |= hs.flag_psi_io_missing;
            break :blk 0;
        },
        else => return err,
    };

    return frame;
}

fn readBootID() ![]const u8 {
    var buf: [64]u8 = undefined;
    const contents = try readFile("/proc/sys/kernel/random/boot_id", buf[0..]);
    return parseBootIDContents(contents);
}

fn parseProcStat(frame: *hs.SampleFrame) !void {
    var buf: [4096]u8 = undefined;
    const contents = try readFile("/proc/stat", buf[0..]);
    return parseProcStatContents(frame, contents);
}

fn parseProcStatContents(frame: *hs.SampleFrame, contents: []const u8) !void {
    var found_cpu = false;
    var found_running = false;
    var found_blocked = false;

    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        if (std.mem.startsWith(u8, line, "cpu ")) {
            var fields = std.mem.tokenizeAny(u8, line, " \t");
            _ = fields.next();
            const user = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const nice = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const system = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const idle = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const iowait = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const irq = try parseU64(fields.next() orelse return error.InvalidProcStat);
            const softirq = try parseU64(fields.next() orelse return error.InvalidProcStat);

            frame.cpu_user_ticks = user + nice;
            frame.cpu_system_ticks = system + irq + softirq;
            frame.cpu_idle_ticks = idle + iowait;
            found_cpu = true;
            continue;
        }
        if (std.mem.startsWith(u8, line, "procs_running ")) {
            const value = std.mem.trimLeft(u8, line["procs_running".len..], " \t");
            frame.procs_running = saturatingCastU16(try parseU64(value));
            found_running = true;
            continue;
        }
        if (std.mem.startsWith(u8, line, "procs_blocked ")) {
            const value = std.mem.trimLeft(u8, line["procs_blocked".len..], " \t");
            frame.procs_blocked = saturatingCastU16(try parseU64(value));
            found_blocked = true;
        }
    }

    if (!found_cpu or !found_running or !found_blocked) {
        return error.InvalidProcStat;
    }
}

fn parseProcLoadavg(frame: *hs.SampleFrame) !void {
    var buf: [256]u8 = undefined;
    const contents = try readFile("/proc/loadavg", buf[0..]);
    return parseProcLoadavgContents(frame, contents);
}

fn parseProcLoadavgContents(frame: *hs.SampleFrame, contents: []const u8) !void {
    var fields = std.mem.tokenizeAny(u8, std.mem.trim(u8, contents, " \n\r\t"), " \t");
    frame.load1_centis = try parseScaledDecimalU32(fields.next() orelse return error.InvalidLoadavg, 100);
    frame.load5_centis = try parseScaledDecimalU32(fields.next() orelse return error.InvalidLoadavg, 100);
    frame.load15_centis = try parseScaledDecimalU32(fields.next() orelse return error.InvalidLoadavg, 100);
}

fn parseProcMeminfo(frame: *hs.SampleFrame) !void {
    var buf: [4096]u8 = undefined;
    const contents = try readFile("/proc/meminfo", buf[0..]);
    return parseProcMeminfoContents(frame, contents);
}

fn parseProcMeminfoContents(frame: *hs.SampleFrame, contents: []const u8) !void {
    var found_total = false;
    var found_available = false;

    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        if (std.mem.startsWith(u8, line, "MemTotal:")) {
            frame.mem_total_kb = try parseMeminfoValue(line);
            found_total = true;
            continue;
        }
        if (std.mem.startsWith(u8, line, "MemAvailable:")) {
            frame.mem_available_kb = try parseMeminfoValue(line);
            found_available = true;
        }
    }

    if (!found_total or !found_available) {
        return error.InvalidMeminfo;
    }
}

fn parseProcDiskstats(frame: *hs.SampleFrame) !void {
    var buf: [8192]u8 = undefined;
    const contents = try readFile("/proc/diskstats", buf[0..]);
    return parseProcDiskstatsContents(frame, contents);
}

fn parseProcDiskstatsContents(frame: *hs.SampleFrame, contents: []const u8) !void {
    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        var fields = std.mem.tokenizeAny(u8, line, " \t");
        _ = fields.next() orelse continue;
        _ = fields.next() orelse continue;
        const name = fields.next() orelse continue;
        if (!std.mem.eql(u8, name, guest_block_dev)) continue;

        _ = fields.next() orelse return error.InvalidDiskstats;
        _ = fields.next() orelse return error.InvalidDiskstats;
        const sectors_read = try parseU64(fields.next() orelse return error.InvalidDiskstats);
        _ = fields.next() orelse return error.InvalidDiskstats;
        _ = fields.next() orelse return error.InvalidDiskstats;
        _ = fields.next() orelse return error.InvalidDiskstats;
        const sectors_written = try parseU64(fields.next() orelse return error.InvalidDiskstats);

        frame.io_read_bytes = sectors_read * 512;
        frame.io_write_bytes = sectors_written * 512;
        return;
    }

    return error.DeviceNotFound;
}

fn parseProcNetDev(frame: *hs.SampleFrame) !void {
    var buf: [4096]u8 = undefined;
    const contents = try readFile("/proc/net/dev", buf[0..]);
    return parseProcNetDevContents(frame, contents);
}

fn parseProcNetDevContents(frame: *hs.SampleFrame, contents: []const u8) !void {
    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        const trimmed = std.mem.trimLeft(u8, line, " \t");
        const colon = std.mem.indexOfScalar(u8, trimmed, ':') orelse continue;
        const iface = std.mem.trim(u8, trimmed[0..colon], " \t");
        if (!std.mem.eql(u8, iface, guest_net_iface)) continue;

        var fields = std.mem.tokenizeAny(u8, trimmed[colon + 1 ..], " \t");
        frame.net_rx_bytes = try parseU64(fields.next() orelse return error.InvalidNetDev);
        var skip: usize = 0;
        while (skip < 7) : (skip += 1) {
            _ = fields.next() orelse return error.InvalidNetDev;
        }
        frame.net_tx_bytes = try parseU64(fields.next() orelse return error.InvalidNetDev);
        return;
    }

    return error.InterfaceNotFound;
}

fn parsePressurePct100(path: []const u8) !u16 {
    var buf: [256]u8 = undefined;
    const contents = try readFile(path, buf[0..]);
    return parsePressurePct100Contents(contents);
}

fn parsePressurePct100Contents(contents: []const u8) !u16 {
    var lines = std.mem.tokenizeScalar(u8, contents, '\n');
    while (lines.next()) |line| {
        if (!std.mem.startsWith(u8, line, "some ")) continue;
        var fields = std.mem.tokenizeAny(u8, line, " \t");
        _ = fields.next();
        while (fields.next()) |field| {
            if (!std.mem.startsWith(u8, field, "avg10=")) continue;
            return try parseScaledDecimalU16(field["avg10=".len..], 100);
        }
    }

    return error.InvalidPressureFile;
}

fn parseBootIDContents(contents: []const u8) []const u8 {
    return std.mem.trim(u8, contents, " \n\r\t");
}

fn parseMeminfoValue(line: []const u8) !u64 {
    var fields = std.mem.tokenizeAny(u8, line, " \t");
    _ = fields.next();
    return try parseU64(fields.next() orelse return error.InvalidMeminfo);
}

fn parseScaledDecimalU16(text: []const u8, scale: u32) !u16 {
    const value = try parseScaledDecimalU32(text, scale);
    return saturatingCastU16(value);
}

fn parseScaledDecimalU32(text: []const u8, scale: u32) !u32 {
    const value = try std.fmt.parseFloat(f64, text);
    const scaled = value * @as(f64, @floatFromInt(scale));
    if (scaled <= 0) return 0;
    return std.math.cast(u32, @as(u64, @intFromFloat(@round(scaled)))) orelse std.math.maxInt(u32);
}

fn parseU64(text: []const u8) !u64 {
    return std.fmt.parseInt(u64, std.mem.trim(u8, text, " \t"), 10);
}

fn readFile(path: []const u8, buf: []u8) ![]const u8 {
    const file = try std.fs.openFileAbsolute(path, .{});
    defer file.close();

    const n = try file.readAll(buf);
    if (n == buf.len) {
        var extra: [1]u8 = undefined;
        if ((try file.read(extra[0..])) != 0) {
            return error.BufferTooSmall;
        }
    }
    return buf[0..n];
}

fn saturatingCastU16(value: anytype) u16 {
    const max = std.math.maxInt(u16);
    if (value > max) return max;
    return @as(u16, @intCast(value));
}

test "parseBootIDContents trims whitespace" {
    try std.testing.expectEqualStrings(
        "7a3153b0-66fb-4e87-96fa-2adadf20a74f",
        parseBootIDContents(" 7a3153b0-66fb-4e87-96fa-2adadf20a74f\n"),
    );
}

test "parseProcStatContents parses cpu ticks and process counts" {
    const fixture =
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

    var frame = hs.SampleFrame{};
    try parseProcStatContents(&frame, fixture);
    try std.testing.expectEqual(@as(u64, 4288), frame.cpu_user_ticks);
    try std.testing.expectEqual(@as(u64, 1996), frame.cpu_system_ticks);
    try std.testing.expectEqual(@as(u64, 239091), frame.cpu_idle_ticks);
    try std.testing.expectEqual(@as(u16, 3), frame.procs_running);
    try std.testing.expectEqual(@as(u16, 0), frame.procs_blocked);
}

test "parseProcStatContents rejects missing cpu line" {
    const fixture =
        \\procs_running 1
        \\procs_blocked 0
    ;

    var frame = hs.SampleFrame{};
    try std.testing.expectError(error.InvalidProcStat, parseProcStatContents(&frame, fixture));
}

test "parseProcStatContents rejects truncated cpu line" {
    var frame = hs.SampleFrame{};
    try std.testing.expectError(error.InvalidProcStat, parseProcStatContents(&frame, "cpu 1234 56\n"));
}

test "parseProcLoadavgContents parses scaled load averages" {
    var frame = hs.SampleFrame{};
    try parseProcLoadavgContents(&frame, "0.47 0.21 0.08 1/142 3578\n");
    try std.testing.expectEqual(@as(u32, 47), frame.load1_centis);
    try std.testing.expectEqual(@as(u32, 21), frame.load5_centis);
    try std.testing.expectEqual(@as(u32, 8), frame.load15_centis);
}

test "parseProcMeminfoContents parses total and available memory" {
    const fixture =
        \\MemTotal:        2000000 kB
        \\MemFree:          120000 kB
        \\MemAvailable:     800000 kB
        \\Buffers:           64000 kB
        \\Cached:           256000 kB
    ;

    var frame = hs.SampleFrame{};
    try parseProcMeminfoContents(&frame, fixture);
    try std.testing.expectEqual(@as(u64, 2000000), frame.mem_total_kb);
    try std.testing.expectEqual(@as(u64, 800000), frame.mem_available_kb);
}

test "parseProcMeminfoContents rejects missing MemAvailable" {
    const fixture =
        \\MemTotal:        2000000 kB
        \\MemFree:          120000 kB
    ;

    var frame = hs.SampleFrame{};
    try std.testing.expectError(error.InvalidMeminfo, parseProcMeminfoContents(&frame, fixture));
}

test "parseProcDiskstatsContents parses exact vda device" {
    const fixture =
        \\   7       0 loop0 1 0 8 0 1 0 8 0 0 0 0 0 0 0 0 0
        \\ 253       0 vda 100 0 200 0 300 0 400 0 0 0 0 0 0 0 0 0
        \\ 253       1 vda1 1 0 2 0 3 0 4 0 0 0 0 0 0 0 0 0
    ;

    var frame = hs.SampleFrame{};
    try parseProcDiskstatsContents(&frame, fixture);
    try std.testing.expectEqual(@as(u64, 200 * 512), frame.io_read_bytes);
    try std.testing.expectEqual(@as(u64, 400 * 512), frame.io_write_bytes);
}

test "parseProcDiskstatsContents rejects missing vda device" {
    const fixture =
        \\   8       0 sda 100 0 200 0 300 0 400 0 0 0 0 0 0 0 0 0
        \\   8       1 sda1 1 0 2 0 3 0 4 0 0 0 0 0 0 0 0 0
    ;

    var frame = hs.SampleFrame{};
    try std.testing.expectError(error.DeviceNotFound, parseProcDiskstatsContents(&frame, fixture));
}

test "parseProcNetDevContents parses eth0 traffic" {
    const fixture =
        \\Inter-|   Receive                                                |  Transmit
        \\ face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
        \\    lo: 111 1 0 0 0 0 0 0 111 1 0 0 0 0 0 0
        \\  eth0: 12345 10 0 0 0 0 0 0 67890 20 0 0 0 0 0 0
    ;

    var frame = hs.SampleFrame{};
    try parseProcNetDevContents(&frame, fixture);
    try std.testing.expectEqual(@as(u64, 12345), frame.net_rx_bytes);
    try std.testing.expectEqual(@as(u64, 67890), frame.net_tx_bytes);
}

test "parseProcNetDevContents rejects missing eth0" {
    const fixture =
        \\Inter-|   Receive                                                |  Transmit
        \\ face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
        \\    lo: 111 1 0 0 0 0 0 0 111 1 0 0 0 0 0 0
    ;

    var frame = hs.SampleFrame{};
    try std.testing.expectError(error.InterfaceNotFound, parseProcNetDevContents(&frame, fixture));
}

test "parsePressurePct100Contents parses avg10 from some line" {
    const fixture =
        \\some avg10=2.50 avg60=1.00 avg300=0.50 total=12345
        \\full avg10=9.99 avg60=9.99 avg300=9.99 total=54321
    ;

    try std.testing.expectEqual(@as(u16, 250), try parsePressurePct100Contents(fixture));
}

test "parsePressurePct100Contents rejects empty file" {
    try std.testing.expectError(error.InvalidPressureFile, parsePressurePct100Contents(""));
}

test "parseScaledDecimalU32 handles zero" {
    try std.testing.expectEqual(@as(u32, 0), try parseScaledDecimalU32("0.00", 100));
}

test "parseScaledDecimalU32 handles normal values" {
    try std.testing.expectEqual(@as(u32, 10000), try parseScaledDecimalU32("100.00", 100));
}

test "parseScaledDecimalU32 saturates large values" {
    try std.testing.expectEqual(
        std.math.maxInt(u32),
        try parseScaledDecimalU32("50000000.00", 100),
    );
}

test "saturatingCastU16 clamps values above max" {
    try std.testing.expectEqual(std.math.maxInt(u16), saturatingCastU16(@as(u32, 70000)));
}

test "saturatingCastU16 preserves zero" {
    try std.testing.expectEqual(@as(u16, 0), saturatingCastU16(@as(u32, 0)));
}
