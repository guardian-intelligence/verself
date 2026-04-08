// 06_ptrcast_encapsulation.zig
//
// DONT: Scatter @ptrCast / @alignCast at every call site.
// Each cast is a potential safety hole. Repeating the cast pattern
// increases the probability of getting it wrong.
//
// DO: Encapsulate the cast in a typed helper that appears exactly once.
// This follows the Zig standard library's pattern (e.g., std.io.Writer).

const std = @import("std");
const linux = std.os.linux;

// ---------------------------------------------------------------------------
// DONT: Raw @ptrCast at every bind/connect call site
// ---------------------------------------------------------------------------
//
//   fn bindVsock(fd: std.posix.fd_t, port: u32) !void {
//       var address = linux.sockaddr.vm{
//           .port = port,
//           .cid = std.math.maxInt(u32),
//           .flags = 0,
//       };
//       // @ptrCast repeated at every call site — easy to forget @alignCast,
//       // easy to pass the wrong @sizeOf.
//       try std.posix.bind(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
//   }
//
//   fn connectVsock(fd: std.posix.fd_t, cid: u32, port: u32) !void {
//       var address = linux.sockaddr.vm{
//           .port = port,
//           .cid = cid,
//           .flags = 0,
//       };
//       // Same @ptrCast repeated — violates DRY, risk of divergence.
//       try std.posix.connect(fd, @ptrCast(&address), @sizeOf(linux.sockaddr.vm));
//   }

// ---------------------------------------------------------------------------
// DO: Typed vsock helpers — the cast appears exactly once
// ---------------------------------------------------------------------------

const VsockAddress = struct {
    cid: u32,
    port: u32,

    const cid_any: u32 = std.math.maxInt(u32);
    const cid_host: u32 = 2;

    /// Convert to the kernel sockaddr representation.
    /// The @ptrCast lives here and nowhere else.
    fn toSockaddr(self: VsockAddress) struct { addr: linux.sockaddr.vm, len: u32 } {
        return .{
            .addr = .{
                .port = self.port,
                .cid = self.cid,
                .flags = 0,
            },
            .len = @sizeOf(linux.sockaddr.vm),
        };
    }

    /// Bind a socket to this vsock address.
    fn bind(self: VsockAddress, fd: std.posix.fd_t) !void {
        var sa = self.toSockaddr();
        try std.posix.bind(fd, @ptrCast(&sa.addr), sa.len);
    }

    /// Connect a socket to this vsock address.
    fn connect(self: VsockAddress, fd: std.posix.fd_t) !void {
        var sa = self.toSockaddr();
        try std.posix.connect(fd, @ptrCast(&sa.addr), sa.len);
    }
};

/// Create a vsock socket with CLOEXEC.
fn vsockSocket() !std.posix.fd_t {
    return try std.posix.socket(
        linux.AF.VSOCK,
        std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC,
        0,
    );
}

// ---------------------------------------------------------------------------
// Usage comparison
// ---------------------------------------------------------------------------

// With the helper, call sites are clean and type-safe:
//
//   const addr = VsockAddress{ .cid = VsockAddress.cid_any, .port = 10790 };
//   const fd = try vsockSocket();
//   errdefer std.posix.close(fd);
//   try addr.bind(fd);
//   try std.posix.listen(fd, 8);

// ---------------------------------------------------------------------------
// Tests (unit-testable without actual vsock support)
// ---------------------------------------------------------------------------

test "VsockAddress — toSockaddr produces correct layout" {
    const addr = VsockAddress{ .cid = VsockAddress.cid_any, .port = 10790 };
    const sa = addr.toSockaddr();

    try std.testing.expectEqual(@as(u32, 10790), sa.addr.port);
    try std.testing.expectEqual(VsockAddress.cid_any, sa.addr.cid);
    try std.testing.expectEqual(@as(u32, 0), sa.addr.flags);
    try std.testing.expectEqual(@as(u32, @sizeOf(linux.sockaddr.vm)), sa.len);
}

test "VsockAddress — cid_any is maxInt(u32)" {
    try std.testing.expectEqual(std.math.maxInt(u32), VsockAddress.cid_any);
}

test "VsockAddress — cid_host is 2 (VMADDR_CID_HOST)" {
    try std.testing.expectEqual(@as(u32, 2), VsockAddress.cid_host);
}

test "VsockAddress — toSockaddr size matches kernel struct" {
    const sa = (VsockAddress{ .cid = 2, .port = 1234 }).toSockaddr();

    // The length must exactly match the kernel struct size.
    // This assertion would catch a padding/alignment mismatch.
    try std.testing.expectEqual(@sizeOf(linux.sockaddr.vm), sa.len);
}
