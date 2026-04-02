const std = @import("std");

pub const default_guest_port: u32 = 10790;
pub const frame_magic: u32 = 0x46505600;
pub const frame_version: u16 = 1;
pub const frame_size: usize = 128;
pub const guest_sample_rate_hz: u32 = 60;
pub const sample_period_ns: u64 = std.time.ns_per_s / guest_sample_rate_hz;
pub const max_line_bytes: usize = 4096;

pub const flag_psi_cpu_missing: u32 = 1 << 0;
pub const flag_psi_mem_missing: u32 = 1 << 1;
pub const flag_psi_io_missing: u32 = 1 << 2;
pub const flag_disk_missing: u32 = 1 << 3;
pub const flag_net_missing: u32 = 1 << 4;

pub const FrameKind = enum(u16) {
    hello = 1,
    sample = 2,
};

/// Guest hello frame.
///
/// All fields are primary observations from the guest. All integers are encoded
/// little-endian on the wire.
pub const HelloFrame = struct {
    /// Monotonic per-stream frame sequence. `hello` MUST use `0`.
    seq: u32 = 0,
    /// Guest-side missing-data bitset. `hello` currently emits `0`.
    flags: u32 = 0,
    /// Guest monotonic clock at emit time, in nanoseconds.
    mono_ns: u64 = 0,
    /// Guest realtime clock at emit time, in nanoseconds since the Unix epoch.
    wall_ns: u64 = 0,
    /// Guest boot identity as raw UUID bytes.
    boot_id: [16]u8 = [_]u8{0} ** 16,
    /// Total memory visible inside the guest, in KiB. Boot-static.
    mem_total_kb: u64 = 0,
};

/// Guest sample frame.
///
/// Counter fields are monotonic within a boot and reset on reboot. Gauge fields
/// describe the guest state at the frame timestamp.
pub const SampleFrame = struct {
    /// Monotonic per-stream frame sequence. Samples start at `1`.
    seq: u32 = 0,
    /// Guest-side missing-data bitset.
    flags: u32 = 0,
    /// Guest monotonic clock at emit time, in nanoseconds.
    mono_ns: u64 = 0,
    /// Guest realtime clock at emit time, in nanoseconds since the Unix epoch.
    wall_ns: u64 = 0,
    /// User plus nice CPU time from `/proc/stat`, in kernel ticks.
    cpu_user_ticks: u64 = 0,
    /// System plus irq plus softirq CPU time from `/proc/stat`, in kernel ticks.
    cpu_system_ticks: u64 = 0,
    /// Idle plus iowait CPU time from `/proc/stat`, in kernel ticks.
    cpu_idle_ticks: u64 = 0,
    /// 1-minute load average scaled by 100.
    load1_centis: u32 = 0,
    /// 5-minute load average scaled by 100.
    load5_centis: u32 = 0,
    /// 15-minute load average scaled by 100.
    load15_centis: u32 = 0,
    /// Runnable task count from `/proc/stat`.
    procs_running: u16 = 0,
    /// Blocked task count from `/proc/stat`.
    procs_blocked: u16 = 0,
    /// Available memory from `/proc/meminfo`, in KiB.
    mem_available_kb: u64 = 0,
    /// Block read bytes for the guest root device, monotonic within a boot.
    io_read_bytes: u64 = 0,
    /// Block write bytes for the guest root device, monotonic within a boot.
    io_write_bytes: u64 = 0,
    /// Network receive bytes for the primary guest interface, monotonic within a boot.
    net_rx_bytes: u64 = 0,
    /// Network transmit bytes for the primary guest interface, monotonic within a boot.
    net_tx_bytes: u64 = 0,
    /// CPU pressure `avg10` scaled by 100.
    psi_cpu_pct100: u16 = 0,
    /// Memory pressure `avg10` scaled by 100.
    psi_mem_pct100: u16 = 0,
    /// IO pressure `avg10` scaled by 100.
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

const header_magic_offset: usize = 0;
const header_version_offset: usize = 4;
const header_kind_offset: usize = 6;
const header_seq_offset: usize = 8;
const header_flags_offset: usize = 12;
const header_mono_offset: usize = 16;
const header_wall_offset: usize = 24;
const payload_offset: usize = 32;

const hello_boot_id_offset: usize = payload_offset;
const hello_mem_total_offset: usize = payload_offset + 16;

const sample_cpu_user_offset: usize = payload_offset;
const sample_cpu_system_offset: usize = payload_offset + 8;
const sample_cpu_idle_offset: usize = payload_offset + 16;
const sample_load1_offset: usize = payload_offset + 24;
const sample_load5_offset: usize = payload_offset + 28;
const sample_load15_offset: usize = payload_offset + 32;
const sample_procs_running_offset: usize = payload_offset + 36;
const sample_procs_blocked_offset: usize = payload_offset + 38;
const sample_mem_available_offset: usize = payload_offset + 40;
const sample_io_read_offset: usize = payload_offset + 48;
const sample_io_write_offset: usize = payload_offset + 56;
const sample_net_rx_offset: usize = payload_offset + 64;
const sample_net_tx_offset: usize = payload_offset + 72;
const sample_psi_cpu_offset: usize = payload_offset + 80;
const sample_psi_mem_offset: usize = payload_offset + 82;
const sample_psi_io_offset: usize = payload_offset + 84;

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

pub fn encodeHelloFrame(frame: HelloFrame) [frame_size]u8 {
    std.debug.assert(frame.mono_ns > 0);
    std.debug.assert(frame.wall_ns > 0);
    std.debug.assert(frame.mem_total_kb > 0);

    var buf = [_]u8{0} ** frame_size;
    encodeHeader(&buf, .hello, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns);
    @memcpy(buf[hello_boot_id_offset .. hello_boot_id_offset + 16], &frame.boot_id);
    writeInt(u64, &buf, hello_mem_total_offset, frame.mem_total_kb);
    return buf;
}

pub fn decodeHelloFrame(buf: *const [frame_size]u8) FrameError!HelloFrame {
    const header = try decodeHeader(buf);
    if (header.kind != .hello) return error.InvalidKind;

    var boot_id: [16]u8 = undefined;
    @memcpy(&boot_id, buf[hello_boot_id_offset .. hello_boot_id_offset + 16]);

    return .{
        .seq = header.seq,
        .flags = header.flags,
        .mono_ns = header.mono_ns,
        .wall_ns = header.wall_ns,
        .boot_id = boot_id,
        .mem_total_kb = readInt(u64, buf, hello_mem_total_offset),
    };
}

pub fn encodeSampleFrame(frame: SampleFrame) [frame_size]u8 {
    std.debug.assert(frame.mono_ns > 0);
    std.debug.assert(frame.wall_ns > 0);

    var buf = [_]u8{0} ** frame_size;
    encodeHeader(&buf, .sample, frame.seq, frame.flags, frame.mono_ns, frame.wall_ns);
    writeInt(u64, &buf, sample_cpu_user_offset, frame.cpu_user_ticks);
    writeInt(u64, &buf, sample_cpu_system_offset, frame.cpu_system_ticks);
    writeInt(u64, &buf, sample_cpu_idle_offset, frame.cpu_idle_ticks);
    writeInt(u32, &buf, sample_load1_offset, frame.load1_centis);
    writeInt(u32, &buf, sample_load5_offset, frame.load5_centis);
    writeInt(u32, &buf, sample_load15_offset, frame.load15_centis);
    writeInt(u16, &buf, sample_procs_running_offset, frame.procs_running);
    writeInt(u16, &buf, sample_procs_blocked_offset, frame.procs_blocked);
    writeInt(u64, &buf, sample_mem_available_offset, frame.mem_available_kb);
    writeInt(u64, &buf, sample_io_read_offset, frame.io_read_bytes);
    writeInt(u64, &buf, sample_io_write_offset, frame.io_write_bytes);
    writeInt(u64, &buf, sample_net_rx_offset, frame.net_rx_bytes);
    writeInt(u64, &buf, sample_net_tx_offset, frame.net_tx_bytes);
    writeInt(u16, &buf, sample_psi_cpu_offset, frame.psi_cpu_pct100);
    writeInt(u16, &buf, sample_psi_mem_offset, frame.psi_mem_pct100);
    writeInt(u16, &buf, sample_psi_io_offset, frame.psi_io_pct100);
    return buf;
}

pub fn decodeSampleFrame(buf: *const [frame_size]u8) FrameError!SampleFrame {
    const header = try decodeHeader(buf);
    if (header.kind != .sample) return error.InvalidKind;

    return .{
        .seq = header.seq,
        .flags = header.flags,
        .mono_ns = header.mono_ns,
        .wall_ns = header.wall_ns,
        .cpu_user_ticks = readInt(u64, buf, sample_cpu_user_offset),
        .cpu_system_ticks = readInt(u64, buf, sample_cpu_system_offset),
        .cpu_idle_ticks = readInt(u64, buf, sample_cpu_idle_offset),
        .load1_centis = readInt(u32, buf, sample_load1_offset),
        .load5_centis = readInt(u32, buf, sample_load5_offset),
        .load15_centis = readInt(u32, buf, sample_load15_offset),
        .procs_running = readInt(u16, buf, sample_procs_running_offset),
        .procs_blocked = readInt(u16, buf, sample_procs_blocked_offset),
        .mem_available_kb = readInt(u64, buf, sample_mem_available_offset),
        .io_read_bytes = readInt(u64, buf, sample_io_read_offset),
        .io_write_bytes = readInt(u64, buf, sample_io_write_offset),
        .net_rx_bytes = readInt(u64, buf, sample_net_rx_offset),
        .net_tx_bytes = readInt(u64, buf, sample_net_tx_offset),
        .psi_cpu_pct100 = readInt(u16, buf, sample_psi_cpu_offset),
        .psi_mem_pct100 = readInt(u16, buf, sample_psi_mem_offset),
        .psi_io_pct100 = readInt(u16, buf, sample_psi_io_offset),
    };
}

pub fn decodeFrameKind(buf: *const [frame_size]u8) FrameError!FrameKind {
    return (try decodeHeader(buf)).kind;
}

pub fn parseUuid(text: []const u8) ![16]u8 {
    if (text.len != 36) return error.InvalidUuid;
    if (text[8] != '-' or text[13] != '-' or text[18] != '-' or text[23] != '-') {
        return error.InvalidUuid;
    }

    var out = [_]u8{0} ** 16;
    var src_index: usize = 0;
    var dst_index: usize = 0;
    while (src_index < text.len) {
        if (text[src_index] == '-') {
            src_index += 1;
            continue;
        }
        if (src_index + 1 >= text.len) return error.InvalidUuid;

        const hi = try parseHexNibble(text[src_index]);
        const lo = try parseHexNibble(text[src_index + 1]);
        out[dst_index] = (hi << 4) | lo;
        dst_index += 1;
        src_index += 2;
    }
    if (dst_index != out.len) return error.InvalidUuid;
    return out;
}

pub fn formatUuid(value: [16]u8) [36]u8 {
    const hex = "0123456789abcdef";
    var out: [36]u8 = undefined;
    var src_index: usize = 0;
    var dst_index: usize = 0;

    while (src_index < value.len) : (src_index += 1) {
        if (dst_index == 8 or dst_index == 13 or dst_index == 18 or dst_index == 23) {
            out[dst_index] = '-';
            dst_index += 1;
        }
        out[dst_index] = hex[value[src_index] >> 4];
        out[dst_index + 1] = hex[value[src_index] & 0x0f];
        dst_index += 2;
    }
    return out;
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
    writeInt(u32, buf, header_magic_offset, frame_magic);
    writeInt(u16, buf, header_version_offset, frame_version);
    writeInt(u16, buf, header_kind_offset, @intFromEnum(kind));
    writeInt(u32, buf, header_seq_offset, seq);
    writeInt(u32, buf, header_flags_offset, flags);
    writeInt(u64, buf, header_mono_offset, mono_ns);
    writeInt(u64, buf, header_wall_offset, wall_ns);
}

const Header = struct {
    kind: FrameKind,
    seq: u32,
    flags: u32,
    mono_ns: u64,
    wall_ns: u64,
};

fn decodeHeader(buf: *const [frame_size]u8) FrameError!Header {
    if (readInt(u32, buf, header_magic_offset) != frame_magic) return error.InvalidMagic;
    if (readInt(u16, buf, header_version_offset) != frame_version) return error.InvalidVersion;
    const kind_int = readInt(u16, buf, header_kind_offset);
    const kind = std.meta.intToEnum(FrameKind, kind_int) catch return error.InvalidKind;
    return .{
        .kind = kind,
        .seq = readInt(u32, buf, header_seq_offset),
        .flags = readInt(u32, buf, header_flags_offset),
        .mono_ns = readInt(u64, buf, header_mono_offset),
        .wall_ns = readInt(u64, buf, header_wall_offset),
    };
}

fn writeInt(comptime T: type, buf: *[frame_size]u8, comptime offset: usize, value: T) void {
    comptime std.debug.assert(offset + @sizeOf(T) <= frame_size);
    std.mem.writeInt(T, buf[offset .. offset + @sizeOf(T)], value, .little);
}

fn readInt(comptime T: type, buf: *const [frame_size]u8, comptime offset: usize) T {
    comptime std.debug.assert(offset + @sizeOf(T) <= frame_size);
    return std.mem.readInt(T, buf[offset .. offset + @sizeOf(T)], .little);
}

fn parseHexNibble(char: u8) !u8 {
    return switch (char) {
        '0'...'9' => char - '0',
        'a'...'f' => char - 'a' + 10,
        'A'...'F' => char - 'A' + 10,
        else => error.InvalidUuid,
    };
}

fn timespecToNs(ts: std.posix.timespec) u64 {
    std.debug.assert(ts.sec >= 0);
    std.debug.assert(ts.nsec >= 0);
    return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
}

comptime {
    std.debug.assert(frame_size == 128);
    std.debug.assert(payload_offset == 32);
    std.debug.assert(hello_mem_total_offset + @sizeOf(u64) <= frame_size);
    std.debug.assert(sample_psi_io_offset + @sizeOf(u16) <= frame_size);
    std.debug.assert(default_guest_port >= 1024);
    std.debug.assert(guest_sample_rate_hz == 60);
}

test "uuid parses and formats canonically" {
    const parsed = try parseUuid("5691d566-f1a6-4342-8604-205e83785b21");
    const formatted = formatUuid(parsed);
    try std.testing.expectEqualStrings("5691d566-f1a6-4342-8604-205e83785b21", formatted[0..]);
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
    const frame = HelloFrame{
        .seq = 1,
        .flags = 2,
        .mono_ns = 3,
        .wall_ns = 4,
        .boot_id = try parseUuid("5691d566-f1a6-4342-8604-205e83785b21"),
        .mem_total_kb = 516096,
    };
    const encoded = encodeHelloFrame(frame);
    const decoded = try decodeHelloFrame(&encoded);
    try std.testing.expectEqualDeep(frame, decoded);
}

test "decodeFrameKind rejects invalid magic" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var encoded = encodeSampleFrame(frame);
    writeInt(u32, &encoded, header_magic_offset, frame_magic ^ 0xffff);
    try std.testing.expectError(error.InvalidMagic, decodeFrameKind(&encoded));
}

test "decodeFrameKind rejects invalid version" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var encoded = encodeSampleFrame(frame);
    writeInt(u16, &encoded, header_version_offset, frame_version + 1);
    try std.testing.expectError(error.InvalidVersion, decodeFrameKind(&encoded));
}

test "decodeFrameKind rejects unknown kind" {
    const frame = SampleFrame{
        .seq = 7,
        .mono_ns = 11,
        .wall_ns = 22,
    };
    var encoded = encodeSampleFrame(frame);
    writeInt(u16, &encoded, header_kind_offset, std.math.maxInt(u16));
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
        .boot_id = [_]u8{0} ** 16,
        .mem_total_kb = 1,
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
