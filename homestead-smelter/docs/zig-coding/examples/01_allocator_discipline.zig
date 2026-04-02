// 01_allocator_discipline.zig
//
// DONT: Use GeneralPurposeAllocator in a long-lived production agent.
// GPA is a debug allocator — it tracks every allocation, detects double-free
// and use-after-free, and has significant overhead. It is invaluable during
// development, but it is not a production allocator.
//
// DO: Allocate a fixed buffer at startup, then use arena-per-connection.
// This eliminates use-after-free, OOM during steady-state, and fragmentation.
// The arena bulk-frees on connection close — no individual free() calls needed.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: GPA as the production allocator
// ---------------------------------------------------------------------------
//
//   pub fn main() !void {
//       var gpa_state = std.heap.GeneralPurposeAllocator(.{}){};
//       defer _ = gpa_state.deinit();
//       const gpa = gpa_state.allocator();
//
//       while (true) {
//           // Every connection allocates and frees individually through GPA.
//           // GPA maintains metadata per allocation, fragmenting the heap.
//           const line = try readLineAlloc(gpa, stream, 4096);
//           defer gpa.free(line);
//           // ... handle line ...
//       }
//   }

// ---------------------------------------------------------------------------
// DO: Fixed buffer at startup, arena per logical unit of work
// ---------------------------------------------------------------------------

/// Maximum memory budget for the entire agent, decided at comptime.
/// If a connection cannot fit within this budget, it is rejected
/// immediately rather than degrading the system.
const connection_buffer_bytes: usize = 64 * 1024;

const Connection = struct {
    arena: std.heap.ArenaAllocator,

    /// Initialize a connection with a scoped arena over the page allocator.
    /// All allocations during the connection's lifetime come from this arena.
    /// On deinit, all memory is freed in one operation — no leak possible.
    fn init() Connection {
        return .{
            .arena = std.heap.ArenaAllocator.init(std.heap.page_allocator),
        };
    }

    fn deinit(self: *Connection) void {
        self.arena.deinit();
    }

    /// Process a single request. The arena allocator means we can call
    /// allocPrint, dupe, etc. freely — everything is freed when the
    /// connection is torn down. No individual free() calls needed.
    fn handleRequest(self: *Connection, request: []const u8) ![]const u8 {
        const allocator = self.arena.allocator();

        // These allocations are "fire and forget" — the arena owns them.
        const trimmed = std.mem.trim(u8, request, " \t\r\n");
        const response = try std.fmt.allocPrint(
            allocator,
            "ACK {d} bytes: {s}",
            .{ trimmed.len, trimmed },
        );

        return response;
    }
};

// ---------------------------------------------------------------------------
// Alternative: FixedBufferAllocator for zero-syscall allocation.
// Use when the maximum size is known at comptime and fits on the stack.
// ---------------------------------------------------------------------------

fn handleRequestNoSyscall(request: []const u8) ![]const u8 {
    // Stack buffer — zero syscalls, zero heap fragmentation.
    var buf: [connection_buffer_bytes]u8 = undefined;
    var fba = std.heap.FixedBufferAllocator.init(&buf);
    const allocator = fba.allocator();

    const response = try std.fmt.allocPrint(
        allocator,
        "ACK {d} bytes",
        .{request.len},
    );

    return response;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "arena per connection — allocations freed in bulk" {
    var conn = Connection.init();
    defer conn.deinit();

    const response = try conn.handleRequest("PING");
    try std.testing.expectEqualStrings("ACK 4 bytes: PING", response);
    // No individual free needed — conn.deinit() reclaims everything.
}

test "fixed buffer allocator — zero syscall path" {
    const response = try handleRequestNoSyscall("hello");
    try std.testing.expectEqualStrings("ACK 5 bytes", response);
}
