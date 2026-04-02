const std = @import("std");

pub const default_guest_port: u32 = 10790;
pub const frame_magic: u32 = 0x46505600;
pub const frame_version: u16 = 1;
pub const frame_size: usize = 128;
pub const default_sample_period_ms: u32 = 500;
pub const sample_period_ns: u64 = default_sample_period_ms * std.time.ns_per_ms;
pub const max_line_bytes: usize = 4096;
pub const max_snapshot_bytes: usize = 1024 * 1024;

pub const flag_psi_cpu_missing: u32 = 1 << 0;
pub const flag_psi_mem_missing: u32 = 1 << 1;
pub const flag_psi_io_missing: u32 = 1 << 2;
pub const flag_disk_missing: u32 = 1 << 3;
pub const flag_net_missing: u32 = 1 << 4;

pub const FrameKind = enum(u16) {
    hello = 1,
    sample = 2,
};

pub const HelloFrame = struct {
    seq: u32 = 0,
    flags: u32 = 0,
    mono_ns: u64 = 0,
    wall_ns: u64 = 0,
    sample_period_ms: u32 = default_sample_period_ms,
    guest_port: u32 = default_guest_port,
    boot_id: [36]u8 = [_]u8{0} ** 36,
    net_iface: [16]u8 = [_]u8{0} ** 16,
    block_dev: [16]u8 = [_]u8{0} ** 16,
};

pub const SampleFrame = struct {
    seq: u32 = 0,
    flags: u32 = 0,
    mono_ns: u64 = 0,
    wall_ns: u64 = 0,
    cpu_user_ticks: u64 = 0,
    cpu_system_ticks: u64 = 0,
    cpu_idle_ticks: u64 = 0,
    load1_centis: u32 = 0,
    load5_centis: u32 = 0,
    load15_centis: u32 = 0,
    procs_running: u16 = 0,
    procs_blocked: u16 = 0,
    mem_total_kb: u64 = 0,
    mem_available_kb: u64 = 0,
    io_read_bytes: u64 = 0,
    io_write_bytes: u64 = 0,
    net_rx_bytes: u64 = 0,
    net_tx_bytes: u64 = 0,
    psi_cpu_pct100: u16 = 0,
    psi_mem_pct100: u16 = 0,
    psi_io_pct100: u16 = 0,
};

pub const LineError = error{
    LineTooLong,
};

pub const FrameError = error{
    InvalidMagic,
    InvalidVersion,
    InvalidKind,
};

comptime {
    std.debug.assert(frame_size == 128);
    std.debug.assert(default_guest_port >= 1024);
    std.debug.assert(default_sample_period_ms > 0);
    std.debug.assert(max_snapshot_bytes >= max_line_bytes);
    std.debug.assert(108 <= frame_size);
    std.debug.assert(126 <= frame_size);
}

pub fn writeLine(stream: std.net.Stream, line: []const u8) !void {
    std.debug.assert(line.len <= max_snapshot_bytes);
    try stream.writeAll(line);
    try stream.writeAll("\n");
}

pub fn readLineAlloc(allocator: std.mem.Allocator, stream: std.net.Stream, limit: usize) ![]u8 {
    var bytes = try std.ArrayList(u8).initCapacity(allocator, 64);
    defer bytes.deinit(allocator);

    var one: [1]u8 = undefined;
    while (true) {
        const n = try stream.read(one[0..]);
        if (n == 0) break;
        switch (one[0]) {
            '\n' => break,
            '\r' => continue,
            else => {},
        }
        if (bytes.items.len >= limit) {
            return error.LineTooLong;
        }
        try bytes.append(allocator, one[0]);
    }

    return bytes.toOwnedSlice(allocator);
}

pub fn readLineInto(stream: std.net.Stream, buf: []u8) ![]u8 {
    std.debug.assert(buf.len > 0);

    var used: usize = 0;
    var one: [1]u8 = undefined;
    while (true) {
        const n = try stream.read(one[0..]);
        if (n == 0) break;
        switch (one[0]) {
            '\n' => break,
            '\r' => continue,
            else => {},
        }
        if (used >= buf.len) return error.LineTooLong;
        buf[used] = one[0];
        used += 1;
    }
    return buf[0..used];
}

pub fn parsePort(value: []const u8) !u32 {
    return std.fmt.parseInt(u32, value, 10);
}

pub fn trimPaddedString(value: []const u8) []const u8 {
    return value[0 .. std.mem.indexOfScalar(u8, value, 0) orelse value.len];
}

pub fn setPaddedString(dest: []u8, value: []const u8) void {
    std.debug.assert(dest.len > 0);

    @memset(dest, 0);
    const count = @min(dest.len, value.len);
    @memcpy(dest[0..count], value[0..count]);
}

pub fn encodeHelloFrame(frame: HelloFrame) [frame_size]u8 {
    std.debug.assert(frame.sample_period_ms > 0);
    std.debug.assert(frame.guest_port > 0);

    var buf = [_]u8{0} ** frame_size;
    encodeHeader(&buf, .hello, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns);
    writeU32(&buf, 32, frame.sample_period_ms);
    writeU32(&buf, 36, frame.guest_port);
    @memcpy(buf[40..76], frame.boot_id[0..]);
    @memcpy(buf[76..92], frame.net_iface[0..]);
    @memcpy(buf[92..108], frame.block_dev[0..]);
    return buf;
}

pub fn decodeHelloFrame(buf: *const [frame_size]u8) FrameError!HelloFrame {
    try validateHeader(buf, .hello);
    var frame = HelloFrame{
        .seq = readU32(buf, 8),
        .flags = readU32(buf, 12),
        .mono_ns = readU64(buf, 16),
        .wall_ns = readU64(buf, 24),
        .sample_period_ms = readU32(buf, 32),
        .guest_port = readU32(buf, 36),
    };
    @memcpy(frame.boot_id[0..], buf[40..76]);
    @memcpy(frame.net_iface[0..], buf[76..92]);
    @memcpy(frame.block_dev[0..], buf[92..108]);
    return frame;
}

pub fn encodeSampleFrame(frame: SampleFrame) [frame_size]u8 {
    var buf = [_]u8{0} ** frame_size;
    encodeHeader(&buf, .sample, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns);
    writeU64(&buf, 32, frame.cpu_user_ticks);
    writeU64(&buf, 40, frame.cpu_system_ticks);
    writeU64(&buf, 48, frame.cpu_idle_ticks);
    writeU32(&buf, 56, frame.load1_centis);
    writeU32(&buf, 60, frame.load5_centis);
    writeU32(&buf, 64, frame.load15_centis);
    writeU16(&buf, 68, frame.procs_running);
    writeU16(&buf, 70, frame.procs_blocked);
    writeU64(&buf, 72, frame.mem_total_kb);
    writeU64(&buf, 80, frame.mem_available_kb);
    writeU64(&buf, 88, frame.io_read_bytes);
    writeU64(&buf, 96, frame.io_write_bytes);
    writeU64(&buf, 104, frame.net_rx_bytes);
    writeU64(&buf, 112, frame.net_tx_bytes);
    writeU16(&buf, 120, frame.psi_cpu_pct100);
    writeU16(&buf, 122, frame.psi_mem_pct100);
    writeU16(&buf, 124, frame.psi_io_pct100);
    return buf;
}

pub fn decodeSampleFrame(buf: *const [frame_size]u8) FrameError!SampleFrame {
    try validateHeader(buf, .sample);
    return SampleFrame{
        .seq = readU32(buf, 8),
        .flags = readU32(buf, 12),
        .mono_ns = readU64(buf, 16),
        .wall_ns = readU64(buf, 24),
        .cpu_user_ticks = readU64(buf, 32),
        .cpu_system_ticks = readU64(buf, 40),
        .cpu_idle_ticks = readU64(buf, 48),
        .load1_centis = readU32(buf, 56),
        .load5_centis = readU32(buf, 60),
        .load15_centis = readU32(buf, 64),
        .procs_running = readU16(buf, 68),
        .procs_blocked = readU16(buf, 70),
        .mem_total_kb = readU64(buf, 72),
        .mem_available_kb = readU64(buf, 80),
        .io_read_bytes = readU64(buf, 88),
        .io_write_bytes = readU64(buf, 96),
        .net_rx_bytes = readU64(buf, 104),
        .net_tx_bytes = readU64(buf, 112),
        .psi_cpu_pct100 = readU16(buf, 120),
        .psi_mem_pct100 = readU16(buf, 122),
        .psi_io_pct100 = readU16(buf, 124),
    };
}

pub fn decodeFrameKind(buf: *const [frame_size]u8) FrameError!FrameKind {
    if (readU32(buf, 0) != frame_magic) return error.InvalidMagic;
    if (readU16(buf, 4) != frame_version) return error.InvalidVersion;
    return std.meta.intToEnum(FrameKind, readU16(buf, 6)) catch error.InvalidKind;
}

pub fn monotonicNowNs() !u64 {
    return timespecToNs(try std.posix.clock_gettime(.MONOTONIC));
}

pub fn realtimeNowNs() !u64 {
    return timespecToNs(try std.posix.clock_gettime(.REALTIME));
}

pub fn sleepToNextTick(loop_started_ns: u64) !void {
    const now_ns = try monotonicNowNs();
    if (now_ns < loop_started_ns) return;
    const elapsed_ns = now_ns - loop_started_ns;
    if (elapsed_ns >= sample_period_ns) return;
    std.Thread.sleep(sample_period_ns - elapsed_ns);
}

fn encodeHeader(buf: *[frame_size]u8, kind: FrameKind, seq: u32, flags: u32, mono_ns: u64, wall_ns: u64) void {
    std.debug.assert(mono_ns > 0);
    std.debug.assert(wall_ns > 0);

    writeU32(buf, 0, frame_magic);
    writeU16(buf, 4, frame_version);
    writeU16(buf, 6, @intFromEnum(kind));
    writeU32(buf, 8, seq);
    writeU32(buf, 12, flags);
    writeU64(buf, 16, mono_ns);
    writeU64(buf, 24, wall_ns);
}

fn validateHeader(buf: *const [frame_size]u8, expected_kind: FrameKind) FrameError!void {
    if (readU32(buf, 0) != frame_magic) return error.InvalidMagic;
    if (readU16(buf, 4) != frame_version) return error.InvalidVersion;
    if (readU16(buf, 6) != @intFromEnum(expected_kind)) return error.InvalidKind;
}

fn writeU16(buf: *[frame_size]u8, offset: usize, value: u16) void {
    std.mem.writeInt(u16, buf[offset..][0..2], value, .little);
}

fn writeU32(buf: *[frame_size]u8, offset: usize, value: u32) void {
    std.mem.writeInt(u32, buf[offset..][0..4], value, .little);
}

fn writeU64(buf: *[frame_size]u8, offset: usize, value: u64) void {
    std.mem.writeInt(u64, buf[offset..][0..8], value, .little);
}

fn readU16(buf: *const [frame_size]u8, offset: usize) u16 {
    return std.mem.readInt(u16, buf[offset..][0..2], .little);
}

fn readU32(buf: *const [frame_size]u8, offset: usize) u32 {
    return std.mem.readInt(u32, buf[offset..][0..4], .little);
}

fn readU64(buf: *const [frame_size]u8, offset: usize) u64 {
    return std.mem.readInt(u64, buf[offset..][0..8], .little);
}

fn timespecToNs(ts: std.posix.timespec) u64 {
    std.debug.assert(ts.sec >= 0);
    std.debug.assert(ts.nsec >= 0);
    return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
}

test "parsePort parses decimal port strings" {
    try std.testing.expectEqual(@as(u32, 10790), try parsePort("10790"));
}

test "sample frame round-trips" {
    const frame = SampleFrame{
        .seq = 7,
        .flags = flag_net_missing,
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
        .mem_total_kb = 111,
        .mem_available_kb = 222,
        .io_read_bytes = 333,
        .io_write_bytes = 444,
        .net_rx_bytes = 555,
        .net_tx_bytes = 666,
        .psi_cpu_pct100 = 77,
        .psi_mem_pct100 = 88,
        .psi_io_pct100 = 99,
    };
    const encoded = encodeSampleFrame(frame);
    const decoded = try decodeSampleFrame(&encoded);
    try std.testing.expectEqualDeep(frame, decoded);
}

test "hello frame round-trips" {
    var frame = HelloFrame{
        .seq = 1,
        .flags = 2,
        .mono_ns = 3,
        .wall_ns = 4,
        .sample_period_ms = default_sample_period_ms,
        .guest_port = default_guest_port,
    };
    setPaddedString(frame.boot_id[0..], "boot-id");
    setPaddedString(frame.net_iface[0..], "eth0");
    setPaddedString(frame.block_dev[0..], "vda");
    const encoded = encodeHelloFrame(frame);
    const decoded = try decodeHelloFrame(&encoded);
    try std.testing.expectEqual(frame.seq, decoded.seq);
    try std.testing.expectEqualStrings("boot-id", trimPaddedString(decoded.boot_id[0..]));
    try std.testing.expectEqualStrings("eth0", trimPaddedString(decoded.net_iface[0..]));
    try std.testing.expectEqualStrings("vda", trimPaddedString(decoded.block_dev[0..]));
}
