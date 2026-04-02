const std = @import("std");

pub const default_guest_port: u32 = 10790;
pub const max_line_bytes: usize = 4096;
pub const max_snapshot_bytes: usize = 1024 * 1024;

pub const LineError = error{
    LineTooLong,
};

pub fn writeLine(stream: std.net.Stream, line: []const u8) !void {
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

pub fn parsePort(value: []const u8) !u32 {
    return std.fmt.parseInt(u32, value, 10);
}

test "parsePort parses decimal port strings" {
    try std.testing.expectEqual(@as(u32, 10790), try parsePort("10790"));
}
