const std = @import("std");
const hs = @import("homestead_smelter");

pub const request_magic: u32 = 0x48534d00;
pub const request_version: u16 = 1;
pub const request_size: usize = 32;

pub const packet_magic: u32 = 0x48534d01;
pub const packet_version: u16 = 1;
pub const packet_size: usize = 176;
pub const packet_payload_size: usize = hs.frame_size;

pub const packet_flag_snapshot: u32 = 1 << 0;

pub const RequestKind = enum(u16) {
    ping = 1,
    snapshot = 2,
};

pub const PacketKind = enum(u16) {
    pong = 1,
    hello = 2,
    sample = 3,
    snapshot_end = 4,
};

pub const Request = struct {
    /// Request opcode. The request shape is fixed-size and carries no payload.
    kind: RequestKind,
};

pub const PacketHeader = struct {
    /// Response opcode.
    kind: PacketKind,
    /// Monotonic host-assigned packet sequence.
    host_seq: u64 = 0,
    /// Host realtime clock at emit time, in nanoseconds since the Unix epoch.
    observed_wall_ns: u64 = 0,
    /// CI job identity as raw UUID bytes.
    job_id: [16]u8 = [_]u8{0} ** 16,
    /// Host stream generation. Increments when the host reconnects to a guest bridge.
    stream_generation: u32 = 0,
    /// Host-side packet flags.
    flags: u32 = 0,
};

pub const Packet = struct {
    header: PacketHeader,
    payload: [packet_payload_size]u8 = [_]u8{0} ** packet_payload_size,
};

pub const Error = error{
    InvalidMagic,
    InvalidVersion,
    InvalidKind,
};

const request_magic_offset: usize = 0;
const request_version_offset: usize = 4;
const request_kind_offset: usize = 6;

const packet_magic_offset: usize = 0;
const packet_version_offset: usize = 4;
const packet_kind_offset: usize = 6;
const packet_host_seq_offset: usize = 8;
const packet_observed_wall_offset: usize = 16;
const packet_job_id_offset: usize = 24;
const packet_stream_generation_offset: usize = 40;
const packet_flags_offset: usize = 44;
const packet_payload_offset: usize = 48;

pub fn encodeRequest(request: Request) [request_size]u8 {
    var buf = [_]u8{0} ** request_size;
    writeInt(u32, &buf, request_magic_offset, request_magic);
    writeInt(u16, &buf, request_version_offset, request_version);
    writeInt(u16, &buf, request_kind_offset, @intFromEnum(request.kind));
    return buf;
}

pub fn decodeRequest(buf: *const [request_size]u8) Error!Request {
    if (readInt(u32, buf, request_magic_offset) != request_magic) return error.InvalidMagic;
    if (readInt(u16, buf, request_version_offset) != request_version) return error.InvalidVersion;
    const kind_int = readInt(u16, buf, request_kind_offset);
    return .{
        .kind = std.meta.intToEnum(RequestKind, kind_int) catch return error.InvalidKind,
    };
}

pub fn encodePacket(packet: Packet) [packet_size]u8 {
    var buf = [_]u8{0} ** packet_size;
    writeInt(u32, &buf, packet_magic_offset, packet_magic);
    writeInt(u16, &buf, packet_version_offset, packet_version);
    writeInt(u16, &buf, packet_kind_offset, @intFromEnum(packet.header.kind));
    writeInt(u64, &buf, packet_host_seq_offset, packet.header.host_seq);
    writeInt(u64, &buf, packet_observed_wall_offset, packet.header.observed_wall_ns);
    @memcpy(buf[packet_job_id_offset .. packet_job_id_offset + 16], packet.header.job_id[0..]);
    writeInt(u32, &buf, packet_stream_generation_offset, packet.header.stream_generation);
    writeInt(u32, &buf, packet_flags_offset, packet.header.flags);
    @memcpy(buf[packet_payload_offset .. packet_payload_offset + packet_payload_size], packet.payload[0..]);
    return buf;
}

pub fn decodePacket(buf: *const [packet_size]u8) Error!Packet {
    if (readInt(u32, buf, packet_magic_offset) != packet_magic) return error.InvalidMagic;
    if (readInt(u16, buf, packet_version_offset) != packet_version) return error.InvalidVersion;
    const kind_int = readInt(u16, buf, packet_kind_offset);
    const kind = std.meta.intToEnum(PacketKind, kind_int) catch return error.InvalidKind;

    var job_id: [16]u8 = undefined;
    @memcpy(job_id[0..], buf[packet_job_id_offset .. packet_job_id_offset + 16]);

    var payload: [packet_payload_size]u8 = undefined;
    @memcpy(payload[0..], buf[packet_payload_offset .. packet_payload_offset + packet_payload_size]);

    return .{
        .header = .{
            .kind = kind,
            .host_seq = readInt(u64, buf, packet_host_seq_offset),
            .observed_wall_ns = readInt(u64, buf, packet_observed_wall_offset),
            .job_id = job_id,
            .stream_generation = readInt(u32, buf, packet_stream_generation_offset),
            .flags = readInt(u32, buf, packet_flags_offset),
        },
        .payload = payload,
    };
}

pub fn zeroPayload() [packet_payload_size]u8 {
    return [_]u8{0} ** packet_payload_size;
}

fn writeInt(comptime T: type, buf: anytype, comptime offset: usize, value: T) void {
    comptime std.debug.assert(offset + @sizeOf(T) <= @typeInfo(@TypeOf(buf.*)).array.len);
    std.mem.writeInt(T, buf[offset .. offset + @sizeOf(T)], value, .little);
}

fn readInt(comptime T: type, buf: anytype, comptime offset: usize) T {
    comptime std.debug.assert(offset + @sizeOf(T) <= @typeInfo(@TypeOf(buf.*)).array.len);
    return std.mem.readInt(T, buf[offset .. offset + @sizeOf(T)], .little);
}

comptime {
    std.debug.assert(packet_payload_offset + packet_payload_size == packet_size);
}

test "request round-trips" {
    const encoded = encodeRequest(.{ .kind = .snapshot });
    const decoded = try decodeRequest(&encoded);
    try std.testing.expectEqual(.snapshot, decoded.kind);
}

test "packet round-trips hello payload" {
    const hello = hs.HelloFrame{
        .seq = 0,
        .mono_ns = 11,
        .wall_ns = 22,
        .boot_id = try hs.parseUuid("5691d566-f1a6-4342-8604-205e83785b21"),
        .mem_total_kb = 516096,
    };
    const packet = Packet{
        .header = .{
            .kind = .hello,
            .host_seq = 7,
            .observed_wall_ns = 99,
            .job_id = try hs.parseUuid("4ea0c4ce-4ca7-4389-9c89-612578239c8d"),
            .stream_generation = 3,
            .flags = packet_flag_snapshot,
        },
        .payload = hs.encodeHelloFrame(hello),
    };

    const encoded = encodePacket(packet);
    const decoded = try decodePacket(&encoded);
    try std.testing.expectEqualDeep(packet, decoded);
    try std.testing.expectEqualDeep(hello, try hs.decodeHelloFrame(&decoded.payload));
}

test "decodeRequest rejects invalid magic" {
    var encoded = encodeRequest(.{ .kind = .ping });
    writeInt(u32, &encoded, request_magic_offset, request_magic ^ 0xffff);
    try std.testing.expectError(error.InvalidMagic, decodeRequest(&encoded));
}

test "decodePacket rejects invalid kind" {
    var encoded = encodePacket(.{
        .header = .{
            .kind = .pong,
        },
    });
    writeInt(u16, &encoded, packet_kind_offset, std.math.maxInt(u16));
    try std.testing.expectError(error.InvalidKind, decodePacket(&encoded));
}
