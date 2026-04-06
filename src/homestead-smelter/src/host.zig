const std = @import("std");
const linux = std.os.linux;
const hs = @import("homestead_smelter");
const hostc = @import("host_core.zig");
const hostp = @import("host_proto.zig");

const default_jailer_root = "/srv/jailer/firecracker";
const bridge_ack_buffer_bytes: usize = 256;
const bridge_stream_buffer_bytes: usize = hs.frame_size * 2;
const consumer_batch_size: usize = 64;
const consumer_request_buffer_bytes: usize = hostp.request_size;
const epoll_max_events: usize = 64;
const max_bridge_cmd_bytes: usize = 32;
const max_consumers: usize = 16;
const max_runtime_vms: usize = 256;
const reconnect_backoff_ns: u64 = 200 * std.time.ns_per_ms;
const timer_tick_ns: u64 = 100 * std.time.ns_per_ms;
const discovery_period_ns: u64 = 250 * std.time.ns_per_ms;
const event_ring_capacity: usize = 131072;
const usage =
    \\Usage:
    \\  homestead-smelter-host serve --listen-uds PATH [--jailer-root PATH]
    \\  homestead-smelter-host snapshot --control-uds PATH
    \\  homestead-smelter-host check-live --control-uds PATH --job-id UUID
    \\
    \\`serve` runs the long-lived host agent, discovers Firecracker VMs, opens one
    \\binary telemetry stream per guest on the fixed vsock port 10790, and exposes
    \\a local AF_UNIX SEQPACKET socket.
    \\`snapshot` attaches, prints the current binary host view in human-readable
    \\lines, and exits after `SNAPSHOT_END`.
    \\`check-live` succeeds when the named job has both hello and sample telemetry
    \\in the current snapshot.
    \\
    \\Options:
    \\  --listen-uds PATH   Host-agent control socket path
    \\  --control-uds PATH  Host-agent control socket path
    \\  --jailer-root PATH  Firecracker jail root to scan (default: /srv/jailer/firecracker)
    \\  --job-id UUID       CI job UUID for `check-live`
    \\  --help              Show this help text
    \\
;

const Mode = enum {
    serve,
    snapshot,
    check_live,
};

const Config = struct {
    mode: Mode,
    control_uds: []const u8 = "",
    jailer_root: []const u8 = default_jailer_root,
    job_id: []const u8 = "",
};

const HostCore = hostc.HostCoreType(max_runtime_vms, event_ring_capacity);

const FdKind = enum(u8) {
    listener = 1,
    timer = 2,
    bridge = 3,
    consumer = 4,
};

const BridgeStage = enum {
    connecting,
    handshaking,
    streaming,
};

const ConsumerStage = enum {
    waiting_request,
    snapshot,
    live,
};

const SnapshotCursorPhase = enum {
    hello,
    sample,
    disconnect,
    end,
};

const RuntimeVm = struct {
    used: bool = false,
    seen_in_scan: bool = false,
    job_id: hostc.JobId = .{},
    uds_path: hostc.UdsPath = .{},
    fd: ?std.posix.fd_t = null,
    stage: BridgeStage = .connecting,
    token: ?HostCore.StreamToken = null,
    retry_due_ns: u64 = 0,
    cmd_len: usize = 0,
    cmd_sent: usize = 0,
    cmd_buf: [max_bridge_cmd_bytes]u8 = [_]u8{0} ** max_bridge_cmd_bytes,
    ack_len: usize = 0,
    ack_buf: [bridge_ack_buffer_bytes]u8 = [_]u8{0} ** bridge_ack_buffer_bytes,
    stream_len: usize = 0,
    stream_buf: [bridge_stream_buffer_bytes]u8 = [_]u8{0} ** bridge_stream_buffer_bytes,
};

const Consumer = struct {
    used: bool = false,
    fd: ?std.posix.fd_t = null,
    stage: ConsumerStage = .waiting_request,
    wants_write: bool = false,
    request_len: usize = 0,
    request_buf: [consumer_request_buffer_bytes]u8 = [_]u8{0} ** consumer_request_buffer_bytes,
    snapshot_vms: [max_runtime_vms]HostCore.SnapshotVm = [_]HostCore.SnapshotVm{.{}} ** max_runtime_vms,
    snapshot_count: usize = 0,
    snapshot_index: usize = 0,
    snapshot_phase: SnapshotCursorPhase = .hello,
    snapshot_next_event_seq: u64 = 1,
    live_cursor: HostCore.Cursor = .{},
    event_buf: [consumer_batch_size]HostCore.EventRecord = undefined,
    event_count: usize = 0,
    event_index: usize = 0,
    pending_packet: ?hostp.Packet = null,
};

const Runtime = struct {
    control_uds: []const u8,
    jailer_root: []const u8,
    epoll_fd: std.posix.fd_t,
    listener_fd: std.posix.fd_t,
    timer_fd: std.posix.fd_t,
    next_discovery_ns: u64 = 0,
    core: HostCore = .{},
    vms: [max_runtime_vms]RuntimeVm = [_]RuntimeVm{.{}} ** max_runtime_vms,
    consumers: [max_consumers]Consumer = [_]Consumer{.{}} ** max_consumers,

    fn init(self: *Runtime, control_uds: []const u8, jailer_root: []const u8) !void {
        const epoll_fd = try std.posix.epoll_create1(linux.EPOLL.CLOEXEC);
        errdefer std.posix.close(epoll_fd);

        const listener_fd = try bindListener(control_uds);
        errdefer std.posix.close(listener_fd);

        const timer_fd = try std.posix.timerfd_create(.MONOTONIC, .{
            .CLOEXEC = true,
            .NONBLOCK = true,
        });
        errdefer std.posix.close(timer_fd);
        try armPeriodicTimer(timer_fd, timer_tick_ns);

        self.* = .{
            .control_uds = control_uds,
            .jailer_root = jailer_root,
            .epoll_fd = epoll_fd,
            .listener_fd = listener_fd,
            .timer_fd = timer_fd,
            .next_discovery_ns = 0,
        };
        try self.registerFd(listener_fd, fdToken(.listener, 0), linux.EPOLL.IN | linux.EPOLL.ERR | linux.EPOLL.HUP);
        try self.registerFd(timer_fd, fdToken(.timer, 0), linux.EPOLL.IN | linux.EPOLL.ERR | linux.EPOLL.HUP);
    }

    fn deinit(self: *Runtime) void {
        for (&self.vms) |*vm| self.closeBridge(vm);
        for (&self.consumers) |*consumer| self.closeConsumer(consumer);
        std.posix.close(self.timer_fd);
        std.posix.close(self.listener_fd);
        std.posix.close(self.epoll_fd);
        std.fs.deleteFileAbsolute(self.control_uds) catch {};
    }

    fn run(self: *Runtime) !void {
        var events: [epoll_max_events]linux.epoll_event = undefined;
        while (true) {
            const count = std.posix.epoll_wait(self.epoll_fd, events[0..], -1);
            var core_changed = false;

            for (events[0..count]) |event| {
                const decoded = decodeToken(event.data.u64);
                switch (decoded.kind) {
                    .listener => try self.acceptConsumers(),
                    .timer => core_changed = (try self.handleTimer()) or core_changed,
                    .bridge => core_changed = (try self.handleBridgeEvent(decoded.index, event.events)) or core_changed,
                    .consumer => self.handleConsumerEvent(decoded.index, event.events),
                }
            }

            if (core_changed) self.flushConsumers();
        }
    }

    fn handleTimer(self: *Runtime) !bool {
        var ticks: [8]u8 = undefined;
        _ = try std.posix.read(self.timer_fd, ticks[0..]);

        const now_ns = try hs.monotonicNowNs();
        var core_changed = false;
        if (now_ns >= self.next_discovery_ns) {
            core_changed = (try self.runDiscovery()) or core_changed;
            self.next_discovery_ns = now_ns + discovery_period_ns;
        }
        self.startDueBridgeConnections(now_ns);
        return core_changed;
    }

    fn runDiscovery(self: *Runtime) !bool {
        for (&self.vms) |*vm| {
            if (!vm.used) continue;
            vm.seen_in_scan = false;
        }

        var root_dir = std.fs.openDirAbsolute(self.jailer_root, .{ .iterate = true }) catch |err| switch (err) {
            error.FileNotFound => {
                return try self.finalizeDiscovery();
            },
            else => return err,
        };
        defer root_dir.close();

        var it = root_dir.iterate();
        while (try it.next()) |entry| {
            if (entry.kind != .directory) continue;

            var uds_buf: [std.fs.max_path_bytes]u8 = undefined;
            const uds_path = try std.fmt.bufPrint(
                uds_buf[0..],
                "{s}/{s}/root/run/forge-control.sock",
                .{ self.jailer_root, entry.name },
            );
            std.fs.accessAbsolute(uds_path, .{}) catch |err| switch (err) {
                error.FileNotFound => continue,
                else => return err,
            };

            try self.upsertDiscoveredVm(entry.name, uds_path);
        }

        return try self.finalizeDiscovery();
    }

    fn upsertDiscoveredVm(self: *Runtime, job_id: []const u8, uds_path: []const u8) !void {
        const now_ns = try hs.monotonicNowNs();
        const index = try self.findOrAllocVm(job_id);
        const vm = &self.vms[index];
        const path_changed = !vm.used or !vm.uds_path.eql(uds_path);
        const was_present = vm.used and vm.seen_in_scan;

        if (!vm.used) {
            vm.used = true;
            try vm.job_id.set(job_id);
        }
        vm.seen_in_scan = true;

        if (path_changed) {
            try vm.uds_path.set(uds_path);
            self.closeBridge(vm);
        }

        try self.core.discover(job_id, uds_path);

        if (path_changed or !was_present) {
            vm.retry_due_ns = now_ns;
        }
    }

    fn finalizeDiscovery(self: *Runtime) !bool {
        var core_changed = false;
        for (&self.vms) |*vm| {
            if (!vm.used or vm.seen_in_scan) continue;
            self.closeBridge(vm);
            try self.core.markGone(vm.job_id.slice());
            vm.* = .{};
            core_changed = true;
        }
        return core_changed;
    }

    fn startDueBridgeConnections(self: *Runtime, now_ns: u64) void {
        for (0..self.vms.len) |index| {
            const vm = &self.vms[index];
            if (!vm.used) continue;
            if (vm.fd != null) continue;
            if (vm.retry_due_ns > now_ns) continue;
            self.startBridgeConnection(index, now_ns);
        }
    }

    fn startBridgeConnection(self: *Runtime, index: usize, now_ns: u64) void {
        var vm = &self.vms[index];
        const token = self.core.beginStream(vm.job_id.slice()) catch |err| {
            std.log.err("beginStream failed for {s}: {s}", .{ vm.job_id.slice(), @errorName(err) });
            vm.retry_due_ns = now_ns + reconnect_backoff_ns;
            return;
        };

        var address = std.net.Address.initUnix(vm.uds_path.slice()) catch |err| {
            _ = self.core.recordDisconnect(token, .connect_failed);
            std.log.err("invalid bridge path for {s}: {s}", .{ vm.job_id.slice(), @errorName(err) });
            vm.retry_due_ns = now_ns + reconnect_backoff_ns;
            return;
        };

        const fd = std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC | std.posix.SOCK.NONBLOCK, 0) catch |err| {
            _ = self.core.recordDisconnect(token, .connect_failed);
            std.log.err("socket failed for {s}: {s}", .{ vm.job_id.slice(), @errorName(err) });
            vm.retry_due_ns = now_ns + reconnect_backoff_ns;
            return;
        };
        errdefer std.posix.close(fd);

        const state = connectNonblocking(fd, &address) catch |err| {
            _ = self.core.recordDisconnect(token, .connect_failed);
            std.log.err("connect failed for {s}: {s}", .{ vm.job_id.slice(), @errorName(err) });
            vm.retry_due_ns = now_ns + reconnect_backoff_ns;
            return;
        };

        vm.fd = fd;
        vm.token = token;
        vm.stage = if (state == .connected) .handshaking else .connecting;
        vm.retry_due_ns = now_ns + reconnect_backoff_ns;
        vm.cmd_sent = 0;
        vm.ack_len = 0;
        vm.stream_len = 0;
        vm.cmd_len = (std.fmt.bufPrint(vm.cmd_buf[0..], "CONNECT {d}\n", .{hs.default_guest_port}) catch unreachable).len;
        self.registerFd(fd, fdToken(.bridge, index), bridgeInterest(vm.stage)) catch |err| {
            _ = self.core.recordDisconnect(token, .connect_failed);
            std.log.err("epoll add failed for {s}: {s}", .{ vm.job_id.slice(), @errorName(err) });
            self.closeBridge(vm);
            vm.retry_due_ns = now_ns + reconnect_backoff_ns;
            return;
        };
    }

    fn acceptConsumers(self: *Runtime) !void {
        while (true) {
            const fd = std.posix.accept(self.listener_fd, null, null, std.posix.SOCK.CLOEXEC | std.posix.SOCK.NONBLOCK) catch |err| switch (err) {
                error.WouldBlock => return,
                else => return err,
            };
            errdefer std.posix.close(fd);

            const index = self.allocConsumer() catch {
                std.posix.close(fd);
                continue;
            };
            const consumer = &self.consumers[index];
            consumer.* = .{
                .used = true,
                .fd = fd,
            };
            try self.registerFd(fd, fdToken(.consumer, index), consumerInterest(consumer));
        }
    }

    fn handleBridgeEvent(self: *Runtime, index: usize, events: u32) !bool {
        if (index >= self.vms.len) return false;
        const vm = &self.vms[index];
        if (!vm.used or vm.fd == null) return false;

        var core_changed = false;
        if ((events & linux.EPOLL.OUT) != 0) {
            core_changed = (try self.handleBridgeWritable(index)) or core_changed;
        }
        if ((events & linux.EPOLL.IN) != 0) {
            core_changed = (try self.handleBridgeReadable(index)) or core_changed;
        }
        if ((events & (linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP)) != 0 and vm.fd != null) {
            self.failBridge(index, .bridge_closed);
            core_changed = true;
        }
        return core_changed;
    }

    fn handleBridgeWritable(self: *Runtime, index: usize) !bool {
        var vm = &self.vms[index];
        const fd = vm.fd orelse return false;

        switch (vm.stage) {
            .connecting => {
                std.posix.getsockoptError(fd) catch {
                    self.failBridge(index, .connect_failed);
                    return true;
                };
                vm.stage = .handshaking;
                try self.modifyFd(fd, fdToken(.bridge, index), bridgeInterest(vm.stage));
                return false;
            },
            .handshaking => {
                while (vm.cmd_sent < vm.cmd_len) {
                    const sent = std.posix.write(fd, vm.cmd_buf[vm.cmd_sent..vm.cmd_len]) catch |err| switch (err) {
                        error.WouldBlock => return false,
                        else => {
                            self.failBridge(index, .connect_failed);
                            return true;
                        },
                    };
                    if (sent == 0) {
                        self.failBridge(index, .connect_failed);
                        return true;
                    }
                    vm.cmd_sent += sent;
                }
                try self.modifyFd(fd, fdToken(.bridge, index), bridgeInterest(vm.stage));
                return false;
            },
            .streaming => return false,
        }
    }

    fn handleBridgeReadable(self: *Runtime, index: usize) !bool {
        var vm = &self.vms[index];
        const fd = vm.fd orelse return false;

        switch (vm.stage) {
            .connecting => return false,
            .handshaking => {
                const n = std.posix.read(fd, vm.ack_buf[vm.ack_len..]) catch |err| switch (err) {
                    error.WouldBlock => return false,
                    else => {
                        self.failBridge(index, .connect_failed);
                        return true;
                    },
                };
                if (n == 0) {
                    self.failBridge(index, .bridge_closed);
                    return true;
                }
                vm.ack_len += n;
                if (vm.ack_len == vm.ack_buf.len and std.mem.indexOfScalar(u8, vm.ack_buf[0..vm.ack_len], '\n') == null) {
                    self.failBridge(index, .connect_failed);
                    return true;
                }

                const newline_index = std.mem.indexOfScalar(u8, vm.ack_buf[0..vm.ack_len], '\n') orelse return false;
                const line_raw = vm.ack_buf[0..newline_index];
                const line = std.mem.trimRight(u8, line_raw, "\r");
                if (!std.mem.startsWith(u8, line, "OK ")) {
                    self.failBridge(index, .connect_failed);
                    return true;
                }

                const remaining_start = newline_index + 1;
                if (remaining_start < vm.ack_len) {
                    const remaining = vm.ack_len - remaining_start;
                    if (remaining > vm.stream_buf.len) {
                        self.failBridge(index, .decode_failed);
                        return true;
                    }
                    @memcpy(vm.stream_buf[0..remaining], vm.ack_buf[remaining_start..vm.ack_len]);
                    vm.stream_len = remaining;
                } else {
                    vm.stream_len = 0;
                }
                vm.ack_len = 0;
                vm.stage = .streaming;
                if (vm.token) |token| _ = self.core.recordConnected(token);
                try self.modifyFd(fd, fdToken(.bridge, index), bridgeInterest(vm.stage));
                return self.processBridgeFrames(vm);
            },
            .streaming => {
                const n = std.posix.read(fd, vm.stream_buf[vm.stream_len..]) catch |err| switch (err) {
                    error.WouldBlock => return false,
                    else => {
                        self.failBridge(index, .decode_failed);
                        return true;
                    },
                };
                if (n == 0) {
                    self.failBridge(index, .bridge_closed);
                    return true;
                }
                vm.stream_len += n;
                return self.processBridgeFrames(vm);
            },
        }
    }

    fn processBridgeFrames(self: *Runtime, vm: *RuntimeVm) bool {
        var core_changed = false;
        while (vm.stream_len >= hs.frame_size) {
            var frame_buf: [hs.frame_size]u8 = undefined;
            @memcpy(frame_buf[0..], vm.stream_buf[0..hs.frame_size]);
            const kind = hs.decodeFrameKind(&frame_buf) catch {
                if (vm.token) |_| self.failBridgeByVm(vm, .decode_failed);
                return true;
            };
            switch (kind) {
                .hello => {
                    if (vm.token) |token| {
                        const hello = hs.decodeHelloFrame(&frame_buf) catch {
                            self.failBridgeByVm(vm, .decode_failed);
                            return true;
                        };
                        _ = self.core.recordHello(token, hello);
                        core_changed = true;
                    }
                },
                .sample => {
                    if (vm.token) |token| {
                        const sample = hs.decodeSampleFrame(&frame_buf) catch {
                            self.failBridgeByVm(vm, .decode_failed);
                            return true;
                        };
                        _ = self.core.recordSample(token, sample);
                        core_changed = true;
                    }
                },
            }
            const remaining = vm.stream_len - hs.frame_size;
            if (remaining > 0) {
                std.mem.copyForwards(u8, vm.stream_buf[0..remaining], vm.stream_buf[hs.frame_size..vm.stream_len]);
            }
            vm.stream_len = remaining;
        }
        return core_changed;
    }

    fn failBridge(self: *Runtime, index: usize, reason: hostc.DisconnectReason) void {
        const vm = &self.vms[index];
        self.failBridgeByVm(vm, reason);
    }

    fn failBridgeByVm(self: *Runtime, vm: *RuntimeVm, reason: hostc.DisconnectReason) void {
        if (vm.token) |token| _ = self.core.recordDisconnect(token, reason);
        self.closeBridge(vm);
        vm.retry_due_ns = (hs.monotonicNowNs() catch 0) + reconnect_backoff_ns;
    }

    fn handleConsumerEvent(self: *Runtime, index: usize, events: u32) void {
        if (index >= self.consumers.len) return;
        const consumer = &self.consumers[index];
        if (!consumer.used or consumer.fd == null) return;

        if ((events & (linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP)) != 0) {
            self.closeConsumer(consumer);
            return;
        }
        if ((events & linux.EPOLL.IN) != 0) {
            self.readConsumerRequest(index);
        }
        if ((events & linux.EPOLL.OUT) != 0) {
            self.flushConsumer(index);
        }
    }

    fn readConsumerRequest(self: *Runtime, index: usize) void {
        var consumer = &self.consumers[index];
        const fd = consumer.fd orelse return;
        if (consumer.stage != .waiting_request) {
            self.closeConsumer(consumer);
            return;
        }

        const n = std.posix.recv(fd, consumer.request_buf[consumer.request_len..], 0) catch |err| switch (err) {
            error.WouldBlock => return,
            else => {
                self.closeConsumer(consumer);
                return;
            },
        };
        if (n == 0) {
            self.closeConsumer(consumer);
            return;
        }
        consumer.request_len += n;
        if (consumer.request_len < hostp.request_size) return;
        if (consumer.request_len != hostp.request_size) {
            self.closeConsumer(consumer);
            return;
        }

        const request = hostp.decodeRequest(@ptrCast(&consumer.request_buf)) catch {
            self.closeConsumer(consumer);
            return;
        };
        if (request.kind != .attach) {
            self.closeConsumer(consumer);
            return;
        }

        const current_snapshot = self.core.snapshot(consumer.snapshot_vms[0..]);
        consumer.snapshot_count = current_snapshot.vms.len;
        consumer.snapshot_index = 0;
        consumer.snapshot_phase = .hello;
        consumer.snapshot_next_event_seq = current_snapshot.next_event_seq;
        consumer.live_cursor = self.core.cursorFromSeq(current_snapshot.next_event_seq);
        consumer.event_count = 0;
        consumer.event_index = 0;
        consumer.pending_packet = null;
        consumer.stage = .snapshot;
        consumer.wants_write = true;
        self.flushConsumer(index);
    }

    fn flushConsumers(self: *Runtime) void {
        for (0..self.consumers.len) |index| {
            if (!self.consumers[index].used) continue;
            self.flushConsumer(index);
        }
    }

    fn flushConsumer(self: *Runtime, index: usize) void {
        var consumer = &self.consumers[index];
        const fd = consumer.fd orelse return;

        while (true) {
            if (consumer.pending_packet == null) {
                consumer.pending_packet = self.nextConsumerPacket(consumer) catch {
                    self.closeConsumer(consumer);
                    return;
                };
                if (consumer.pending_packet == null) break;
            }

            const encoded = hostp.encodePacket(consumer.pending_packet.?);
            const sent = std.posix.send(fd, encoded[0..], 0) catch |err| switch (err) {
                error.WouldBlock => {
                    consumer.wants_write = true;
                    self.updateConsumerInterest(index) catch self.closeConsumer(consumer);
                    return;
                },
                else => {
                    self.closeConsumer(consumer);
                    return;
                },
            };
            if (sent != hostp.packet_size) {
                self.closeConsumer(consumer);
                return;
            }
            consumer.pending_packet = null;
        }

        consumer.wants_write = false;
        self.updateConsumerInterest(index) catch self.closeConsumer(consumer);
    }

    fn nextConsumerPacket(self: *Runtime, consumer: *Consumer) !?hostp.Packet {
        switch (consumer.stage) {
            .waiting_request => return null,
            .snapshot => {
                if (try nextSnapshotPacket(consumer)) |packet| return packet;
                consumer.stage = .live;
                return self.nextConsumerPacket(consumer);
            },
            .live => {
                while (true) {
                    if (consumer.event_index >= consumer.event_count) {
                        const drained = self.core.drain(&consumer.live_cursor, consumer.event_buf[0..]);
                        if (drained.status == .overflow) return error.ConsumerLagged;
                        consumer.event_count = drained.count;
                        consumer.event_index = 0;
                        if (drained.count == 0) return null;
                    }
                    const record = consumer.event_buf[consumer.event_index];
                    consumer.event_index += 1;
                    if (try packetFromEvent(record)) |packet| return packet;
                }
            },
        }
    }

    fn findOrAllocVm(self: *Runtime, job_id: []const u8) !usize {
        for (self.vms, 0..) |vm, index| {
            if (!vm.used) continue;
            if (vm.job_id.eql(job_id)) return index;
        }
        for (&self.vms, 0..) |*vm, index| {
            if (vm.used) continue;
            return index;
        }
        return error.FleetFull;
    }

    fn allocConsumer(self: *Runtime) !usize {
        for (&self.consumers, 0..) |*consumer, index| {
            if (consumer.used) continue;
            return index;
        }
        return error.ConsumerCapacityExceeded;
    }

    fn closeBridge(self: *Runtime, vm: *RuntimeVm) void {
        _ = self;
        if (vm.fd) |fd| std.posix.close(fd);
        vm.fd = null;
        vm.token = null;
        vm.cmd_len = 0;
        vm.cmd_sent = 0;
        vm.ack_len = 0;
        vm.stream_len = 0;
        vm.stage = .connecting;
    }

    fn closeConsumer(self: *Runtime, consumer: *Consumer) void {
        _ = self;
        if (consumer.fd) |fd| std.posix.close(fd);
        consumer.* = .{};
    }

    fn registerFd(self: *Runtime, fd: std.posix.fd_t, token: u64, events: u32) !void {
        var event = linux.epoll_event{
            .events = events,
            .data = .{ .u64 = token },
        };
        try std.posix.epoll_ctl(self.epoll_fd, linux.EPOLL.CTL_ADD, fd, &event);
    }

    fn modifyFd(self: *Runtime, fd: std.posix.fd_t, token: u64, events: u32) !void {
        var event = linux.epoll_event{
            .events = events,
            .data = .{ .u64 = token },
        };
        try std.posix.epoll_ctl(self.epoll_fd, linux.EPOLL.CTL_MOD, fd, &event);
    }

    fn updateConsumerInterest(self: *Runtime, index: usize) !void {
        const consumer = &self.consumers[index];
        const fd = consumer.fd orelse return;
        try self.modifyFd(fd, fdToken(.consumer, index), consumerInterest(consumer));
    }
};

pub fn main() !void {
    const allocator = std.heap.page_allocator;
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    const config = parseArgs(args) catch |err| switch (err) {
        error.ShowUsage => {
            try std.fs.File.stdout().writeAll(usage);
            return;
        },
        else => return err,
    };

    switch (config.mode) {
        .serve => try serve(config.control_uds, config.jailer_root),
        .snapshot => try snapshot(allocator, config.control_uds),
        .check_live => try checkLive(allocator, config.control_uds, config.job_id),
    }
}

fn parseArgs(args: []const []const u8) !Config {
    if (args.len < 2) return error.ShowUsage;

    var config = Config{
        .mode = switchMode(args[1]) orelse return error.ShowUsage,
    };

    var index: usize = 2;
    while (index < args.len) : (index += 1) {
        const arg = args[index];
        if (std.mem.eql(u8, arg, "--help")) return error.ShowUsage;
        if (std.mem.eql(u8, arg, "--listen-uds") or std.mem.eql(u8, arg, "--control-uds")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.control_uds = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--jailer-root")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.jailer_root = args[index];
            continue;
        }
        if (std.mem.eql(u8, arg, "--job-id")) {
            index += 1;
            if (index >= args.len) return error.MissingOptionValue;
            config.job_id = args[index];
            continue;
        }

        std.log.err("unknown argument: {s}", .{arg});
        return error.ShowUsage;
    }

    switch (config.mode) {
        .serve, .snapshot, .check_live => {
            if (config.control_uds.len == 0) {
                std.log.err("--listen-uds/--control-uds is required", .{});
                return error.ShowUsage;
            }
        },
    }
    if (config.mode == .check_live and config.job_id.len == 0) {
        std.log.err("--job-id is required for check-live", .{});
        return error.ShowUsage;
    }

    return config;
}

fn switchMode(value: []const u8) ?Mode {
    if (std.mem.eql(u8, value, "serve")) return .serve;
    if (std.mem.eql(u8, value, "snapshot")) return .snapshot;
    if (std.mem.eql(u8, value, "check-live")) return .check_live;
    return null;
}

fn serve(control_uds: []const u8, jailer_root: []const u8) !void {
    const allocator = std.heap.page_allocator;
    const runtime = try allocator.create(Runtime);
    defer allocator.destroy(runtime);
    try runtime.init(control_uds, jailer_root);
    defer runtime.deinit();

    std.log.info("homestead-smelter host agent listening on {s}", .{control_uds});
    try runtime.run();
}

fn snapshot(allocator: std.mem.Allocator, control_uds: []const u8) !void {
    const packets = try requestAttachPackets(allocator, control_uds);
    defer allocator.free(packets);

    for (packets) |packet| {
        try writeSnapshotLine(packet);
    }
}

fn checkLive(allocator: std.mem.Allocator, control_uds: []const u8, job_id_text: []const u8) !void {
    const job_id = try hs.parseUuid(job_id_text);
    const packets = try requestAttachPackets(allocator, control_uds);
    defer allocator.free(packets);

    var saw_hello = false;
    var saw_sample = false;
    for (packets) |packet| {
        if (!std.mem.eql(u8, packet.header.job_id[0..], job_id[0..])) continue;
        switch (packet.header.kind) {
            .hello => saw_hello = true,
            .sample => saw_sample = true,
            else => {},
        }
    }

    if (!saw_hello or !saw_sample) return error.JobNotLive;
    var buf: [96]u8 = undefined;
    const line = try std.fmt.bufPrint(&buf, "LIVE {s}\n", .{job_id_text});
    try std.fs.File.stdout().writeAll(line);
}

fn openControlFd(control_uds: []const u8) !std.posix.fd_t {
    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.SEQPACKET | std.posix.SOCK.CLOEXEC, 0);
    errdefer std.posix.close(fd);
    try std.posix.connect(fd, &address.any, address.getOsSockLen());
    return fd;
}

fn requestAttachPackets(allocator: std.mem.Allocator, control_uds: []const u8) ![]hostp.Packet {
    const fd = try openControlFd(control_uds);
    defer std.posix.close(fd);

    const request = hostp.encodeRequest(.{ .kind = .attach });
    const sent = try std.posix.send(fd, request[0..], 0);
    if (sent != hostp.request_size) return error.ShortWrite;

    var packets = try std.ArrayList(hostp.Packet).initCapacity(allocator, 8);
    defer packets.deinit(allocator);

    while (true) {
        var buf: [hostp.packet_size]u8 = undefined;
        const n = try std.posix.recv(fd, buf[0..], 0);
        if (n == 0) return error.EndOfStream;
        if (n != hostp.packet_size) return error.ShortRead;
        const packet = try hostp.decodePacket(&buf);
        try packets.append(allocator, packet);
        if (packet.header.kind == .snapshot_end) break;
    }

    return packets.toOwnedSlice(allocator);
}

fn writeSnapshotLine(packet: hostp.Packet) !void {
    var line_buf: [512]u8 = undefined;
    switch (packet.header.kind) {
        .hello => {
            const hello = try hs.decodeHelloFrame(&packet.payload);
            const job_id = hs.formatUuid(packet.header.job_id);
            const boot_id = hs.formatUuid(hello.boot_id);
            const line = try std.fmt.bufPrint(&line_buf,
                "HELLO job_id={s} stream_generation={d} host_seq={d} guest_seq={d} boot_id={s} mem_total_kb={d}\n",
                .{ job_id[0..], packet.header.stream_generation, packet.header.host_seq, hello.seq, boot_id[0..], hello.mem_total_kb },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .sample => {
            const sample = try hs.decodeSampleFrame(&packet.payload);
            const job_id = hs.formatUuid(packet.header.job_id);
            const line = try std.fmt.bufPrint(&line_buf,
                "SAMPLE job_id={s} stream_generation={d} host_seq={d} guest_seq={d} mem_available_kb={d} cpu_user_ticks={d}\n",
                .{ job_id[0..], packet.header.stream_generation, packet.header.host_seq, sample.seq, sample.mem_available_kb, sample.cpu_user_ticks },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .disconnect => {
            const job_id = hs.formatUuid(packet.header.job_id);
            const reason = try hostp.decodeDisconnectPayload(&packet.payload);
            const line = try std.fmt.bufPrint(&line_buf,
                "DISCONNECT job_id={s} stream_generation={d} host_seq={d} reason={s}\n",
                .{ job_id[0..], packet.header.stream_generation, packet.header.host_seq, hostc.disconnectReasonText(reason) },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .vm_gone => {
            const job_id = hs.formatUuid(packet.header.job_id);
            const line = try std.fmt.bufPrint(&line_buf,
                "VM_GONE job_id={s} host_seq={d}\n",
                .{ job_id[0..], packet.header.host_seq },
            );
            try std.fs.File.stdout().writeAll(line);
        },
        .snapshot_end => {
            const line = try std.fmt.bufPrint(&line_buf, "SNAPSHOT_END host_seq={d}\n", .{packet.header.host_seq});
            try std.fs.File.stdout().writeAll(line);
        },
    }
}

fn bindListener(control_uds: []const u8) !std.posix.fd_t {
    if (std.fs.path.dirname(control_uds)) |parent| {
        try std.fs.cwd().makePath(parent);
    }
    std.fs.deleteFileAbsolute(control_uds) catch |err| switch (err) {
        error.FileNotFound => {},
        else => return err,
    };

    var address = try std.net.Address.initUnix(control_uds);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.SEQPACKET | std.posix.SOCK.CLOEXEC | std.posix.SOCK.NONBLOCK, 0);
    errdefer std.posix.close(fd);
    try std.posix.bind(fd, &address.any, address.getOsSockLen());
    try std.posix.listen(fd, 16);
    return fd;
}

fn armPeriodicTimer(fd: std.posix.fd_t, interval_ns: u64) !void {
    var spec = linux.itimerspec{
        .it_interval = nsToLinuxTimespec(interval_ns),
        .it_value = nsToLinuxTimespec(interval_ns),
    };
    try std.posix.timerfd_settime(fd, .{}, &spec, null);
}

fn nsToLinuxTimespec(ns: u64) linux.timespec {
    return .{
        .sec = @intCast(ns / std.time.ns_per_s),
        .nsec = @intCast(ns % std.time.ns_per_s),
    };
}

const ConnectState = enum {
    connected,
    pending,
};

fn connectNonblocking(fd: std.posix.fd_t, address: *const std.net.Address) !ConnectState {
    const rc = std.posix.system.connect(fd, &address.any, address.getOsSockLen());
    switch (std.posix.errno(rc)) {
        .SUCCESS => return .connected,
        .INPROGRESS, .ALREADY => return .pending,
        .AGAIN => return error.SystemResources,
        .ACCES => return error.AccessDenied,
        .PERM => return error.PermissionDenied,
        .ADDRINUSE => return error.AddressInUse,
        .ADDRNOTAVAIL => return error.AddressNotAvailable,
        .AFNOSUPPORT => return error.AddressFamilyNotSupported,
        .CONNREFUSED => return error.ConnectionRefused,
        .HOSTUNREACH, .NETUNREACH => return error.NetworkUnreachable,
        .NOENT => return error.FileNotFound,
        else => |err| return std.posix.unexpectedErrno(err),
    }
}

fn bridgeInterest(stage: BridgeStage) u32 {
    return switch (stage) {
        .connecting => linux.EPOLL.OUT | linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP,
        .handshaking => linux.EPOLL.IN | linux.EPOLL.OUT | linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP,
        .streaming => linux.EPOLL.IN | linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP,
    };
}

fn consumerInterest(consumer: *const Consumer) u32 {
    var events: u32 = linux.EPOLL.IN | linux.EPOLL.ERR | linux.EPOLL.HUP | linux.EPOLL.RDHUP;
    if (consumer.wants_write) events |= linux.EPOLL.OUT;
    return events;
}

fn nextSnapshotPacket(consumer: *Consumer) !?hostp.Packet {
    while (true) {
        if (consumer.snapshot_index >= consumer.snapshot_count) {
            if (consumer.snapshot_phase == .end) return null;
            consumer.snapshot_phase = .end;
            return hostp.Packet{
                .header = .{
                    .kind = .snapshot_end,
                    .host_seq = consumer.snapshot_next_event_seq,
                    .observed_wall_ns = try hs.realtimeNowNs(),
                },
                .payload = hostp.zeroPayload(),
            };
        }

        const vm = consumer.snapshot_vms[consumer.snapshot_index];
        switch (consumer.snapshot_phase) {
            .hello => {
                consumer.snapshot_phase = .sample;
                if (vm.hello) |hello| {
                    return .{
                        .header = .{
                            .kind = .hello,
                            .host_seq = vm.hello_event_seq,
                            .observed_wall_ns = try hs.realtimeNowNs(),
                            .job_id = try hs.parseUuid(vm.job_id.slice()),
                            .stream_generation = vm.stream_generation,
                            .flags = hostp.packet_flag_snapshot,
                        },
                        .payload = hs.encodeHelloFrame(hello),
                    };
                }
            },
            .sample => {
                consumer.snapshot_phase = .disconnect;
                if (vm.sample) |sample| {
                    return .{
                        .header = .{
                            .kind = .sample,
                            .host_seq = vm.sample_event_seq,
                            .observed_wall_ns = try hs.realtimeNowNs(),
                            .job_id = try hs.parseUuid(vm.job_id.slice()),
                            .stream_generation = vm.stream_generation,
                            .flags = hostp.packet_flag_snapshot,
                        },
                        .payload = hs.encodeSampleFrame(sample),
                    };
                }
            },
            .disconnect => {
                consumer.snapshot_phase = .hello;
                consumer.snapshot_index += 1;
                if (vm.disconnect_reason) |reason| {
                    return .{
                        .header = .{
                            .kind = .disconnect,
                            .host_seq = vm.disconnect_event_seq,
                            .observed_wall_ns = try hs.realtimeNowNs(),
                            .job_id = try hs.parseUuid(vm.job_id.slice()),
                            .stream_generation = vm.stream_generation,
                            .flags = hostp.packet_flag_snapshot,
                        },
                        .payload = hostp.encodeDisconnectPayload(reason),
                    };
                }
            },
            .end => unreachable,
        }
    }
}

fn packetFromEvent(record: HostCore.EventRecord) !?hostp.Packet {
    switch (record.event) {
        .hello => |payload| {
            return .{
                .header = .{
                    .kind = .hello,
                    .host_seq = record.seq,
                    .observed_wall_ns = try hs.realtimeNowNs(),
                    .job_id = try hs.parseUuid(payload.job_id.slice()),
                    .stream_generation = payload.stream_generation,
                },
                .payload = hs.encodeHelloFrame(payload.frame),
            };
        },
        .sample => |payload| {
            return .{
                .header = .{
                    .kind = .sample,
                    .host_seq = record.seq,
                    .observed_wall_ns = try hs.realtimeNowNs(),
                    .job_id = try hs.parseUuid(payload.job_id.slice()),
                    .stream_generation = payload.stream_generation,
                },
                .payload = hs.encodeSampleFrame(payload.frame),
            };
        },
        .stream_disconnected => |payload| {
            return .{
                .header = .{
                    .kind = .disconnect,
                    .host_seq = record.seq,
                    .observed_wall_ns = try hs.realtimeNowNs(),
                    .job_id = try hs.parseUuid(payload.job_id.slice()),
                    .stream_generation = payload.stream_generation,
                },
                .payload = hostp.encodeDisconnectPayload(payload.reason),
            };
        },
        .vm_gone => |payload| {
            return .{
                .header = .{
                    .kind = .vm_gone,
                    .host_seq = record.seq,
                    .observed_wall_ns = try hs.realtimeNowNs(),
                    .job_id = try hs.parseUuid(payload.job_id.slice()),
                },
                .payload = hostp.zeroPayload(),
            };
        },
        else => return null,
    }
}

const DecodedToken = struct {
    kind: FdKind,
    index: usize,
};

fn fdToken(kind: FdKind, index: usize) u64 {
    return (@as(u64, @intFromEnum(kind)) << 32) | @as(u64, @intCast(index));
}

fn decodeToken(value: u64) DecodedToken {
    return .{
        .kind = @enumFromInt(@as(u8, @truncate(value >> 32))),
        .index = @intCast(value & 0xffff_ffff),
    };
}

// Minimal bridge peer used by the regression test: accept one client, read the
// CONNECT line, then close without sending an ACK.
fn acceptAndCloseBridgeServer(socket_path: []const u8) void {
    acceptAndCloseBridgeServerMain(socket_path) catch |err| {
        std.debug.panic("acceptAndCloseBridgeServerMain failed: {s}", .{@errorName(err)});
    };
}

fn acceptAndCloseBridgeServerMain(socket_path: []const u8) !void {
    std.fs.deleteFileAbsolute(socket_path) catch |err| switch (err) {
        error.FileNotFound => {},
        else => return err,
    };

    var address = try std.net.Address.initUnix(socket_path);
    const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
    defer std.posix.close(fd);
    try std.posix.bind(fd, &address.any, address.getOsSockLen());
    try std.posix.listen(fd, 1);

    const conn_fd = try std.posix.accept(fd, null, null, std.posix.SOCK.CLOEXEC);
    defer std.posix.close(conn_fd);

    var cmd_buf: [bridge_ack_buffer_bytes]u8 = undefined;
    _ = try std.posix.read(conn_fd, cmd_buf[0..]);
}

fn connectGuestBridge(uds_path: []const u8, guest_port: u32) !std.net.Stream {
    var address = try std.net.Address.initUnix(uds_path);
    const stream = blk: {
        const fd = try std.posix.socket(std.posix.AF.UNIX, std.posix.SOCK.STREAM | std.posix.SOCK.CLOEXEC, 0);
        errdefer std.posix.close(fd);
        try std.posix.connect(fd, &address.any, address.getOsSockLen());
        break :blk std.net.Stream{ .handle = fd };
    };
    errdefer stream.close();

    var cmd_buf: [max_bridge_cmd_bytes]u8 = undefined;
    const cmd = try std.fmt.bufPrint(cmd_buf[0..], "CONNECT {d}\n", .{guest_port});
    try stream.writeAll(cmd);

    var ack_buf: [bridge_ack_buffer_bytes]u8 = undefined;
    const ack = try hs.readLineInto(stream, ack_buf[0..]);
    if (!std.mem.startsWith(u8, ack, "OK ")) return error.InvalidBridgeReply;
    return stream;
}

test "connectGuestBridge closes cleanly on handshake error path" {
    const allocator = std.testing.allocator;
    const dir_path = try std.testing.tmpDir(.{}).dir.realpathAlloc(allocator, ".");
    defer allocator.free(dir_path);

    const socket_path = try std.fs.path.join(allocator, &.{ dir_path, "bridge.sock" });
    defer allocator.free(socket_path);

    const server_thread = try std.Thread.spawn(.{}, acceptAndCloseBridgeServer, .{socket_path});
    defer server_thread.join();

    var tries: usize = 0;
    while (tries < 50) : (tries += 1) {
        std.fs.accessAbsolute(socket_path, .{}) catch {
            std.Thread.sleep(10 * std.time.ns_per_ms);
            continue;
        };
        break;
    }

    const err = connectGuestBridge(socket_path, hs.default_guest_port);
    try std.testing.expectError(error.InvalidBridgeReply, err);
}

test "snapshot packet builder emits disconnect state and terminator" {
    const job_id = "00000000-0000-0000-0000-000000000001";
    var consumer = Consumer{
        .used = true,
        .stage = .snapshot,
        .snapshot_count = 1,
        .snapshot_next_event_seq = 23,
    };
    consumer.snapshot_vms[0] = .{
        .job_id = try hostc.JobId.init(job_id),
        .uds_path = try hostc.UdsPath.init("/run/job.sock"),
        .present = true,
        .connected = false,
        .stream_generation = 7,
        .disconnect_reason = .bridge_closed,
        .hello_event_seq = 11,
        .sample_event_seq = 12,
        .disconnect_event_seq = 13,
        .hello = .{
            .seq = 0,
            .mono_ns = 1,
            .wall_ns = 2,
            .boot_id = try hs.parseUuid("5691d566-f1a6-4342-8604-205e83785b21"),
            .mem_total_kb = 516096,
        },
        .sample = .{
            .seq = 9,
            .mono_ns = 3,
            .wall_ns = 4,
            .mem_available_kb = 401232,
        },
    };

    const hello = (try nextSnapshotPacket(&consumer)).?;
    try std.testing.expectEqual(hostp.PacketKind.hello, hello.header.kind);
    try std.testing.expectEqual(@as(u64, 11), hello.header.host_seq);

    const sample = (try nextSnapshotPacket(&consumer)).?;
    try std.testing.expectEqual(hostp.PacketKind.sample, sample.header.kind);
    try std.testing.expectEqual(@as(u64, 12), sample.header.host_seq);

    const disconnect = (try nextSnapshotPacket(&consumer)).?;
    try std.testing.expectEqual(hostp.PacketKind.disconnect, disconnect.header.kind);
    try std.testing.expectEqual(hostc.DisconnectReason.bridge_closed, try hostp.decodeDisconnectPayload(&disconnect.payload));

    const end = (try nextSnapshotPacket(&consumer)).?;
    try std.testing.expectEqual(hostp.PacketKind.snapshot_end, end.header.kind);
    try std.testing.expectEqual(@as(u64, 23), end.header.host_seq);
}

test "packetFromEvent ignores non-consumer lifecycle noise" {
    const packet = try packetFromEvent(.{
        .seq = 1,
        .event = .{
            .stream_started = .{
                .job_id = try hostc.JobId.init("00000000-0000-0000-0000-000000000001"),
                .stream_generation = 1,
            },
        },
    });
    try std.testing.expect(packet == null);
}

test "runtime init and empty timer tick are stable" {
    const allocator = std.testing.allocator;
    var dir = std.testing.tmpDir(.{});
    defer dir.cleanup();

    const root_path = try dir.dir.realpathAlloc(allocator, ".");
    defer allocator.free(root_path);

    const control_uds = try std.fs.path.join(allocator, &.{ root_path, "smelter.sock" });
    defer allocator.free(control_uds);
    const jailer_root = try std.fs.path.join(allocator, &.{ root_path, "jailer" });
    defer allocator.free(jailer_root);
    try std.fs.cwd().makePath(jailer_root);

    const runtime = try allocator.create(Runtime);
    defer allocator.destroy(runtime);
    try runtime.init(control_uds, jailer_root);
    defer runtime.deinit();

    std.Thread.sleep(timer_tick_ns + (10 * std.time.ns_per_ms));
    _ = try runtime.handleTimer();
}

test "runtime serves empty snapshot over seqpacket attach" {
    const allocator = std.testing.allocator;
    var dir = std.testing.tmpDir(.{});
    defer dir.cleanup();

    const root_path = try dir.dir.realpathAlloc(allocator, ".");
    defer allocator.free(root_path);

    const control_uds = try std.fs.path.join(allocator, &.{ root_path, "smelter.sock" });
    defer allocator.free(control_uds);
    const jailer_root = try std.fs.path.join(allocator, &.{ root_path, "jailer" });
    defer allocator.free(jailer_root);
    try std.fs.cwd().makePath(jailer_root);

    const runtime = try allocator.create(Runtime);
    defer allocator.destroy(runtime);
    try runtime.init(control_uds, jailer_root);
    defer runtime.deinit();

    const client_fd = try openControlFd(control_uds);
    defer std.posix.close(client_fd);

    try runtime.acceptConsumers();

    const request = hostp.encodeRequest(.{ .kind = .attach });
    try std.testing.expectEqual(@as(usize, hostp.request_size), try std.posix.send(client_fd, request[0..], 0));

    runtime.handleConsumerEvent(0, linux.EPOLL.IN);

    var buf: [hostp.packet_size]u8 = undefined;
    const n = try std.posix.recv(client_fd, buf[0..], 0);
    try std.testing.expectEqual(@as(usize, hostp.packet_size), n);

    const packet = try hostp.decodePacket(&buf);
    try std.testing.expectEqual(hostp.PacketKind.snapshot_end, packet.header.kind);
    try std.testing.expectEqual(@as(u64, 1), packet.header.host_seq);
}

test "runtime epoll attach path is stable" {
    const allocator = std.testing.allocator;
    var dir = std.testing.tmpDir(.{});
    defer dir.cleanup();

    const root_path = try dir.dir.realpathAlloc(allocator, ".");
    defer allocator.free(root_path);

    const control_uds = try std.fs.path.join(allocator, &.{ root_path, "smelter.sock" });
    defer allocator.free(control_uds);
    const jailer_root = try std.fs.path.join(allocator, &.{ root_path, "jailer" });
    defer allocator.free(jailer_root);
    try std.fs.cwd().makePath(jailer_root);

    const runtime = try std.heap.page_allocator.create(Runtime);
    defer std.heap.page_allocator.destroy(runtime);
    try runtime.init(control_uds, jailer_root);
    defer runtime.deinit();

    const client_fd = try openControlFd(control_uds);
    defer std.posix.close(client_fd);

    var events: [8]linux.epoll_event = undefined;
    var count = std.posix.epoll_wait(runtime.epoll_fd, events[0..], 1000);
    try std.testing.expectEqual(@as(usize, 1), count);
    try std.testing.expectEqual(FdKind.listener, decodeToken(events[0].data.u64).kind);
    try runtime.acceptConsumers();

    const request = hostp.encodeRequest(.{ .kind = .attach });
    try std.testing.expectEqual(@as(usize, hostp.request_size), try std.posix.send(client_fd, request[0..], 0));

    count = std.posix.epoll_wait(runtime.epoll_fd, events[0..], 1000);
    try std.testing.expect(count >= 1);
    for (events[0..count]) |event| {
        const decoded = decodeToken(event.data.u64);
        if (decoded.kind != .consumer) continue;
        runtime.handleConsumerEvent(decoded.index, event.events);
    }

    var buf: [hostp.packet_size]u8 = undefined;
    const n = try std.posix.recv(client_fd, buf[0..], 0);
    try std.testing.expectEqual(@as(usize, hostp.packet_size), n);

    const packet = try hostp.decodePacket(&buf);
    try std.testing.expectEqual(hostp.PacketKind.snapshot_end, packet.header.kind);
}

test "runtime handles consumer disconnect after snapshot" {
    const allocator = std.testing.allocator;
    var dir = std.testing.tmpDir(.{});
    defer dir.cleanup();

    const root_path = try dir.dir.realpathAlloc(allocator, ".");
    defer allocator.free(root_path);

    const control_uds = try std.fs.path.join(allocator, &.{ root_path, "smelter.sock" });
    defer allocator.free(control_uds);
    const jailer_root = try std.fs.path.join(allocator, &.{ root_path, "jailer" });
    defer allocator.free(jailer_root);
    try std.fs.cwd().makePath(jailer_root);

    const runtime = try allocator.create(Runtime);
    defer allocator.destroy(runtime);
    try runtime.init(control_uds, jailer_root);
    defer runtime.deinit();

    const client_fd = try openControlFd(control_uds);

    var events: [8]linux.epoll_event = undefined;
    var count = std.posix.epoll_wait(runtime.epoll_fd, events[0..], 1000);
    try std.testing.expectEqual(@as(usize, 1), count);
    try runtime.acceptConsumers();

    const request = hostp.encodeRequest(.{ .kind = .attach });
    try std.testing.expectEqual(@as(usize, hostp.request_size), try std.posix.send(client_fd, request[0..], 0));

    count = std.posix.epoll_wait(runtime.epoll_fd, events[0..], 1000);
    for (events[0..count]) |event| {
        const decoded = decodeToken(event.data.u64);
        if (decoded.kind != .consumer) continue;
        runtime.handleConsumerEvent(decoded.index, event.events);
    }

    var buf: [hostp.packet_size]u8 = undefined;
    _ = try std.posix.recv(client_fd, buf[0..], 0);
    std.posix.close(client_fd);

    count = std.posix.epoll_wait(runtime.epoll_fd, events[0..], 1000);
    for (events[0..count]) |event| {
        const decoded = decodeToken(event.data.u64);
        if (decoded.kind != .consumer) continue;
        runtime.handleConsumerEvent(decoded.index, event.events);
    }
}
