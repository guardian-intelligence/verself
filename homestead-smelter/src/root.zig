const builtin = @import("builtin");
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

const HeaderWire = extern struct {
    magic: u32,
    version: u16,
    kind: u16,
    seq: u32,
    flags: u32,
    mono_ns: u64,
    wall_ns: u64,

    fn init(kind: FrameKind, seq: u32, flags: u32, mono_ns: u64, wall_ns: u64) HeaderWire {
        std.debug.assert(mono_ns > 0);
        std.debug.assert(wall_ns > 0);

        return .{
            .magic = frame_magic,
            .version = frame_version,
            .kind = @intFromEnum(kind),
            .seq = seq,
            .flags = flags,
            .mono_ns = mono_ns,
            .wall_ns = wall_ns,
        };
    }

    fn decode(buf: *const [frame_size]u8) FrameError!HeaderWire {
        const bytes: [@sizeOf(HeaderWire)]u8 = buf[0..@sizeOf(HeaderWire)].*;
        const header: HeaderWire = @bitCast(bytes);
        if (header.magic != frame_magic) return error.InvalidMagic;
        if (header.version != frame_version) return error.InvalidVersion;
        _ = std.meta.intToEnum(FrameKind, header.kind) catch return error.InvalidKind;
        return header;
    }

    fn validateKind(self: HeaderWire, expected_kind: FrameKind) FrameError!void {
        if (self.kind != @intFromEnum(expected_kind)) return error.InvalidKind;
    }
};

const HelloWire = extern struct {
    header: HeaderWire,
    sample_period_ms: u32,
    guest_port: u32,
    boot_id: [36]u8,
    net_iface: [16]u8,
    block_dev: [16]u8,
    reserved: [20]u8 = [_]u8{0} ** 20,

    fn fromFrame(frame: HelloFrame) HelloWire {
        return .{
            .header = HeaderWire.init(.hello, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns),
            .sample_period_ms = frame.sample_period_ms,
            .guest_port = frame.guest_port,
            .boot_id = frame.boot_id,
            .net_iface = frame.net_iface,
            .block_dev = frame.block_dev,
        };
    }

    fn toFrame(self: HelloWire) HelloFrame {
        return .{
            .seq = self.header.seq,
            .flags = self.header.flags,
            .mono_ns = self.header.mono_ns,
            .wall_ns = self.header.wall_ns,
            .sample_period_ms = self.sample_period_ms,
            .guest_port = self.guest_port,
            .boot_id = self.boot_id,
            .net_iface = self.net_iface,
            .block_dev = self.block_dev,
        };
    }

    fn encode(self: HelloWire) [frame_size]u8 {
        return @bitCast(self);
    }

    fn decode(buf: *const [frame_size]u8) FrameError!HelloWire {
        const wire: HelloWire = @bitCast(buf.*);
        const header = try HeaderWire.decode(buf);
        try header.validateKind(.hello);
        return wire;
    }
};

const SampleWire = extern struct {
    header: HeaderWire,
    cpu_user_ticks: u64,
    cpu_system_ticks: u64,
    cpu_idle_ticks: u64,
    load1_centis: u32,
    load5_centis: u32,
    load15_centis: u32,
    procs_running: u16,
    procs_blocked: u16,
    mem_total_kb: u64,
    mem_available_kb: u64,
    io_read_bytes: u64,
    io_write_bytes: u64,
    net_rx_bytes: u64,
    net_tx_bytes: u64,
    psi_cpu_pct100: u16,
    psi_mem_pct100: u16,
    psi_io_pct100: u16,
    reserved: u16 = 0,

    fn fromFrame(frame: SampleFrame) SampleWire {
        return .{
            .header = HeaderWire.init(.sample, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns),
            .cpu_user_ticks = frame.cpu_user_ticks,
            .cpu_system_ticks = frame.cpu_system_ticks,
            .cpu_idle_ticks = frame.cpu_idle_ticks,
            .load1_centis = frame.load1_centis,
            .load5_centis = frame.load5_centis,
            .load15_centis = frame.load15_centis,
            .procs_running = frame.procs_running,
            .procs_blocked = frame.procs_blocked,
            .mem_total_kb = frame.mem_total_kb,
            .mem_available_kb = frame.mem_available_kb,
            .io_read_bytes = frame.io_read_bytes,
            .io_write_bytes = frame.io_write_bytes,
            .net_rx_bytes = frame.net_rx_bytes,
            .net_tx_bytes = frame.net_tx_bytes,
            .psi_cpu_pct100 = frame.psi_cpu_pct100,
            .psi_mem_pct100 = frame.psi_mem_pct100,
            .psi_io_pct100 = frame.psi_io_pct100,
        };
    }

    fn toFrame(self: SampleWire) SampleFrame {
        return .{
            .seq = self.header.seq,
            .flags = self.header.flags,
            .mono_ns = self.header.mono_ns,
            .wall_ns = self.header.wall_ns,
            .cpu_user_ticks = self.cpu_user_ticks,
            .cpu_system_ticks = self.cpu_system_ticks,
            .cpu_idle_ticks = self.cpu_idle_ticks,
            .load1_centis = self.load1_centis,
            .load5_centis = self.load5_centis,
            .load15_centis = self.load15_centis,
            .procs_running = self.procs_running,
            .procs_blocked = self.procs_blocked,
            .mem_total_kb = self.mem_total_kb,
            .mem_available_kb = self.mem_available_kb,
            .io_read_bytes = self.io_read_bytes,
            .io_write_bytes = self.io_write_bytes,
            .net_rx_bytes = self.net_rx_bytes,
            .net_tx_bytes = self.net_tx_bytes,
            .psi_cpu_pct100 = self.psi_cpu_pct100,
            .psi_mem_pct100 = self.psi_mem_pct100,
            .psi_io_pct100 = self.psi_io_pct100,
        };
    }

    fn encode(self: SampleWire) [frame_size]u8 {
        return @bitCast(self);
    }

    fn decode(buf: *const [frame_size]u8) FrameError!SampleWire {
        const wire: SampleWire = @bitCast(buf.*);
        const header = try HeaderWire.decode(buf);
        try header.validateKind(.sample);
        return wire;
    }
};

comptime {
    std.debug.assert(builtin.cpu.arch.endian() == .little);
    std.debug.assert(frame_size == 128);
    std.debug.assert(default_guest_port >= 1024);
    std.debug.assert(default_sample_period_ms > 0);
    std.debug.assert(max_snapshot_bytes >= max_line_bytes);
    std.debug.assert(@sizeOf(HeaderWire) == 32);
    std.debug.assert(@sizeOf(HelloWire) == frame_size);
    std.debug.assert(@sizeOf(SampleWire) == frame_size);
    std.debug.assert(@offsetOf(HelloWire, "sample_period_ms") == 32);
    std.debug.assert(@offsetOf(HelloWire, "boot_id") == 40);
    std.debug.assert(@offsetOf(SampleWire, "cpu_user_ticks") == 32);
    std.debug.assert(@offsetOf(SampleWire, "psi_cpu_pct100") == 120);
}

pub fn writeLine(stream: std.net.Stream, line: []const u8) !void {
    std.debug.assert(line.len <= max_snapshot_bytes);
    try stream.writeAll(line);
    try stream.writeAll("\n");
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
    return HelloWire.fromFrame(frame).encode();
}

pub fn decodeHelloFrame(buf: *const [frame_size]u8) FrameError!HelloFrame {
    return (try HelloWire.decode(buf)).toFrame();
}

pub fn encodeSampleFrame(frame: SampleFrame) [frame_size]u8 {
    return SampleWire.fromFrame(frame).encode();
}

pub fn decodeSampleFrame(buf: *const [frame_size]u8) FrameError!SampleFrame {
    return (try SampleWire.decode(buf)).toFrame();
}

pub fn decodeFrameKind(buf: *const [frame_size]u8) FrameError!FrameKind {
    const header = try HeaderWire.decode(buf);
    return std.meta.intToEnum(FrameKind, header.kind) catch error.InvalidKind;
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

test "decodeFrameKind rejects invalid magic" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var wire: SampleWire = @bitCast(encodeSampleFrame(frame));
    wire.header.magic = frame_magic ^ 0xffff;
    const encoded: [frame_size]u8 = @bitCast(wire);
    try std.testing.expectError(error.InvalidMagic, decodeFrameKind(&encoded));
}

test "decodeFrameKind rejects invalid version" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var wire: SampleWire = @bitCast(encodeSampleFrame(frame));
    wire.header.version = frame_version + 1;
    const encoded: [frame_size]u8 = @bitCast(wire);
    try std.testing.expectError(error.InvalidVersion, decodeFrameKind(&encoded));
}

test "decodeFrameKind rejects unknown kind" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var wire: SampleWire = @bitCast(encodeSampleFrame(frame));
    wire.header.kind = std.math.maxInt(u16);
    const encoded: [frame_size]u8 = @bitCast(wire);
    try std.testing.expectError(error.InvalidKind, decodeFrameKind(&encoded));
}

test "decodeHelloFrame rejects sample frame kind" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    const encoded = encodeSampleFrame(frame);
    try std.testing.expectError(error.InvalidKind, decodeHelloFrame(&encoded));
}

test "decodeSampleFrame rejects hello frame kind" {
    const frame = HelloFrame{
        .seq = 1,
        .mono_ns = 3,
        .wall_ns = 4,
        .sample_period_ms = default_sample_period_ms,
        .guest_port = default_guest_port,
    };
    const encoded = encodeHelloFrame(frame);
    try std.testing.expectError(error.InvalidKind, decodeSampleFrame(&encoded));
}

test "sample frame boundary values round-trip" {
    const frame = SampleFrame{
        .seq = std.math.maxInt(u32),
        .flags = std.math.maxInt(u32),
        .mono_ns = std.math.maxInt(u64),
        .wall_ns = std.math.maxInt(u64),
        .cpu_user_ticks = std.math.maxInt(u64),
        .cpu_system_ticks = std.math.maxInt(u64),
        .cpu_idle_ticks = std.math.maxInt(u64),
        .load1_centis = std.math.maxInt(u32),
        .load5_centis = std.math.maxInt(u32),
        .load15_centis = std.math.maxInt(u32),
        .procs_running = std.math.maxInt(u16),
        .procs_blocked = std.math.maxInt(u16),
        .mem_total_kb = std.math.maxInt(u64),
        .mem_available_kb = std.math.maxInt(u64),
        .io_read_bytes = std.math.maxInt(u64),
        .io_write_bytes = std.math.maxInt(u64),
        .net_rx_bytes = std.math.maxInt(u64),
        .net_tx_bytes = std.math.maxInt(u64),
        .psi_cpu_pct100 = std.math.maxInt(u16),
        .psi_mem_pct100 = std.math.maxInt(u16),
        .psi_io_pct100 = std.math.maxInt(u16),
    };
    const encoded = encodeSampleFrame(frame);
    const decoded = try decodeSampleFrame(&encoded);
    try std.testing.expectEqualDeep(frame, decoded);
}

test "setPaddedString truncates long input" {
    var dest = [_]u8{0xaa} ** 8;
    setPaddedString(dest[0..], "123456789");
    try std.testing.expectEqualStrings("12345678", dest[0..]);
}

test "setPaddedString and trimPaddedString are symmetric" {
    var dest = [_]u8{0xaa} ** 16;
    setPaddedString(dest[0..], "eth0");
    try std.testing.expectEqualStrings("eth0", trimPaddedString(dest[0..]));
    try std.testing.expectEqual(@as(u8, 0), dest[4]);
}
