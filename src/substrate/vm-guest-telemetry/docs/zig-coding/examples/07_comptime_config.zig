// 07_comptime_config.zig
//
// DONT: Use runtime constants for values known at build time.
// The compiler cannot optimize buffer sizes, eliminate branches,
// or validate constraints if values are only known at runtime.
//
// DO: Use comptime constants and build-time configuration.
// Zig evaluates comptime expressions during compilation, enabling
// the compiler to eliminate dead branches and optimize buffer sizes.
// Configuration that varies per build target goes through build.zig options.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: Runtime constants that are actually known at build time
// ---------------------------------------------------------------------------
//
//   pub const default_guest_port: u32 = 10790;
//   pub const max_line_bytes: usize = 4096;
//
//   // These are "constants" but the compiler treats them as runtime values
//   // in some contexts. No compile-time validation possible.
//   // No dead-code elimination based on their values.

// ---------------------------------------------------------------------------
// DO: Comptime configuration with compile-time validation
// ---------------------------------------------------------------------------

/// Build-time configuration. In the real build, these would be injected
/// via build.zig options:
///
///   const options = b.addOptions();
///   options.addOption(u32, "guest_port", 10790);
///   options.addOption(usize, "max_line_bytes", 4096);
///   exe.root_module.addOptions("config", options);
///
/// Then in source:
///   const config = @import("config");
///   const guest_port = config.guest_port;
///
/// For this standalone example, we use comptime constants directly.
const Config = struct {
    guest_port: u32,
    max_line_bytes: usize,
    max_snapshot_bytes: usize,

    /// Validate configuration at compile time.
    /// This runs during compilation — invalid configs never produce a binary.
    fn validate(comptime self: Config) Config {
        if (self.guest_port < 1024) {
            @compileError("guest_port must be >= 1024 (non-privileged)");
        }
        if (self.max_line_bytes < 256) {
            @compileError("max_line_bytes must be >= 256 for protocol commands");
        }
        if (self.max_snapshot_bytes < self.max_line_bytes) {
            @compileError("max_snapshot_bytes must be >= max_line_bytes");
        }
        return self;
    }
};

/// The active configuration, validated at comptime.
/// If any constraint is violated, the program does not compile.
const config = (Config{
    .guest_port = 10790,
    .max_line_bytes = 4096,
    .max_snapshot_bytes = 1024 * 1024,
}).validate();

// ---------------------------------------------------------------------------
// Using comptime config to size buffers and eliminate branches
// ---------------------------------------------------------------------------

/// A line reader whose buffer size is determined at comptime.
/// The compiler knows the exact size — no dynamic allocation needed.
fn LineReader(comptime max_bytes: usize) type {
    return struct {
        const Self = @This();

        buf: [max_bytes]u8 = undefined,
        len: usize = 0,

        fn push(self: *Self, byte: u8) !void {
            if (self.len >= max_bytes) return error.LineTooLong;
            self.buf[self.len] = byte;
            self.len += 1;
        }

        fn slice(self: *const Self) []const u8 {
            return self.buf[0..self.len];
        }

        fn reset(self: *Self) void {
            self.len = 0;
        }
    };
}

/// Comptime-dispatched protocol handler.
/// The target is known at compile time, so the compiler eliminates
/// the unreachable branches entirely — zero runtime cost.
fn protocolVersion(comptime target: enum { v1, v2 }) type {
    return struct {
        fn greeting() []const u8 {
            return switch (target) {
                .v1 => "PONG vm-guest-telemetry",
                .v2 => "PONG vm-guest-telemetry v2",
            };
        }

        fn maxPayload() usize {
            return switch (target) {
                .v1 => 4096,
                .v2 => 64 * 1024,
            };
        }
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "config — comptime validation passes" {
    // If this test compiles, the config is valid.
    try std.testing.expectEqual(@as(u32, 10790), config.guest_port);
    try std.testing.expectEqual(@as(usize, 4096), config.max_line_bytes);
}

test "LineReader — buffer sized at comptime" {
    var reader = LineReader(config.max_line_bytes){};

    try reader.push('A');
    try reader.push('B');
    try reader.push('C');

    try std.testing.expectEqualStrings("ABC", reader.slice());
}

test "LineReader — rejects overflow" {
    var reader = LineReader(4){};

    try reader.push('A');
    try reader.push('B');
    try reader.push('C');
    try reader.push('D');
    try std.testing.expectError(error.LineTooLong, reader.push('E'));
}

test "comptime protocol dispatch — v1" {
    const v1 = protocolVersion(.v1);
    try std.testing.expectEqualStrings("PONG vm-guest-telemetry", v1.greeting());
    try std.testing.expectEqual(@as(usize, 4096), v1.maxPayload());
}

test "comptime protocol dispatch — v2" {
    const v2 = protocolVersion(.v2);
    try std.testing.expectEqualStrings("PONG vm-guest-telemetry v2", v2.greeting());
    try std.testing.expectEqual(@as(usize, 64 * 1024), v2.maxPayload());
}
