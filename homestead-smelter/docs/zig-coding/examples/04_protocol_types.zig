// 04_protocol_types.zig
//
// DONT: Dispatch on raw strings with chained if/else.
// Adding a new command requires updating the dispatch, the argument parser,
// and the handler — with no compiler assistance if you forget one.
//
// DO: Model the wire protocol as a tagged union.
// The compiler enforces exhaustive switches — if you add a variant,
// every switch must handle it or the build fails.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: String-based dispatch
// ---------------------------------------------------------------------------
//
//   fn handleControlConnection(stream: Stream) !void {
//       const line = try readLine(stream);
//
//       if (std.mem.eql(u8, line, "PING")) {
//           try writeLine(stream, "PONG homestead-smelter-host");
//           return;
//       }
//       if (std.mem.eql(u8, line, "SNAPSHOT")) {
//           try writeSnapshotResponse(stream);
//           return;
//       }
//       // Forgot to handle "STATUS"? No compiler error. Silent bug.
//
//       try writeLine(stream, "ERR unsupported command");
//   }

// ---------------------------------------------------------------------------
// DO: Tagged union for the command set
// ---------------------------------------------------------------------------

/// Every command the host agent can receive over its control socket.
/// Adding a variant here forces every switch to handle it — compile error
/// if you forget.
const Command = union(enum) {
    ping,
    snapshot,
    probe: ProbeParams,

    const ProbeParams = struct {
        job_id: []const u8,
    };

    /// Parse a raw line into a typed command.
    /// Returns null for unrecognized commands — the caller decides
    /// whether to send an error or drop the connection.
    fn parse(line: []const u8) ?Command {
        if (std.mem.eql(u8, line, "PING")) return .ping;
        if (std.mem.eql(u8, line, "SNAPSHOT")) return .snapshot;

        if (std.mem.startsWith(u8, line, "PROBE ")) {
            const job_id = line["PROBE ".len..];
            if (job_id.len == 0) return null;
            return .{ .probe = .{ .job_id = job_id } };
        }

        return null;
    }
};

/// Every response the host agent can send.
const Response = union(enum) {
    pong,
    snapshot: []const u8,
    probe_result: ProbeResult,
    err: []const u8,

    const ProbeResult = struct {
        job_id: []const u8,
        status: Status,
        message: []const u8,
    };

    const Status = enum { ok, @"error" };

    /// Format a response for the wire.
    /// Exhaustive switch: the compiler guarantees every variant is handled.
    fn format(self: Response, buf: []u8) ![]u8 {
        return switch (self) {
            .pong => try std.fmt.bufPrint(buf, "PONG homestead-smelter-host", .{}),
            .snapshot => |payload| try std.fmt.bufPrint(buf, "{s}", .{payload}),
            .probe_result => |result| try std.fmt.bufPrint(
                buf,
                "PROBE {s} {s}: {s}",
                .{ result.job_id, @tagName(result.status), result.message },
            ),
            .err => |message| try std.fmt.bufPrint(buf, "ERR {s}", .{message}),
        };
    }
};

/// Handle a control connection. The exhaustive switch means every command
/// variant must be handled — if you add Command.status, this function
/// will not compile until you add the handler.
fn dispatch(command: Command) Response {
    return switch (command) {
        .ping => .pong,
        .snapshot => .{ .snapshot = "{\"schema_version\":1,\"vms\":[]}" },
        .probe => |params| .{ .probe_result = .{
            .job_id = params.job_id,
            .status = .ok,
            .message = "guest alive",
        } },
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "parse — recognizes PING" {
    const cmd = Command.parse("PING");
    try std.testing.expect(cmd != null);
    try std.testing.expect(cmd.? == .ping);
}

test "parse — recognizes SNAPSHOT" {
    const cmd = Command.parse("SNAPSHOT");
    try std.testing.expect(cmd != null);
    try std.testing.expect(cmd.? == .snapshot);
}

test "parse — parses PROBE with job ID" {
    const cmd = Command.parse("PROBE job-abc-123");
    try std.testing.expect(cmd != null);

    switch (cmd.?) {
        .probe => |params| {
            try std.testing.expectEqualStrings("job-abc-123", params.job_id);
        },
        else => return error.TestUnexpectedResult,
    }
}

test "parse — returns null for unknown commands" {
    try std.testing.expect(Command.parse("UNKNOWN") == null);
    try std.testing.expect(Command.parse("") == null);
}

test "parse — rejects PROBE without job ID" {
    try std.testing.expect(Command.parse("PROBE ") == null);
}

test "dispatch — ping returns pong" {
    const response = dispatch(.ping);
    try std.testing.expect(response == .pong);
}

test "dispatch — probe returns result with job ID" {
    const response = dispatch(.{ .probe = .{ .job_id = "job-xyz" } });

    switch (response) {
        .probe_result => |result| {
            try std.testing.expectEqualStrings("job-xyz", result.job_id);
            try std.testing.expect(result.status == .ok);
        },
        else => return error.TestUnexpectedResult,
    }
}

test "response format — pong" {
    var buf: [256]u8 = undefined;
    const line = try (Response{ .pong = {} }).format(&buf);
    try std.testing.expectEqualStrings("PONG homestead-smelter-host", line);
}

test "response format — error" {
    var buf: [256]u8 = undefined;
    const line = try (Response{ .err = "unsupported command" }).format(&buf);
    try std.testing.expectEqualStrings("ERR unsupported command", line);
}
