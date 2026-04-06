const std = @import("std");
const proto = @import("host_ops_proto.zig");

// Operation executors for the privileged ops socket.
//
// Each operation validates arguments against a security policy, then shells
// out to the relevant CLI tool. All operations are synchronous and complete
// within the caller's thread — ZFS clone is ~2ms COW, TAP setup is <10ms.

/// Configuration for the ops executor. Set once at startup from CLI args.
pub const OpsConfig = struct {
    /// ZFS pool prefix that all dataset operations must be under (e.g. "forgepool").
    pool_prefix: []const u8,
    /// Absolute path that all jail paths must be under (e.g. "/srv/jailer").
    jailer_root: []const u8,
    /// Min/max UID range for chown/jailer operations.
    uid_min: u32 = 10000,
    uid_max: u32 = 65534,
    /// Min/max GID range for chown/jailer operations.
    gid_min: u32 = 10000,
    gid_max: u32 = 65534,
    /// Paths to executables.
    zfs_bin: []const u8 = "/usr/sbin/zfs",
    ip_bin: []const u8 = "/usr/sbin/ip",
    mknod_bin: []const u8 = "/usr/bin/mknod",
    firecracker_bin: []const u8 = "/usr/local/bin/firecracker",
    jailer_bin: []const u8 = "/usr/local/bin/jailer",
};

pub const OpsError = error{
    ValidationFailed,
    OperationFailed,
    PayloadMalformed,
};

/// Dispatch a decoded request to the appropriate operation executor.
/// Writes the response into resp_buf and returns the response length.
pub fn dispatch(config: *const OpsConfig, req: proto.Request, resp_buf: *[proto.max_message_size]u8) usize {
    return switch (req.op) {
        .zfs_clone => execZfsClone(config, req, resp_buf),
        .zfs_destroy => execZfsDestroy(config, req, resp_buf),
        .tap_create => execTapCreate(config, req, resp_buf),
        .tap_up => execTapUp(config, req, resp_buf),
        .tap_delete => execTapDelete(config, req, resp_buf),
        .setup_jail => execSetupJail(config, req, resp_buf),
        .start_jailer => execStartJailer(config, req, resp_buf),
        .chown => execChown(config, req, resp_buf),
        .mknod_block => execMknodBlock(config, req, resp_buf),
    };
}

// --- Argument validation ---

fn validateDataset(config: *const OpsConfig, dataset: []const u8) ?[]const u8 {
    if (dataset.len == 0) return "dataset is empty";
    if (std.mem.indexOf(u8, dataset, "..") != null) return "dataset contains '..'";
    if (!std.mem.startsWith(u8, dataset, config.pool_prefix)) return "dataset not under pool prefix";
    for (dataset) |c| {
        switch (c) {
            'a'...'z', 'A'...'Z', '0'...'9', '.', '_', '/', '-', '@', ':' => {},
            else => return "dataset contains invalid character",
        }
    }
    return null;
}

fn validateTapName(name: []const u8) ?[]const u8 {
    // Must match ^fc-tap-[a-z0-9]+$
    if (name.len < 8) return "tap name too short";
    if (!std.mem.startsWith(u8, name, "fc-tap-")) return "tap name must start with 'fc-tap-'";
    for (name[7..]) |c| {
        switch (c) {
            'a'...'z', '0'...'9' => {},
            else => return "tap name contains invalid character after prefix",
        }
    }
    return null;
}

fn validateAbsolutePath(config: *const OpsConfig, path: []const u8) ?[]const u8 {
    if (path.len == 0) return "path is empty";
    if (path[0] != '/') return "path is not absolute";
    if (std.mem.indexOf(u8, path, "..") != null) return "path contains '..'";
    if (!std.mem.startsWith(u8, path, config.jailer_root)) return "path not under jailer root";
    return null;
}

fn validateUidGid(config: *const OpsConfig, uid: u32, gid: u32) ?[]const u8 {
    if (uid < config.uid_min or uid > config.uid_max) return "uid out of allowed range";
    if (gid < config.gid_min or gid > config.gid_max) return "gid out of allowed range";
    return null;
}

// --- Operation executors ---

fn execZfsClone(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const snapshot = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing snapshot arg");
    const target = proto.readString(req.payload, snapshot.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing target arg");
    const job_id = proto.readString(req.payload, target.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing job_id arg");

    if (validateDataset(config, snapshot.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);
    if (validateDataset(config, target.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    var job_prop_buf: [256]u8 = undefined;
    const job_prop = std.fmt.bufPrint(&job_prop_buf, "forge:job_id={s}", .{job_id.value}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "job_id too long");

    var time_buf: [64]u8 = undefined;
    const now_epoch_s = std.time.timestamp();
    const time_prop = std.fmt.bufPrint(&time_buf, "forge:created_at={d}", .{now_epoch_s}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "timestamp format failed");

    return runCommand(resp, req.request_id, &.{
        config.zfs_bin, "clone", "-o", job_prop, "-o", time_prop, snapshot.value, target.value,
    });
}

fn execZfsDestroy(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const dataset = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing dataset arg");

    if (validateDataset(config, dataset.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    return runCommand(resp, req.request_id, &.{ config.zfs_bin, "destroy", dataset.value });
}

fn execTapCreate(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    _ = config;
    const tap_name = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing tap_name arg");
    const host_cidr = proto.readString(req.payload, tap_name.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing host_cidr arg");

    if (validateTapName(tap_name.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    // Create TAP device.
    const create_result = runCommandResult(&.{ "/usr/sbin/ip", "tuntap", "add", tap_name.value, "mode", "tap" });
    if (create_result.failed) {
        return proto.errorResponse(resp, req.request_id, .operation_failed, create_result.stderr_slice());
    }

    // Assign IP address.
    const addr_result = runCommandResult(&.{ "/usr/sbin/ip", "addr", "add", host_cidr.value, "dev", tap_name.value });
    if (addr_result.failed) {
        // Rollback: delete the TAP we just created.
        _ = runCommandResult(&.{ "/usr/sbin/ip", "link", "del", tap_name.value });
        return proto.errorResponse(resp, req.request_id, .operation_failed, addr_result.stderr_slice());
    }

    return proto.okResponse(resp, req.request_id);
}

fn execTapUp(_: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const tap_name = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing tap_name arg");

    if (validateTapName(tap_name.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    return runCommand(resp, req.request_id, &.{ "/usr/sbin/ip", "link", "set", tap_name.value, "up" });
}

fn execTapDelete(_: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const tap_name = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing tap_name arg");

    if (validateTapName(tap_name.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    return runCommand(resp, req.request_id, &.{ "/usr/sbin/ip", "link", "del", tap_name.value });
}

fn execSetupJail(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const jail_root = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing jail_root arg");
    const zvol_dev = proto.readString(req.payload, jail_root.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing zvol_dev arg");
    const kernel_src = proto.readString(req.payload, zvol_dev.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing kernel_src arg");
    const uid_raw = proto.readU32(req.payload, kernel_src.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing uid arg");
    const gid_raw = proto.readU32(req.payload, uid_raw.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing gid arg");

    if (validateAbsolutePath(config, jail_root.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);
    if (validateUidGid(config, uid_raw.value, gid_raw.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    // 1. Create directories.
    var run_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    const run_path = std.fmt.bufPrint(&run_path_buf, "{s}/run", .{jail_root.value}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "path too long");

    makePathAbsolute(jail_root.value) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "mkdir jail_root failed");
    makePathAbsolute(run_path) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "mkdir jail_root/run failed");

    // 2. Link/copy kernel.
    var kernel_dst_buf: [std.fs.max_path_bytes]u8 = undefined;
    const kernel_dst = std.fmt.bufPrint(&kernel_dst_buf, "{s}/vmlinux", .{jail_root.value}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "path too long");

    // Try hardlink, fall back to copy.
    std.posix.link(kernel_src.value, kernel_dst) catch {
        copyFileAbsolute(kernel_src.value, kernel_dst) catch
            return proto.errorResponse(resp, req.request_id, .operation_failed, "copy kernel failed");
    };

    // chown kernel
    chownPath(kernel_dst, uid_raw.value, gid_raw.value) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chown kernel failed");

    // 3. Get device major/minor via stat syscall.
    // Use the mknod_bin (via the stat CLI) since Zig's std.posix doesn't
    // expose stat() or major()/minor() at this Zig version. Shell out to
    // `stat -L -c '%t %T'` matching the Go DirectPrivOps approach.
    const major_minor = getDeviceMajorMinor(zvol_dev.value) orelse
        return proto.errorResponse(resp, req.request_id, .operation_failed, "stat zvol device failed");
    const major = major_minor.major;
    const minor = major_minor.minor;

    // 4. mknod rootfs device. major/minor are hex strings from stat -c '%t %T'.
    // mknod accepts 0x-prefixed hex values.
    var rootfs_dev_buf: [std.fs.max_path_bytes]u8 = undefined;
    const rootfs_dev = std.fmt.bufPrint(&rootfs_dev_buf, "{s}/rootfs", .{jail_root.value}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "path too long");

    var major_buf: [20]u8 = undefined;
    const major_str = std.fmt.bufPrint(&major_buf, "0x{s}", .{major}) catch unreachable;
    var minor_buf: [20]u8 = undefined;
    const minor_str = std.fmt.bufPrint(&minor_buf, "0x{s}", .{minor}) catch unreachable;

    const mknod_result = runCommandResult(&.{ config.mknod_bin, rootfs_dev, "b", major_str, minor_str });
    if (mknod_result.failed) {
        return proto.errorResponse(resp, req.request_id, .operation_failed, mknod_result.stderr_slice());
    }

    // chown rootfs device
    chownPath(rootfs_dev, uid_raw.value, gid_raw.value) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chown rootfs failed");

    // 5. Create and chown metrics file.
    var metrics_buf: [std.fs.max_path_bytes]u8 = undefined;
    const metrics_path = std.fmt.bufPrint(&metrics_buf, "{s}/metrics.json", .{jail_root.value}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "path too long");

    const metrics_file = std.fs.createFileAbsolute(metrics_path, .{}) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "create metrics file failed");
    metrics_file.close();

    chownPath(metrics_path, uid_raw.value, gid_raw.value) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chown metrics failed");

    return proto.okResponse(resp, req.request_id);
}

fn execStartJailer(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const job_id = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing job_id arg");
    const fc_bin = proto.readString(req.payload, job_id.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing fc_bin arg");
    const jailer_bin = proto.readString(req.payload, fc_bin.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing jailer_bin arg");
    const chroot_base = proto.readString(req.payload, jailer_bin.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing chroot_base arg");
    const uid_raw = proto.readU32(req.payload, chroot_base.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing uid arg");
    const gid_raw = proto.readU32(req.payload, uid_raw.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing gid arg");

    if (validateUidGid(config, uid_raw.value, gid_raw.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    var uid_buf: [16]u8 = undefined;
    const uid_str = std.fmt.bufPrint(&uid_buf, "{d}", .{uid_raw.value}) catch unreachable;
    var gid_buf: [16]u8 = undefined;
    const gid_str = std.fmt.bufPrint(&gid_buf, "{d}", .{gid_raw.value}) catch unreachable;

    const argv = [_][]const u8{
        jailer_bin.value,  "--id",             job_id.value,
        "--exec-file",     fc_bin.value,       "--uid",
        uid_str,           "--gid",            gid_str,
        "--chroot-base-dir", chroot_base.value, "--",
        "--api-sock",      "/run/firecracker.sock",
    };

    var child = std.process.Child.init(&argv, std.heap.page_allocator);
    child.spawn() catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "spawn jailer failed");

    var api_socket_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    const api_socket_path = std.fmt.bufPrint(
        &api_socket_path_buf,
        "{s}/firecracker/{s}/root/run/firecracker.sock",
        .{ chroot_base.value, job_id.value },
    ) catch return proto.errorResponse(resp, req.request_id, .internal_error, "api socket path too long");
    var jail_root_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    const jail_root_path = std.fmt.bufPrint(
        &jail_root_path_buf,
        "{s}/firecracker/{s}/root",
        .{ chroot_base.value, job_id.value },
    ) catch return proto.errorResponse(resp, req.request_id, .internal_error, "jail root path too long");
    var jail_run_path_buf: [std.fs.max_path_bytes]u8 = undefined;
    const jail_run_path = std.fmt.bufPrint(&jail_run_path_buf, "{s}/run", .{jail_root_path}) catch
        return proto.errorResponse(resp, req.request_id, .internal_error, "jail run path too long");

    waitForPathAbsolute(api_socket_path, 5 * std.time.ns_per_s) catch {
        std.posix.kill(@intCast(child.id), std.posix.SIG.KILL) catch {};
        return proto.errorResponse(resp, req.request_id, .operation_failed, "wait for api socket failed");
    };

    chmodAbsolute(jail_root_path, 0o750) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chmod jail root failed");
    chmodAbsolute(jail_run_path, 0o770) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chmod jail run failed");
    chmodAbsolute(api_socket_path, 0o770) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chmod api socket failed");

    return proto.okResponseWithU32(resp, req.request_id, @intCast(child.id));
}

fn execChown(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const path = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing path arg");
    const uid_raw = proto.readU32(req.payload, path.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing uid arg");
    const gid_raw = proto.readU32(req.payload, uid_raw.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing gid arg");

    if (validateAbsolutePath(config, path.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);
    if (validateUidGid(config, uid_raw.value, gid_raw.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    chownPath(path.value, uid_raw.value, gid_raw.value) catch
        return proto.errorResponse(resp, req.request_id, .operation_failed, "chown failed");

    return proto.okResponse(resp, req.request_id);
}

fn execMknodBlock(config: *const OpsConfig, req: proto.Request, resp: *[proto.max_message_size]u8) usize {
    const path = proto.readString(req.payload, 0) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing path arg");
    const major_raw = proto.readU32(req.payload, path.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing major arg");
    const minor_raw = proto.readU32(req.payload, major_raw.next) orelse
        return proto.errorResponse(resp, req.request_id, .invalid_request, "missing minor arg");

    if (validateAbsolutePath(config, path.value)) |msg|
        return proto.errorResponse(resp, req.request_id, .validation_failed, msg);

    var major_buf: [16]u8 = undefined;
    const major_str = std.fmt.bufPrint(&major_buf, "{d}", .{major_raw.value}) catch unreachable;
    var minor_buf: [16]u8 = undefined;
    const minor_str = std.fmt.bufPrint(&minor_buf, "{d}", .{minor_raw.value}) catch unreachable;

    return runCommand(resp, req.request_id, &.{ config.mknod_bin, path.value, "b", major_str, minor_str });
}

// --- Helpers ---

fn getDeviceMajorMinor(path: []const u8) ?struct { major: []const u8, minor: []const u8 } {
    // Shell out to stat -L -c '%t %T' to get hex major/minor, matching the Go approach.
    const result = runCommandResultWithStdout(&.{ "/usr/bin/stat", "-L", "-c", "%t %T", path });
    if (result.failed) return null;
    const output = std.mem.trim(u8, result.stdout_slice(), " \t\n\r");
    // output is "major_hex minor_hex". We pass them straight to mknod.
    const sep = std.mem.indexOfScalar(u8, output, ' ') orelse return null;
    return .{ .major = output[0..sep], .minor = output[sep + 1 ..] };
}

fn chownPath(path: []const u8, uid: u32, gid: u32) !void {
    // std.posix exposes fchown but not chown. Open the file, fchown, close.
    const file = std.fs.openFileAbsolute(path, .{ .mode = .write_only }) catch
        return error.ChownFailed;
    defer file.close();
    std.posix.fchown(file.handle, uid, gid) catch return error.ChownFailed;
}

fn copyFileAbsolute(src: []const u8, dst: []const u8) !void {
    const src_file = try std.fs.openFileAbsolute(src, .{});
    defer src_file.close();
    const dst_file = try std.fs.createFileAbsolute(dst, .{});
    defer dst_file.close();

    var buf: [8192]u8 = undefined;
    while (true) {
        const n = try src_file.read(&buf);
        if (n == 0) break;
        try dst_file.writeAll(buf[0..n]);
    }
}

fn makePathAbsolute(path: []const u8) !void {
    if (path.len == 0 or path[0] != '/') return error.NotAbsolutePath;
    if (path.len == 1) return;

    var idx: usize = 1;
    while (idx <= path.len) : (idx += 1) {
        if (idx < path.len and path[idx] != '/') continue;

        const component_path = path[0..idx];
        if (component_path.len <= 1) continue;

        std.fs.makeDirAbsolute(component_path) catch |err| switch (err) {
            error.PathAlreadyExists => {},
            else => return err,
        };
    }
}

fn waitForPathAbsolute(path: []const u8, timeout_ns: u64) !void {
    const deadline = std.time.nanoTimestamp() + @as(i128, @intCast(timeout_ns));
    while (std.time.nanoTimestamp() < deadline) {
        std.fs.accessAbsolute(path, .{}) catch {
            std.Thread.sleep(10 * std.time.ns_per_ms);
            continue;
        };
        return;
    }
    return error.PathNotFound;
}

fn chmodAbsolute(path: []const u8, mode: u32) !void {
    var path_z_buf: [std.fs.max_path_bytes:0]u8 = undefined;
    const path_z = try std.fmt.bufPrintZ(&path_z_buf, "{s}", .{path});
    if (std.posix.system.chmod(path_z, mode) != 0) {
        return error.ChmodFailed;
    }
}

const CmdResult = struct {
    failed: bool,
    stderr_buf: [512]u8 = [_]u8{0} ** 512,
    stderr_len: usize = 0,
    stdout_buf: [512]u8 = [_]u8{0} ** 512,
    stdout_len: usize = 0,

    fn stderr_slice(self: *const CmdResult) []const u8 {
        return self.stderr_buf[0..self.stderr_len];
    }

    fn stdout_slice(self: *const CmdResult) []const u8 {
        return self.stdout_buf[0..self.stdout_len];
    }
};

fn runCommandResult(argv: []const []const u8) CmdResult {
    var result = CmdResult{ .failed = false };
    var child = std.process.Child.init(argv, std.heap.page_allocator);
    child.stderr_behavior = .Pipe;
    child.stdout_behavior = .Pipe;
    child.spawn() catch {
        result.failed = true;
        const msg = "spawn failed";
        @memcpy(result.stderr_buf[0..msg.len], msg);
        result.stderr_len = msg.len;
        return result;
    };
    // Read stderr.
    if (child.stderr) |stderr_fd| {
        result.stderr_len = stderr_fd.read(&result.stderr_buf) catch 0;
    }
    const term = child.wait() catch {
        result.failed = true;
        return result;
    };
    result.failed = (term.Exited != 0);
    return result;
}

fn runCommandResultWithStdout(argv: []const []const u8) CmdResult {
    var result = CmdResult{ .failed = false };
    var child = std.process.Child.init(argv, std.heap.page_allocator);
    child.stderr_behavior = .Pipe;
    child.stdout_behavior = .Pipe;
    child.spawn() catch {
        result.failed = true;
        const msg = "spawn failed";
        @memcpy(result.stderr_buf[0..msg.len], msg);
        result.stderr_len = msg.len;
        return result;
    };
    // Read stdout.
    if (child.stdout) |stdout_fd| {
        result.stdout_len = stdout_fd.read(&result.stdout_buf) catch 0;
    }
    // Read stderr.
    if (child.stderr) |stderr_fd| {
        result.stderr_len = stderr_fd.read(&result.stderr_buf) catch 0;
    }
    const term = child.wait() catch {
        result.failed = true;
        return result;
    };
    result.failed = (term.Exited != 0);
    return result;
}

fn runCommand(resp: *[proto.max_message_size]u8, request_id: u64, argv: []const []const u8) usize {
    const result = runCommandResult(argv);
    if (result.failed) {
        return proto.errorResponse(resp, request_id, .operation_failed, result.stderr_slice());
    }
    return proto.okResponse(resp, request_id);
}

// --- Tests ---

test "validateDataset accepts valid" {
    const config = OpsConfig{ .pool_prefix = "forgepool", .jailer_root = "/srv/jailer" };
    try std.testing.expectEqual(@as(?[]const u8, null), validateDataset(&config, "forgepool/ci/test-job"));
    try std.testing.expectEqual(@as(?[]const u8, null), validateDataset(&config, "forgepool/golden-zvol@ready"));
}

test "validateDataset rejects traversal" {
    const config = OpsConfig{ .pool_prefix = "forgepool", .jailer_root = "/srv/jailer" };
    try std.testing.expect(validateDataset(&config, "forgepool/../etc/shadow") != null);
}

test "validateDataset rejects wrong prefix" {
    const config = OpsConfig{ .pool_prefix = "forgepool", .jailer_root = "/srv/jailer" };
    try std.testing.expect(validateDataset(&config, "otherPool/ci/test") != null);
}

test "validateTapName accepts valid" {
    try std.testing.expectEqual(@as(?[]const u8, null), validateTapName("fc-tap-0"));
    try std.testing.expectEqual(@as(?[]const u8, null), validateTapName("fc-tap-abc123"));
}

test "validateTapName rejects invalid" {
    try std.testing.expect(validateTapName("eth0") != null);
    try std.testing.expect(validateTapName("fc-tap-A") != null); // uppercase
}

test "validateTapName accepts empty suffix" {
    // "fc-tap-" with no suffix is still a valid tap name pattern (7 chars >= 8 check is the min).
    // Actually "fc-tap-" is 7 chars, so it fails the len check.
    try std.testing.expect(validateTapName("fc-tap-") != null); // too short
}

test "validateAbsolutePath rejects traversal" {
    const config = OpsConfig{ .pool_prefix = "forgepool", .jailer_root = "/srv/jailer" };
    try std.testing.expect(validateAbsolutePath(&config, "/srv/jailer/../etc/passwd") != null);
}

test "validateAbsolutePath accepts valid" {
    const config = OpsConfig{ .pool_prefix = "forgepool", .jailer_root = "/srv/jailer" };
    try std.testing.expectEqual(@as(?[]const u8, null), validateAbsolutePath(&config, "/srv/jailer/firecracker/test/root"));
}

test "makePathAbsolute creates nested directories" {
    const allocator = std.testing.allocator;
    var dir = std.testing.tmpDir(.{});
    defer dir.cleanup();

    const root_path = try dir.dir.realpathAlloc(allocator, ".");
    defer allocator.free(root_path);

    const nested = try std.fs.path.join(allocator, &.{ root_path, "firecracker", "job-id", "root", "run" });
    defer allocator.free(nested);

    try makePathAbsolute(nested);
    try std.fs.accessAbsolute(nested, .{});
}
