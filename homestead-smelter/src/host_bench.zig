const std = @import("std");
const host = @import("host.zig");

pub fn main() !void {
    const allocator = std.heap.page_allocator;
    const scenarios = [_]struct {
        stream_count: usize,
        samples_per_stream: usize,
    }{
        .{ .stream_count = 1, .samples_per_stream = 250_000 },
        .{ .stream_count = 8, .samples_per_stream = 125_000 },
        .{ .stream_count = 32, .samples_per_stream = 62_500 },
    };

    var best: ?host.BenchmarkResult = null;
    for (scenarios) |scenario| {
        const result = try host.benchmarkIngest(allocator, scenario.stream_count, scenario.samples_per_stream);
        if (best == null or result.samples_per_second > best.?.samples_per_second) {
            best = result;
        }
        try printResult("SCENARIO", result);
    }

    try printResult("MAX", best.?);
}

fn printResult(prefix: []const u8, result: host.BenchmarkResult) !void {
    var buf: [256]u8 = undefined;
    const line = try std.fmt.bufPrint(&buf,
        "{s} streams={d} total_samples={d} elapsed_ns={d} samples_per_second={d}\n",
        .{ prefix, result.stream_count, result.total_samples, result.elapsed_ns, result.samples_per_second },
    );
    try std.fs.File.stdout().writeAll(line);
}
