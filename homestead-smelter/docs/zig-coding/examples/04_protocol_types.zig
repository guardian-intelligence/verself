// 04_protocol_types.zig
//
// DONT: Dispatch on ad hoc protocol values with chained if/else.
// Adding a new request or packet kind then requires updating multiple
// decode and dispatch sites, with no compiler assistance if you forget one.
//
// DO: Model the wire protocol as a tagged union.
// The compiler enforces exhaustive switches — if you add a variant,
// every switch must handle it or the build fails.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: Numeric dispatch without typed protocol values
// ---------------------------------------------------------------------------
//
//   fn decodeRequestKind(kind: u16) !RequestKind {
//       if (kind == 1) return .attach;
//       if (kind == 2) return .status;
//       // Forgot to handle "tail"? No compiler error. Silent bug.
//       return error.InvalidRequestKind;
//   }

// ---------------------------------------------------------------------------
// DO: Tagged union for protocol records
// ---------------------------------------------------------------------------

/// Every request the host agent can receive over its control socket.
/// Adding a variant here forces every switch to handle it — compile error
/// if you forget.
const Request = union(enum) {
    attach,
    status,

    fn decode(kind: u16) !Request {
        return switch (kind) {
            1 => .attach,
            2 => .status,
            else => error.InvalidRequestKind,
        };
    }
};

const HelloPayload = struct {
    job_id: []const u8,
};

const SamplePayload = struct {
    mem_available_kb: u64,
};

const SnapshotEnd = struct {
    host_seq: u64,
};

const DisconnectReason = enum {
    bridge_closed,
    connect_failed,
    decode_failed,
    vm_gone,
};

/// Every packet the host agent can emit on the attach socket.
const Packet = union(enum) {
    hello: HelloPayload,
    sample: SamplePayload,
    disconnect: DisconnectReason,
    vm_gone,
    snapshot_end: SnapshotEnd,
};

/// Route a typed packet. The exhaustive switch means every packet kind must be
/// handled — if you add Packet.tail_gap, this function will not compile until
/// you add the handler.
fn route(packet: Packet) []const u8 {
    return switch (packet) {
        .hello => "hello",
        .sample => "sample",
        .disconnect => "disconnect",
        .vm_gone => "vm_gone",
        .snapshot_end => "snapshot_end",
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "decode — recognizes attach" {
    const request = try Request.decode(1);
    try std.testing.expect(request == .attach);
}

test "decode — recognizes status" {
    const request = try Request.decode(2);
    try std.testing.expect(request == .status);
}

test "decode — rejects unknown request kinds" {
    try std.testing.expectError(error.InvalidRequestKind, Request.decode(99));
}

test "route — sample packet is exhaustively handled" {
    const kind = route(.{ .sample = .{ .mem_available_kb = 401232 } });
    try std.testing.expectEqualStrings("sample", kind);
}

test "route — snapshot_end packet is exhaustively handled" {
    const kind = route(.{ .snapshot_end = .{ .host_seq = 17 } });
    try std.testing.expectEqualStrings("snapshot_end", kind);
}
