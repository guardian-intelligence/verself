// 02_buffered_io.zig
//
// DONT: Read one byte at a time with a syscall per byte.
// Each read(2) call crosses the user/kernel boundary. For a heartbeat agent
// called continuously, this means N syscalls per N-byte line.
//
// DO: Read into a fixed buffer, scan for the delimiter in userspace.
// One syscall fills the buffer, then scanning is pure memory access.

const std = @import("std");

// ---------------------------------------------------------------------------
// DONT: Byte-at-a-time I/O — one syscall per byte
// ---------------------------------------------------------------------------
//
//   pub fn readLineAlloc(allocator: Allocator, stream: Stream, limit: usize) ![]u8 {
//       var bytes = try std.ArrayList(u8).initCapacity(allocator, 64);
//       defer bytes.deinit(allocator);
//
//       var one: [1]u8 = undefined;
//       while (true) {
//           const n = try stream.read(one[0..]);  // <-- one syscall per byte!
//           if (n == 0) break;
//           if (one[0] == '\n') break;
//           if (bytes.items.len >= limit) return error.LineTooLong;
//           try bytes.append(allocator, one[0]);
//       }
//       return bytes.toOwnedSlice(allocator);
//   }

// ---------------------------------------------------------------------------
// DO: Use a buffered reader that amortizes syscalls
// ---------------------------------------------------------------------------

/// A line reader that uses a fixed stack buffer for I/O.
/// No allocator needed. No syscall per byte. The buffer is filled in chunks,
/// and lines are scanned in userspace.
///
/// The output slice is written into a caller-provided buffer, so the caller
/// controls the memory. This follows the libxev principle: "all memory is
/// caller-provided."
pub fn readLine(reader: anytype, output: []u8) !?[]u8 {
    var pos: usize = 0;

    while (pos < output.len) {
        const byte = reader.readByte() catch |err| switch (err) {
            error.EndOfStream => {
                if (pos == 0) return null;
                return output[0..pos];
            },
            else => return err,
        };

        switch (byte) {
            '\n' => return output[0..pos],
            '\r' => continue,
            else => {
                output[pos] = byte;
                pos += 1;
            },
        }
    }

    return error.LineTooLong;
}

// NOTE: In 0.15, std.io.bufferedReader is removed. Buffering is done
// by passing a buffer to the reader constructor:
//
//   var read_buf: [4096]u8 = undefined;
//   var file_reader = file.reader(&read_buf);
//   return readLine(&file_reader.interface, output);
//
// For std.io.fixedBufferStream (which still exists in 0.15), the reader
// returned by .reader() already operates on an in-memory buffer, so no
// additional buffering layer is needed.

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test "readLine — reads a complete line" {
    var stream = std.io.fixedBufferStream("hello world\n");
    var buf: [64]u8 = undefined;

    const line = try readLine(stream.reader(), &buf);
    try std.testing.expect(line != null);
    try std.testing.expectEqualStrings("hello world", line.?);
}

test "readLine — strips CR from CRLF" {
    var stream = std.io.fixedBufferStream("line one\r\n");
    var buf: [64]u8 = undefined;

    const line = try readLine(stream.reader(), &buf);
    try std.testing.expect(line != null);
    try std.testing.expectEqualStrings("line one", line.?);
}

test "readLine — returns null on empty stream" {
    var stream = std.io.fixedBufferStream("");
    var buf: [64]u8 = undefined;

    const line = try readLine(stream.reader(), &buf);
    try std.testing.expect(line == null);
}

test "readLine — returns error on line exceeding buffer" {
    var stream = std.io.fixedBufferStream("this line is too long\n");
    var buf: [4]u8 = undefined;

    const result = readLine(stream.reader(), &buf);
    try std.testing.expectError(error.LineTooLong, result);
}

test "readLine — handles EOF without trailing newline" {
    var stream = std.io.fixedBufferStream("no newline");
    var buf: [64]u8 = undefined;

    const line = try readLine(stream.reader(), &buf);
    try std.testing.expect(line != null);
    try std.testing.expectEqualStrings("no newline", line.?);
}

test "readLine — multiple lines from same stream" {
    var stream = std.io.fixedBufferStream("first\nsecond\nthird\n");
    var buf: [64]u8 = undefined;

    const first = (try readLine(stream.reader(), &buf)).?;
    try std.testing.expectEqualStrings("first", first);

    const second = (try readLine(stream.reader(), &buf)).?;
    try std.testing.expectEqualStrings("second", second);

    const third = (try readLine(stream.reader(), &buf)).?;
    try std.testing.expectEqualStrings("third", third);

    const eof = try readLine(stream.reader(), &buf);
    try std.testing.expect(eof == null);
}
