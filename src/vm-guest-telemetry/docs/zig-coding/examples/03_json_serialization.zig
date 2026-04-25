// 03_json_serialization.zig
//
// DONT: Hand-roll JSON serialization with manual string escaping.
// It is verbose, error-prone (easy to miss an escape character),
// and every new field requires modifying the serialization function.
//
// DO: Use std.json.stringify with typed structs.
// The compiler generates serialization code at comptime for any struct.
// Adding a field to the struct automatically includes it in the output.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: Manual JSON builder (60+ lines of fragile code)
// ---------------------------------------------------------------------------
//
//   fn buildSnapshotJSON(allocator: Allocator, ...) ![]u8 {
//       var out: std.ArrayList(u8) = .{};
//       defer out.deinit(allocator);
//       try out.ensureTotalCapacity(allocator, 512);
//       try out.appendSlice(allocator, "{\"schema_version\":1,\"jailer_root\":");
//       try appendJSONString(&out, allocator, jailer_root);
//       try out.appendSlice(allocator, ",\"guest_port\":");
//       try appendInt(&out, allocator, guest_port);
//       // ... 40 more lines of manual field-by-field serialization ...
//       // Every new field requires editing this function.
//       // Miss one escape in appendJSONString and you have a security bug.
//       return out.toOwnedSlice(allocator);
//   }

// ---------------------------------------------------------------------------
// DO: Typed structs + std.json.stringify
// ---------------------------------------------------------------------------

const VMObservation = struct {
    job_id: []const u8,
    uds_path: []const u8,
    status: Status,
    message: []const u8,
    observed_at_unix_ms: i64,

    const Status = enum {
        ok,
        @"error",
    };
};

const Snapshot = struct {
    schema_version: u32 = 1,
    jailer_root: []const u8,
    guest_port: u32,
    observed_at_unix_ms: i64,
    vms: []const VMObservation,
};

/// Serialize a snapshot to JSON. Adding a field to Snapshot or VMObservation
/// automatically includes it in the output — no serialization code to update.
fn serializeSnapshot(allocator: std.mem.Allocator, snapshot: Snapshot) ![]u8 {
    return std.json.Stringify.valueAlloc(allocator, snapshot, .{});
}

/// Write a snapshot directly to a stream writer without intermediate allocation.
/// Uses std.json.Stringify.value with a std.io.Writer.Allocating under the hood,
/// then copies the result to the stream.
fn writeSnapshotTo(allocator: std.mem.Allocator, snapshot: Snapshot, stream: anytype) !void {
    const json = try std.json.Stringify.valueAlloc(allocator, snapshot, .{});
    defer allocator.free(json);
    try stream.writeAll(json);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "serialize snapshot — round-trips through parse" {
    const allocator = std.testing.allocator;

    const observations = [_]VMObservation{
        .{
            .job_id = "job-abc",
            .uds_path = "/srv/jailer/firecracker/job-abc/root/run/vs-control.sock",
            .status = .ok,
            .message = "hello from guest",
            .observed_at_unix_ms = 1700000000000,
        },
        .{
            .job_id = "job-def",
            .uds_path = "/srv/jailer/firecracker/job-def/root/run/vs-control.sock",
            .status = .@"error",
            .message = "ConnectionRefused",
            .observed_at_unix_ms = 1700000001000,
        },
    };

    const snapshot = Snapshot{
        .jailer_root = "/srv/jailer/firecracker",
        .guest_port = 10790,
        .observed_at_unix_ms = 1700000002000,
        .vms = &observations,
    };

    const json = try serializeSnapshot(allocator, snapshot);
    defer allocator.free(json);

    // Parse it back to verify it is valid JSON.
    const parsed = try std.json.parseFromSlice(Snapshot, allocator, json, .{});
    defer parsed.deinit();

    try std.testing.expectEqual(@as(u32, 1), parsed.value.schema_version);
    try std.testing.expectEqual(@as(u32, 10790), parsed.value.guest_port);
    try std.testing.expectEqual(@as(usize, 2), parsed.value.vms.len);
    try std.testing.expectEqualStrings("job-abc", parsed.value.vms[0].job_id);
    try std.testing.expectEqualStrings("job-def", parsed.value.vms[1].job_id);
    try std.testing.expect(parsed.value.vms[1].status == .@"error");
}

test "serialize snapshot — handles special characters in strings" {
    const allocator = std.testing.allocator;

    const observations = [_]VMObservation{
        .{
            .job_id = "job-with-\"quotes\"",
            .uds_path = "/path/with\\backslash",
            .status = .@"error",
            .message = "line1\nline2\ttab",
            .observed_at_unix_ms = 0,
        },
    };

    const snapshot = Snapshot{
        .jailer_root = "/root",
        .guest_port = 10790,
        .observed_at_unix_ms = 0,
        .vms = &observations,
    };

    const json = try serializeSnapshot(allocator, snapshot);
    defer allocator.free(json);

    // If this parses back, the escaping is correct.
    const parsed = try std.json.parseFromSlice(Snapshot, allocator, json, .{});
    defer parsed.deinit();

    try std.testing.expectEqualStrings("job-with-\"quotes\"", parsed.value.vms[0].job_id);
    try std.testing.expectEqualStrings("line1\nline2\ttab", parsed.value.vms[0].message);
}

test "write snapshot to stream — serialize and write" {
    const allocator = std.testing.allocator;
    var buf: [2048]u8 = undefined;
    var stream = std.io.fixedBufferStream(&buf);

    const snapshot = Snapshot{
        .jailer_root = "/srv/jailer",
        .guest_port = 10790,
        .observed_at_unix_ms = 0,
        .vms = &.{},
    };

    try writeSnapshotTo(allocator, snapshot, stream.writer());

    const written = stream.getWritten();
    try std.testing.expect(written.len > 0);
    try std.testing.expect(std.mem.indexOf(u8, written, "\"schema_version\":1") != null);
}
