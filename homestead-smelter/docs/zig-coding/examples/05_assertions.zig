// 05_assertions.zig
//
// DONT: Write functions with no assertions, relying on callers to
// provide correct inputs. Bugs silently propagate until they corrupt
// data or crash far from the root cause.
//
// DO: Assert preconditions, postconditions, and invariants.
// Minimum two assertions per function. Assert both what you expect
// (positive space) and what you do not expect (negative space).
// Assertions downgrade catastrophic correctness bugs into liveness bugs.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: No assertions — silent corruption
// ---------------------------------------------------------------------------
//
//   fn readLineAlloc(allocator: Allocator, stream: Stream, limit: usize) ![]u8 {
//       var bytes = try std.ArrayList(u8).initCapacity(allocator, 64);
//       // No assertion that limit > 0 — a limit of 0 silently returns empty.
//       // No assertion on the result — could return garbage on partial read.
//       var one: [1]u8 = undefined;
//       while (true) {
//           const n = try stream.read(one[0..]);
//           if (n == 0) break;
//           if (one[0] == '\n') break;
//           if (bytes.items.len >= limit) return error.LineTooLong;
//           try bytes.append(allocator, one[0]);
//       }
//       return bytes.toOwnedSlice(allocator);
//   }

// ---------------------------------------------------------------------------
// DO: Assertions at every layer
// ---------------------------------------------------------------------------

/// Protocol constants. Compile-time assertions verify design integrity.
const max_line_bytes: usize = 4096;
const max_snapshot_bytes: usize = 1024 * 1024;
const default_guest_port: u32 = 10790;

// Compile-time assertions: verify relationships between constants.
// These run at comptime — zero runtime cost, caught before the program executes.
comptime {
    // Snapshot must be large enough to hold at least one line.
    std.debug.assert(max_snapshot_bytes >= max_line_bytes);

    // Line buffer must be at least large enough for the longest command.
    // "PROBE " + max job ID (256) + padding.
    std.debug.assert(max_line_bytes >= 512);

    // Port must be in the vsock range (>= 1024, non-reserved).
    std.debug.assert(default_guest_port >= 1024);
}

/// Read a line from a reader into a caller-provided buffer.
///
/// Precondition: buffer must be non-empty (a zero-length buffer is a
/// programmer error, not an operating condition).
///
/// Postcondition: returned slice does not contain '\n' or '\r'.
fn readLine(reader: anytype, buf: []u8) !?[]u8 {
    // Precondition: buffer must be usable.
    std.debug.assert(buf.len > 0);

    var pos: usize = 0;

    while (pos < buf.len) {
        const byte = reader.readByte() catch |err| switch (err) {
            error.EndOfStream => {
                if (pos == 0) return null;
                // Postcondition: no control characters in output.
                assertNoControlChars(buf[0..pos]);
                return buf[0..pos];
            },
            else => return err,
        };

        switch (byte) {
            '\n' => {
                // Postcondition: returned slice contains no newlines.
                assertNoControlChars(buf[0..pos]);
                return buf[0..pos];
            },
            '\r' => continue,
            else => {
                buf[pos] = byte;
                pos += 1;
            },
        }
    }

    return error.LineTooLong;
}

/// Assert that a slice contains no newline or carriage return characters.
/// This is a postcondition assertion — it verifies the negative space
/// (what we do NOT expect in the output).
fn assertNoControlChars(slice: []const u8) void {
    for (slice) |byte| {
        // Negative space: these characters must never appear in a parsed line.
        std.debug.assert(byte != '\n');
        std.debug.assert(byte != '\r');
    }
}

/// Parse a port number from a string.
///
/// Precondition: input is non-empty.
/// Postcondition: returned port is in the valid range [1, 65535].
fn parsePort(value: []const u8) !u16 {
    // Precondition: input is non-empty.
    std.debug.assert(value.len > 0);

    const port = try std.fmt.parseInt(u16, value, 10);

    // Postcondition: port is in the valid range.
    // Port 0 is "any port" in POSIX — never valid for explicit binding.
    std.debug.assert(port > 0);

    return port;
}

/// A ring buffer for buffering incoming bytes.
/// Demonstrates invariant assertions that hold across all operations.
fn RingBuffer(comptime capacity: usize) type {
    // Compile-time assertion: capacity must be a power of two for
    // efficient modular arithmetic (bitwise AND instead of modulo).
    comptime {
        std.debug.assert(capacity > 0);
        std.debug.assert(capacity & (capacity - 1) == 0);
    }

    return struct {
        const Self = @This();

        buf: [capacity]u8 = undefined,
        read_pos: usize = 0,
        write_pos: usize = 0,

        /// Push a byte into the ring buffer.
        fn push(self: *Self, byte: u8) !void {
            // Precondition: buffer is not full.
            const len_before = self.len();
            if (len_before >= capacity) return error.BufferFull;

            // Invariant: write_pos is always ahead of or equal to read_pos
            // (modulo wrapping). Split assertion for clarity.
            std.debug.assert(self.write_pos >= self.read_pos);
            std.debug.assert(len_before < capacity);

            self.buf[self.write_pos & (capacity - 1)] = byte;
            self.write_pos += 1;

            // Postcondition: count increased by exactly one.
            std.debug.assert(self.len() == len_before + 1);
        }

        /// Pop a byte from the ring buffer.
        fn pop(self: *Self) ?u8 {
            const len_before = self.len();
            if (len_before == 0) return null;

            const byte = self.buf[self.read_pos & (capacity - 1)];
            self.read_pos += 1;

            // Postcondition: count decreased by exactly one.
            std.debug.assert(self.len() == len_before - 1);

            return byte;
        }

        fn len(self: *const Self) usize {
            // Invariant: write_pos >= read_pos (no underflow).
            std.debug.assert(self.write_pos >= self.read_pos);
            return self.write_pos - self.read_pos;
        }
    };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "readLine — basic line" {
    var stream = std.io.fixedBufferStream("PING\n");
    var buf: [64]u8 = undefined;

    const line = try readLine(stream.reader(), &buf);
    try std.testing.expect(line != null);
    try std.testing.expectEqualStrings("PING", line.?);
}

test "parsePort — valid port" {
    const port = try parsePort("10790");
    try std.testing.expectEqual(@as(u16, 10790), port);
}

test "parsePort — rejects non-numeric" {
    try std.testing.expectError(error.InvalidCharacter, parsePort("abc"));
}

test "ring buffer — push and pop" {
    var ring = RingBuffer(16){};

    try ring.push('A');
    try ring.push('B');

    try std.testing.expectEqual(@as(usize, 2), ring.len());
    try std.testing.expectEqual(@as(u8, 'A'), ring.pop().?);
    try std.testing.expectEqual(@as(u8, 'B'), ring.pop().?);
    try std.testing.expect(ring.pop() == null);
}

test "ring buffer — full returns error" {
    var ring = RingBuffer(4){};

    try ring.push('A');
    try ring.push('B');
    try ring.push('C');
    try ring.push('D');
    try std.testing.expectError(error.BufferFull, ring.push('E'));
}

test "comptime assertions — verify constant relationships" {
    // These already ran at comptime. This test documents that they exist
    // and would catch changes that violate the invariants.
    comptime {
        std.debug.assert(max_snapshot_bytes >= max_line_bytes);
        std.debug.assert(max_line_bytes >= 512);
        std.debug.assert(default_guest_port >= 1024);
    }
}
