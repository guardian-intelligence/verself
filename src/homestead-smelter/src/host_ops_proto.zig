const std = @import("std");

// Wire protocol for the privileged operations socket.
//
// Variable-length messages over AF_UNIX SEQPACKET. Max 4096 bytes per message.
// SEQPACKET provides message boundaries — no length framing needed.
//
// Request (client → server):
//   Offset  Size  Field
//   0       4     magic        0x48534d10
//   4       2     version      1
//   6       2     op           OpCode enum
//   8       8     request_id   client-assigned correlation ID
//   16      2     payload_len  0..4078
//   18      N     payload      OpCode-specific args
//
// Response (server → client):
//   Offset  Size  Field
//   0       4     magic        0x48534d11
//   4       2     version      1
//   6       2     status       0=ok, else error code
//   8       8     request_id   echoed from request
//   16      2     payload_len  0..4078
//   18      N     payload      result data or UTF-8 error message

pub const request_magic: u32 = 0x48534d10;
pub const response_magic: u32 = 0x48534d11;
pub const protocol_version: u16 = 1;
pub const max_message_size: usize = 4096;
pub const header_size: usize = 18;
pub const max_payload_size: usize = max_message_size - header_size;

pub const OpCode = enum(u16) {
    zfs_clone = 1,
    zfs_destroy = 2,
    tap_create = 3,
    tap_up = 4,
    tap_delete = 5,
    setup_jail = 6,
    start_jailer = 7,
    chown = 8,
    mknod_block = 9,
    chmod = 10,
};

pub const Status = enum(u16) {
    ok = 0,
    invalid_request = 1,
    validation_failed = 2,
    operation_failed = 3,
    internal_error = 4,
};

pub const Error = error{
    InvalidMagic,
    InvalidVersion,
    InvalidOpCode,
    InvalidStatus,
    PayloadTooLarge,
    MessageTruncated,
};

pub const Request = struct {
    op: OpCode,
    request_id: u64,
    payload: []const u8,
};

pub const Response = struct {
    status: Status,
    request_id: u64,
    payload: []const u8,
};

// Field offsets (shared between request and response headers).
const magic_offset: usize = 0;
const version_offset: usize = 4;
const opcode_or_status_offset: usize = 6;
const request_id_offset: usize = 8;
const payload_len_offset: usize = 16;

pub fn encodeRequest(buf: *[max_message_size]u8, req: Request) usize {
    const payload_len: u16 = @intCast(req.payload.len);
    writeInt(u32, buf, magic_offset, request_magic);
    writeInt(u16, buf, version_offset, protocol_version);
    writeInt(u16, buf, opcode_or_status_offset, @intFromEnum(req.op));
    writeInt(u64, buf, request_id_offset, req.request_id);
    writeInt(u16, buf, payload_len_offset, payload_len);
    if (req.payload.len > 0) {
        @memcpy(buf[header_size .. header_size + req.payload.len], req.payload);
    }
    return header_size + req.payload.len;
}

pub fn decodeRequest(msg: []const u8) Error!Request {
    if (msg.len < header_size) return error.MessageTruncated;
    if (readInt(u32, msg, magic_offset) != request_magic) return error.InvalidMagic;
    if (readInt(u16, msg, version_offset) != protocol_version) return error.InvalidVersion;
    const op_int = readInt(u16, msg, opcode_or_status_offset);
    const op = std.meta.intToEnum(OpCode, op_int) catch return error.InvalidOpCode;
    const request_id = readInt(u64, msg, request_id_offset);
    const payload_len = readInt(u16, msg, payload_len_offset);
    if (payload_len > max_payload_size) return error.PayloadTooLarge;
    const total = header_size + payload_len;
    if (msg.len < total) return error.MessageTruncated;
    return .{
        .op = op,
        .request_id = request_id,
        .payload = msg[header_size..total],
    };
}

pub fn encodeResponse(buf: *[max_message_size]u8, resp: Response) usize {
    const payload_len: u16 = @intCast(resp.payload.len);
    writeInt(u32, buf, magic_offset, response_magic);
    writeInt(u16, buf, version_offset, protocol_version);
    writeInt(u16, buf, opcode_or_status_offset, @intFromEnum(resp.status));
    writeInt(u64, buf, request_id_offset, resp.request_id);
    writeInt(u16, buf, payload_len_offset, payload_len);
    if (resp.payload.len > 0) {
        @memcpy(buf[header_size .. header_size + resp.payload.len], resp.payload);
    }
    return header_size + resp.payload.len;
}

pub fn decodeResponse(msg: []const u8) Error!Response {
    if (msg.len < header_size) return error.MessageTruncated;
    if (readInt(u32, msg, magic_offset) != response_magic) return error.InvalidMagic;
    if (readInt(u16, msg, version_offset) != protocol_version) return error.InvalidVersion;
    const status_int = readInt(u16, msg, opcode_or_status_offset);
    const status = std.meta.intToEnum(Status, status_int) catch return error.InvalidStatus;
    const request_id = readInt(u64, msg, request_id_offset);
    const payload_len = readInt(u16, msg, payload_len_offset);
    if (payload_len > max_payload_size) return error.PayloadTooLarge;
    const total = header_size + payload_len;
    if (msg.len < total) return error.MessageTruncated;
    return .{
        .status = status,
        .request_id = request_id,
        .payload = msg[header_size..total],
    };
}

// --- Payload encoding helpers ---
//
// String args: [2-byte len][bytes], concatenated.
// Integer args: 4-byte little-endian after the strings.

pub fn writeString(buf: []u8, offset: usize, s: []const u8) ?usize {
    const end = offset + 2 + s.len;
    if (end > buf.len) return null;
    std.mem.writeInt(u16, buf[offset..][0..2], @intCast(s.len), .little);
    @memcpy(buf[offset + 2 .. end], s);
    return end;
}

pub fn readString(payload: []const u8, offset: usize) ?struct { value: []const u8, next: usize } {
    if (offset + 2 > payload.len) return null;
    const len = std.mem.readInt(u16, payload[offset..][0..2], .little);
    const end = offset + 2 + len;
    if (end > payload.len) return null;
    return .{ .value = payload[offset + 2 .. end], .next = end };
}

pub fn writeU32(buf: []u8, offset: usize, value: u32) ?usize {
    const end = offset + 4;
    if (end > buf.len) return null;
    std.mem.writeInt(u32, buf[offset..][0..4], value, .little);
    return end;
}

pub fn readU32(payload: []const u8, offset: usize) ?struct { value: u32, next: usize } {
    const end = offset + 4;
    if (end > payload.len) return null;
    return .{
        .value = std.mem.readInt(u32, payload[offset..][0..4], .little),
        .next = end,
    };
}

fn writeInt(comptime T: type, buf: anytype, comptime offset: usize, value: T) void {
    std.mem.writeInt(T, buf[offset .. offset + @sizeOf(T)], value, .little);
}

fn readInt(comptime T: type, msg: []const u8, comptime offset: usize) T {
    return std.mem.readInt(T, msg[offset..][0..@sizeOf(T)], .little);
}

// --- Success response helpers ---

pub fn okResponse(buf: *[max_message_size]u8, request_id: u64) usize {
    return encodeResponse(buf, .{ .status = .ok, .request_id = request_id, .payload = &.{} });
}

pub fn okResponseWithU32(buf: *[max_message_size]u8, request_id: u64, value: u32) usize {
    var payload: [4]u8 = undefined;
    std.mem.writeInt(u32, &payload, value, .little);
    return encodeResponse(buf, .{ .status = .ok, .request_id = request_id, .payload = &payload });
}

pub fn errorResponse(buf: *[max_message_size]u8, request_id: u64, status: Status, msg: []const u8) usize {
    const clamped = if (msg.len > max_payload_size) msg[0..max_payload_size] else msg;
    return encodeResponse(buf, .{ .status = status, .request_id = request_id, .payload = clamped });
}

// --- Tests ---

test "request round-trips" {
    var buf: [max_message_size]u8 = undefined;
    var payload_buf: [128]u8 = undefined;
    var pos: usize = 0;
    pos = writeString(&payload_buf, pos, "forgepool/golden-zvol@ready") orelse unreachable;
    pos = writeString(&payload_buf, pos, "forgepool/ci/test-job") orelse unreachable;
    pos = writeString(&payload_buf, pos, "test-job-id") orelse unreachable;

    const len = encodeRequest(&buf, .{
        .op = .zfs_clone,
        .request_id = 42,
        .payload = payload_buf[0..pos],
    });

    const decoded = try decodeRequest(buf[0..len]);
    try std.testing.expectEqual(.zfs_clone, decoded.op);
    try std.testing.expectEqual(@as(u64, 42), decoded.request_id);
    try std.testing.expectEqual(pos, decoded.payload.len);
}

test "response round-trips" {
    var buf: [max_message_size]u8 = undefined;
    const len = okResponse(&buf, 99);
    const decoded = try decodeResponse(buf[0..len]);
    try std.testing.expectEqual(.ok, decoded.status);
    try std.testing.expectEqual(@as(u64, 99), decoded.request_id);
    try std.testing.expectEqual(@as(usize, 0), decoded.payload.len);
}

test "error response round-trips" {
    var buf: [max_message_size]u8 = undefined;
    const msg = "dataset path traversal blocked";
    const len = errorResponse(&buf, 7, .validation_failed, msg);
    const decoded = try decodeResponse(buf[0..len]);
    try std.testing.expectEqual(.validation_failed, decoded.status);
    try std.testing.expectEqual(@as(u64, 7), decoded.request_id);
    try std.testing.expectEqualStrings(msg, decoded.payload);
}

test "response with u32 payload round-trips" {
    var buf: [max_message_size]u8 = undefined;
    const len = okResponseWithU32(&buf, 55, 12345);
    const decoded = try decodeResponse(buf[0..len]);
    try std.testing.expectEqual(.ok, decoded.status);
    const pid = readU32(decoded.payload, 0) orelse unreachable;
    try std.testing.expectEqual(@as(u32, 12345), pid.value);
}

test "decodeRequest rejects invalid magic" {
    var buf: [max_message_size]u8 = undefined;
    _ = encodeRequest(&buf, .{ .op = .zfs_clone, .request_id = 1, .payload = &.{} });
    writeInt(u32, &buf, magic_offset, 0xdeadbeef);
    try std.testing.expectError(error.InvalidMagic, decodeRequest(buf[0..header_size]));
}

test "decodeRequest rejects truncated message" {
    var buf: [max_message_size]u8 = undefined;
    _ = encodeRequest(&buf, .{ .op = .zfs_clone, .request_id = 1, .payload = &.{} });
    try std.testing.expectError(error.MessageTruncated, decodeRequest(buf[0..4]));
}

test "string encode/decode round-trips" {
    var buf: [128]u8 = undefined;
    const s = "forgepool/ci/test";
    const end = writeString(&buf, 0, s) orelse unreachable;
    const result = readString(&buf, 0) orelse unreachable;
    try std.testing.expectEqualStrings(s, result.value);
    try std.testing.expectEqual(end, result.next);
}
