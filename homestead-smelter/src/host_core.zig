const std = @import("std");
const hs = @import("root.zig");

pub const max_job_id_bytes: usize = 63;
pub const max_uds_path_bytes: usize = 191;
pub const max_error_bytes: usize = 96;

pub const DisconnectReason = enum(u8) {
    bridge_closed,
    connect_failed,
    decode_failed,
    vm_gone,
};

fn InlineBytes(comptime max_len: usize) type {
    return struct {
        len: u16 = 0,
        bytes: [max_len]u8 = [_]u8{0} ** max_len,

        const Self = @This();

        fn init(value: []const u8) !Self {
            var out = Self{};
            try out.set(value);
            return out;
        }

        fn set(self: *Self, value: []const u8) !void {
            if (value.len > max_len) return error.ValueTooLong;
            self.clear();
            self.len = @intCast(value.len);
            @memcpy(self.bytes[0..value.len], value);
        }

        fn clear(self: *Self) void {
            self.len = 0;
            @memset(self.bytes[0..], 0);
        }

        fn slice(self: *const Self) []const u8 {
            return self.bytes[0..self.len];
        }

        fn eql(self: *const Self, value: []const u8) bool {
            return std.mem.eql(u8, self.slice(), value);
        }

        fn empty(self: *const Self) bool {
            return self.len == 0;
        }
    };
}

pub const JobId = InlineBytes(max_job_id_bytes);
pub const UdsPath = InlineBytes(max_uds_path_bytes);
pub const ErrorText = InlineBytes(max_error_bytes);

fn disconnectReasonText(reason: DisconnectReason) []const u8 {
    return switch (reason) {
        .bridge_closed => "bridge_closed",
        .connect_failed => "connect_failed",
        .decode_failed => "decode_failed",
        .vm_gone => "vm_gone",
    };
}

pub fn HostCoreType(comptime max_vms: usize, comptime ring_capacity: usize) type {
    comptime {
        std.debug.assert(max_vms > 0);
        std.debug.assert(ring_capacity > 0);
    }

    return struct {
        pub const max_vm_count = max_vms;
        pub const event_capacity = ring_capacity;

        pub const StreamToken = struct {
            slot_index: u16,
            stream_generation: u32,
        };

        pub const CursorStart = enum {
            oldest,
            next,
        };

        pub const Event = union(enum) {
            vm_discovered: struct {
                job_id: JobId,
                uds_path: UdsPath,
            },
            stream_started: struct {
                job_id: JobId,
                stream_generation: u32,
            },
            stream_connected: struct {
                job_id: JobId,
                stream_generation: u32,
            },
            hello: struct {
                job_id: JobId,
                stream_generation: u32,
                frame: hs.HelloFrame,
            },
            sample: struct {
                job_id: JobId,
                stream_generation: u32,
                frame: hs.SampleFrame,
            },
            stream_disconnected: struct {
                job_id: JobId,
                stream_generation: u32,
                reason: DisconnectReason,
            },
            vm_gone: struct {
                job_id: JobId,
            },
        };

        pub const EventRecord = struct {
            seq: u64,
            event: Event,
        };

        pub const Cursor = struct {
            next_seq: u64 = 1,
        };

        pub const DrainStatus = enum {
            ok,
            overflow,
        };

        pub const DrainResult = struct {
            status: DrainStatus,
            count: usize,
            next_seq: u64,
        };

        pub const SnapshotVm = struct {
            job_id: JobId = .{},
            uds_path: UdsPath = .{},
            present: bool = false,
            connected: bool = false,
            stream_generation: u32 = 0,
            last_error: ErrorText = .{},
            hello: ?hs.HelloFrame = null,
            sample: ?hs.SampleFrame = null,

            pub fn lastError(self: *const SnapshotVm) ?[]const u8 {
                if (self.last_error.empty()) return null;
                return self.last_error.slice();
            }
        };

        pub const Snapshot = struct {
            next_event_seq: u64,
            vms: []const SnapshotVm,
        };

        const Self = @This();

        const VmSlot = struct {
            used: bool = false,
            present: bool = false,
            connected: bool = false,
            stream_generation: u32 = 0,
            job_id: JobId = .{},
            uds_path: UdsPath = .{},
            last_error: ErrorText = .{},
            hello: ?hs.HelloFrame = null,
            sample: ?hs.SampleFrame = null,

            fn snapshot(self: *const VmSlot) SnapshotVm {
                return .{
                    .job_id = self.job_id,
                    .uds_path = self.uds_path,
                    .present = self.present,
                    .connected = self.connected,
                    .stream_generation = self.stream_generation,
                    .last_error = self.last_error,
                    .hello = self.hello,
                    .sample = self.sample,
                };
            }
        };

        const Ring = struct {
            items: [ring_capacity]EventRecord = undefined,
            start_seq: u64 = 1,
            next_seq: u64 = 1,
            len: usize = 0,

            fn oldestSeq(self: *const Ring) u64 {
                return self.start_seq;
            }

            fn append(self: *Ring, event: Event) void {
                const seq = self.next_seq;
                const slot = @as(usize, @intCast((seq - 1) % ring_capacity));
                self.items[slot] = .{
                    .seq = seq,
                    .event = event,
                };

                self.next_seq += 1;
                if (self.len < ring_capacity) {
                    self.len += 1;
                } else {
                    self.start_seq += 1;
                }
            }

            fn cursor(self: *const Ring, start: CursorStart) Cursor {
                return .{
                    .next_seq = switch (start) {
                        .oldest => self.oldestSeq(),
                        .next => self.next_seq,
                    },
                };
            }

            fn cursorFromSeq(_: *const Ring, next_seq: u64) Cursor {
                return .{ .next_seq = next_seq };
            }

            fn drain(self: *const Ring, drain_cursor: *Cursor, out: []EventRecord) DrainResult {
                if (drain_cursor.next_seq < self.oldestSeq()) {
                    drain_cursor.next_seq = self.next_seq;
                    return .{
                        .status = .overflow,
                        .count = 0,
                        .next_seq = drain_cursor.next_seq,
                    };
                }

                var count: usize = 0;
                while (count < out.len and drain_cursor.next_seq < self.next_seq) : (count += 1) {
                    const slot = @as(usize, @intCast((drain_cursor.next_seq - 1) % ring_capacity));
                    out[count] = self.items[slot];
                    drain_cursor.next_seq += 1;
                }

                return .{
                    .status = .ok,
                    .count = count,
                    .next_seq = drain_cursor.next_seq,
                };
            }
        };

        slots: [max_vms]VmSlot = [_]VmSlot{.{}} ** max_vms,
        ring: Ring = .{},

        pub fn discover(self: *Self, job_id: []const u8, uds_path: []const u8) !void {
            if (self.findSlotIndex(job_id)) |slot_index| {
                const slot = &self.slots[slot_index];
                const path_changed = !slot.uds_path.eql(uds_path);
                const was_present = slot.present;

                if (!path_changed and was_present) return;

                if (path_changed) {
                    try slot.uds_path.set(uds_path);
                }

                if (path_changed or !was_present) {
                    if (slot.used) try self.bumpGeneration(slot);
                    slot.present = true;
                    slot.connected = false;
                    slot.hello = null;
                    slot.sample = null;
                    slot.last_error.clear();
                    self.ring.append(.{
                        .vm_discovered = .{
                            .job_id = slot.job_id,
                            .uds_path = slot.uds_path,
                        },
                    });
                }
                return;
            }

            const slot = &self.slots[try self.allocSlot()];
            slot.used = true;
            slot.present = true;
            try slot.job_id.set(job_id);
            try slot.uds_path.set(uds_path);
            slot.connected = false;
            slot.stream_generation = 0;
            slot.last_error.clear();
            slot.hello = null;
            slot.sample = null;

            self.ring.append(.{
                .vm_discovered = .{
                    .job_id = slot.job_id,
                    .uds_path = slot.uds_path,
                },
            });
        }

        pub fn beginStream(self: *Self, job_id: []const u8) !StreamToken {
            const slot = self.findPresentSlot(job_id) orelse return error.VmNotPresent;
            try self.bumpGeneration(slot);
            slot.connected = false;
            slot.last_error.clear();

            self.ring.append(.{
                .stream_started = .{
                    .job_id = slot.job_id,
                    .stream_generation = slot.stream_generation,
                },
            });

            return .{
                .slot_index = @intCast(self.slotIndex(slot)),
                .stream_generation = slot.stream_generation,
            };
        }

        pub fn recordConnected(self: *Self, token: StreamToken) bool {
            const slot = self.currentSlot(token) orelse return false;
            slot.connected = true;
            slot.last_error.clear();
            self.ring.append(.{
                .stream_connected = .{
                    .job_id = slot.job_id,
                    .stream_generation = token.stream_generation,
                },
            });
            return true;
        }

        pub fn recordHello(self: *Self, token: StreamToken, frame: hs.HelloFrame) bool {
            const slot = self.currentSlot(token) orelse return false;
            slot.connected = true;
            slot.hello = frame;
            slot.last_error.clear();
            self.ring.append(.{
                .hello = .{
                    .job_id = slot.job_id,
                    .stream_generation = token.stream_generation,
                    .frame = frame,
                },
            });
            return true;
        }

        pub fn recordSample(self: *Self, token: StreamToken, frame: hs.SampleFrame) bool {
            const slot = self.currentSlot(token) orelse return false;
            slot.connected = true;
            slot.sample = frame;
            slot.last_error.clear();
            self.ring.append(.{
                .sample = .{
                    .job_id = slot.job_id,
                    .stream_generation = token.stream_generation,
                    .frame = frame,
                },
            });
            return true;
        }

        pub fn recordDisconnect(self: *Self, token: StreamToken, reason: DisconnectReason) bool {
            const slot = self.currentSlot(token) orelse return false;
            slot.connected = false;
            slot.last_error.set(disconnectReasonText(reason)) catch unreachable;
            self.ring.append(.{
                .stream_disconnected = .{
                    .job_id = slot.job_id,
                    .stream_generation = token.stream_generation,
                    .reason = reason,
                },
            });
            return true;
        }

        pub fn markGone(self: *Self, job_id: []const u8) !void {
            const slot = self.findSlot(job_id) orelse return;
            if (!slot.present) return;

            try self.bumpGeneration(slot);
            slot.present = false;
            slot.connected = false;
            slot.last_error.set(disconnectReasonText(.vm_gone)) catch unreachable;
            self.ring.append(.{
                .vm_gone = .{
                    .job_id = slot.job_id,
                },
            });
        }

        pub fn snapshot(self: *const Self, out: []SnapshotVm) Snapshot {
            var count: usize = 0;
            for (self.slots) |slot| {
                if (!slot.used) continue;
                if (!slot.present) continue;
                std.debug.assert(count < out.len);
                out[count] = slot.snapshot();
                count += 1;
            }
            return .{
                .next_event_seq = self.ring.next_seq,
                .vms = out[0..count],
            };
        }

        pub fn cursor(self: *const Self, start: CursorStart) Cursor {
            return self.ring.cursor(start);
        }

        pub fn cursorFromSeq(self: *const Self, next_seq: u64) Cursor {
            return self.ring.cursorFromSeq(next_seq);
        }

        pub fn drain(self: *const Self, drain_cursor: *Cursor, out: []EventRecord) DrainResult {
            return self.ring.drain(drain_cursor, out);
        }

        fn allocSlot(self: *Self) !usize {
            for (&self.slots, 0..) |*slot, index| {
                if (slot.used) continue;
                return index;
            }
            return error.FleetFull;
        }

        fn findSlotIndex(self: *const Self, job_id: []const u8) ?usize {
            for (self.slots, 0..) |slot, index| {
                if (!slot.used) continue;
                if (slot.job_id.eql(job_id)) return index;
            }
            return null;
        }

        fn findSlot(self: *Self, job_id: []const u8) ?*VmSlot {
            const index = self.findSlotIndex(job_id) orelse return null;
            return &self.slots[index];
        }

        fn findPresentSlot(self: *Self, job_id: []const u8) ?*VmSlot {
            const slot = self.findSlot(job_id) orelse return null;
            if (!slot.present) return null;
            return slot;
        }

        fn slotIndex(self: *const Self, target: *const VmSlot) usize {
            const base = @intFromPtr(&self.slots[0]);
            const ptr = @intFromPtr(target);
            return (ptr - base) / @sizeOf(VmSlot);
        }

        fn currentSlot(self: *Self, token: StreamToken) ?*VmSlot {
            if (token.slot_index >= max_vms) return null;
            const slot = &self.slots[token.slot_index];
            if (!slot.used) return null;
            if (!slot.present) return null;
            if (slot.stream_generation != token.stream_generation) return null;
            return slot;
        }

        fn bumpGeneration(self: *Self, slot: *VmSlot) !void {
            _ = self;
            if (slot.stream_generation == std.math.maxInt(u32)) {
                return error.StreamGenerationOverflow;
            }
            slot.stream_generation += 1;
        }
    };
}

fn makeHelloFrame(seq: u32) hs.HelloFrame {
    return .{
        .seq = seq,
        .flags = 0,
        .mono_ns = 1000 + seq,
        .wall_ns = 2000 + seq,
        .boot_id = [_]u8{
            0, 1, 2, 3,
            4, 5, 6, 7,
            8, 9, 10, 11,
            12, 13, 14, @intCast(seq % 256),
        },
        .mem_total_kb = 1024 + seq,
    };
}

fn makeSampleFrame(seq: u32) hs.SampleFrame {
    return .{
        .seq = seq,
        .flags = 0,
        .mono_ns = 3000 + seq,
        .wall_ns = 4000 + seq,
        .cpu_user_ticks = seq * 10,
        .cpu_system_ticks = seq * 20,
        .cpu_idle_ticks = seq * 30,
        .load1_centis = seq,
        .load5_centis = seq + 1,
        .load15_centis = seq + 2,
        .procs_running = @intCast(seq % 5),
        .procs_blocked = @intCast(seq % 3),
        .mem_available_kb = 512 + seq,
        .io_read_bytes = seq * 100,
        .io_write_bytes = seq * 200,
        .net_rx_bytes = seq * 300,
        .net_tx_bytes = seq * 400,
        .psi_cpu_pct100 = @intCast(seq % 1000),
        .psi_mem_pct100 = @intCast((seq + 1) % 1000),
        .psi_io_pct100 = @intCast((seq + 2) % 1000),
    };
}

const TestJob = enum(u8) {
    a,
    b,
    c,
};

const test_jobs = [_]TestJob{ .a, .b, .c };

fn testJobId(job: TestJob) []const u8 {
    return switch (job) {
        .a => "job-a",
        .b => "job-b",
        .c => "job-c",
    };
}

fn testPath(job: TestJob, variant: u1) []const u8 {
    return switch (job) {
        .a => if (variant == 0) "/run/job-a.sock" else "/run/job-a-alt.sock",
        .b => if (variant == 0) "/run/job-b.sock" else "/run/job-b-alt.sock",
        .c => if (variant == 0) "/run/job-c.sock" else "/run/job-c-alt.sock",
    };
}

const TestCore = HostCoreType(4, 64);

const ModelVm = struct {
    used: bool = false,
    present: bool = false,
    connected: bool = false,
    stream_generation: u32 = 0,
    path_variant: u1 = 0,
    last_error: ?DisconnectReason = null,
    hello: ?hs.HelloFrame = null,
    sample: ?hs.SampleFrame = null,
};

const Model = struct {
    vms: [test_jobs.len]ModelVm = [_]ModelVm{.{}} ** test_jobs.len,

    fn index(job: TestJob) usize {
        return @intFromEnum(job);
    }

    fn discover(self: *Model, job: TestJob, path_variant: u1) !void {
        const vm = &self.vms[index(job)];
        const was_present = vm.present;
        const path_changed = vm.used and vm.path_variant != path_variant;

        if (vm.used and was_present and !path_changed) return;

        if (vm.used and (path_changed or !was_present)) {
            if (vm.stream_generation == std.math.maxInt(u32)) return error.StreamGenerationOverflow;
            vm.stream_generation += 1;
        }

        vm.used = true;
        vm.present = true;
        vm.connected = false;
        vm.path_variant = path_variant;
        vm.last_error = null;
        vm.hello = null;
        vm.sample = null;
    }

    fn beginStream(self: *Model, job: TestJob) !?u32 {
        const vm = &self.vms[index(job)];
        if (!vm.present) return null;
        if (vm.stream_generation == std.math.maxInt(u32)) return error.StreamGenerationOverflow;
        vm.stream_generation += 1;
        vm.connected = false;
        vm.last_error = null;
        return vm.stream_generation;
    }

    fn recordConnected(self: *Model, job: TestJob, generation: u32) bool {
        const vm = &self.vms[index(job)];
        if (!vm.present) return false;
        if (vm.stream_generation != generation) return false;
        vm.connected = true;
        vm.last_error = null;
        return true;
    }

    fn recordHello(self: *Model, job: TestJob, generation: u32, frame: hs.HelloFrame) bool {
        const vm = &self.vms[index(job)];
        if (!vm.present) return false;
        if (vm.stream_generation != generation) return false;
        vm.connected = true;
        vm.last_error = null;
        vm.hello = frame;
        return true;
    }

    fn recordSample(self: *Model, job: TestJob, generation: u32, frame: hs.SampleFrame) bool {
        const vm = &self.vms[index(job)];
        if (!vm.present) return false;
        if (vm.stream_generation != generation) return false;
        vm.connected = true;
        vm.last_error = null;
        vm.sample = frame;
        return true;
    }

    fn recordDisconnect(self: *Model, job: TestJob, generation: u32, reason: DisconnectReason) bool {
        const vm = &self.vms[index(job)];
        if (!vm.present) return false;
        if (vm.stream_generation != generation) return false;
        vm.connected = false;
        vm.last_error = reason;
        return true;
    }

    fn markGone(self: *Model, job: TestJob) !void {
        const vm = &self.vms[index(job)];
        if (!vm.present) return;
        if (vm.stream_generation == std.math.maxInt(u32)) return error.StreamGenerationOverflow;
        vm.stream_generation += 1;
        vm.present = false;
        vm.connected = false;
        vm.last_error = .vm_gone;
    }

    fn initFromSnapshot(snapshot: TestCore.Snapshot) Model {
        var out = Model{};
        for (snapshot.vms) |vm| {
            const job = parseTestJob(vm.job_id.slice()).?;
            const state = &out.vms[index(job)];
            state.used = true;
            state.present = vm.present;
            state.connected = vm.connected;
            state.stream_generation = vm.stream_generation;
            state.path_variant = parsePathVariant(job, vm.uds_path.slice());
            state.last_error = if (vm.lastError()) |value| parseDisconnectReason(value) else null;
            state.hello = vm.hello;
            state.sample = vm.sample;
        }
        return out;
    }

    fn applyEvent(self: *Model, event: TestCore.EventRecord) !void {
        switch (event.event) {
            .vm_discovered => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try self.discover(job, parsePathVariant(job, payload.uds_path.slice()));
            },
            .stream_started => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                const generation = (try self.beginStream(job)).?;
                try std.testing.expectEqual(payload.stream_generation, generation);
            },
            .stream_connected => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try std.testing.expect(self.recordConnected(job, payload.stream_generation));
            },
            .hello => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try std.testing.expect(self.recordHello(job, payload.stream_generation, payload.frame));
            },
            .sample => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try std.testing.expect(self.recordSample(job, payload.stream_generation, payload.frame));
            },
            .stream_disconnected => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try std.testing.expect(self.recordDisconnect(job, payload.stream_generation, payload.reason));
            },
            .vm_gone => |payload| {
                const job = parseTestJob(payload.job_id.slice()).?;
                try self.markGone(job);
            },
        }
    }

    fn expectMatchesCore(self: *const Model, core: *const TestCore) !void {
        var snapshot_buf: [test_jobs.len]TestCore.SnapshotVm = undefined;
        const snapshot = core.snapshot(snapshot_buf[0..]);

        var present_count: usize = 0;
        for (test_jobs) |job| {
            const expected = self.vms[index(job)];
            const actual = findSnapshotVm(snapshot, testJobId(job));
            if (expected.present) {
                present_count += 1;
                try std.testing.expect(actual != null);
                const vm = actual.?;
                try std.testing.expect(vm.present);
                try std.testing.expectEqual(expected.connected, vm.connected);
                try std.testing.expectEqual(expected.stream_generation, vm.stream_generation);
                try std.testing.expectEqualStrings(testPath(job, expected.path_variant), vm.uds_path.slice());
                try std.testing.expectEqualDeep(expected.hello, vm.hello);
                try std.testing.expectEqualDeep(expected.sample, vm.sample);
                if (expected.last_error) |reason| {
                    try std.testing.expectEqualStrings(disconnectReasonText(reason), vm.lastError().?);
                } else {
                    try std.testing.expect(vm.lastError() == null);
                }
            } else {
                try std.testing.expect(actual == null);
            }
        }

        try std.testing.expectEqual(present_count, snapshot.vms.len);
    }
};

fn parseTestJob(value: []const u8) ?TestJob {
    if (std.mem.eql(u8, value, "job-a")) return .a;
    if (std.mem.eql(u8, value, "job-b")) return .b;
    if (std.mem.eql(u8, value, "job-c")) return .c;
    return null;
}

fn parsePathVariant(job: TestJob, path: []const u8) u1 {
    if (std.mem.eql(u8, path, testPath(job, 0))) return 0;
    std.debug.assert(std.mem.eql(u8, path, testPath(job, 1)));
    return 1;
}

fn parseDisconnectReason(value: []const u8) DisconnectReason {
    if (std.mem.eql(u8, value, "bridge_closed")) return .bridge_closed;
    if (std.mem.eql(u8, value, "connect_failed")) return .connect_failed;
    if (std.mem.eql(u8, value, "decode_failed")) return .decode_failed;
    std.debug.assert(std.mem.eql(u8, value, "vm_gone"));
    return .vm_gone;
}

fn findSnapshotVm(snapshot: TestCore.Snapshot, job_id: []const u8) ?*const TestCore.SnapshotVm {
    for (snapshot.vms) |*vm| {
        if (vm.job_id.eql(job_id)) return vm;
    }
    return null;
}

const SimulationAction = enum(u8) {
    discover,
    rediscover_alt_path,
    begin_stream,
    connect_current,
    hello_current,
    sample_current,
    disconnect_current,
    hello_stale,
    sample_stale,
    mark_gone,
};

fn chooseAction(random: std.Random, enabled: []const bool) SimulationAction {
    var candidates: [@typeInfo(SimulationAction).@"enum".fields.len]SimulationAction = undefined;
    var count: usize = 0;
    for (0..enabled.len) |index| {
        if (!enabled[index]) continue;
        candidates[count] = @enumFromInt(index);
        count += 1;
    }
    std.debug.assert(count > 0);
    return candidates[random.uintLessThan(usize, count)];
}

fn randomEnabledActions(random: std.Random) [@typeInfo(SimulationAction).@"enum".fields.len]bool {
    var enabled = [_]bool{false} ** @typeInfo(SimulationAction).@"enum".fields.len;
    var any = false;
    for (0..enabled.len) |index| {
        enabled[index] = random.boolean();
        any = any or enabled[index];
    }
    if (!any) enabled[0] = true;
    return enabled;
}

fn runSwarmSimulation(seed: u64) !void {
    var core = TestCore{};
    var model = Model{};
    var current_tokens = [_]?TestCore.StreamToken{null} ** test_jobs.len;
    var stale_tokens = [_]?TestCore.StreamToken{null} ** test_jobs.len;

    var prng = std.Random.DefaultPrng.init(seed);
    const random = prng.random();
    const enabled = randomEnabledActions(random);

    var step: usize = 0;
    while (step < 300) : (step += 1) {
        const job = test_jobs[random.uintLessThan(usize, test_jobs.len)];
        const action = chooseAction(random, enabled[0..]);
        const job_index = @intFromEnum(job);

        switch (action) {
            .discover => {
                try core.discover(testJobId(job), testPath(job, 0));
                try model.discover(job, 0);
            },
            .rediscover_alt_path => {
                try core.discover(testJobId(job), testPath(job, 1));
                try model.discover(job, 1);
            },
            .begin_stream => {
                const core_token = core.beginStream(testJobId(job)) catch null;
                const model_generation = try model.beginStream(job);
                if (core_token) |token| {
                    try std.testing.expect(model_generation != null);
                    try std.testing.expectEqual(model_generation.?, token.stream_generation);
                    stale_tokens[job_index] = current_tokens[job_index];
                    current_tokens[job_index] = token;
                } else {
                    try std.testing.expect(model_generation == null);
                }
            },
            .connect_current => {
                if (current_tokens[job_index]) |token| {
                    try std.testing.expectEqual(model.recordConnected(job, token.stream_generation), core.recordConnected(token));
                }
            },
            .hello_current => {
                if (current_tokens[job_index]) |token| {
                    const frame = makeHelloFrame(@intCast(step + 1));
                    try std.testing.expectEqual(model.recordHello(job, token.stream_generation, frame), core.recordHello(token, frame));
                }
            },
            .sample_current => {
                if (current_tokens[job_index]) |token| {
                    const frame = makeSampleFrame(@intCast(step + 1));
                    try std.testing.expectEqual(model.recordSample(job, token.stream_generation, frame), core.recordSample(token, frame));
                }
            },
            .disconnect_current => {
                if (current_tokens[job_index]) |token| {
                    const reason: DisconnectReason = switch (step % 3) {
                        0 => .bridge_closed,
                        1 => .connect_failed,
                        else => .decode_failed,
                    };
                    try std.testing.expectEqual(model.recordDisconnect(job, token.stream_generation, reason), core.recordDisconnect(token, reason));
                }
            },
            .hello_stale => {
                if (stale_tokens[job_index]) |token| {
                    const frame = makeHelloFrame(@intCast(step + 1));
                    try std.testing.expectEqual(model.recordHello(job, token.stream_generation, frame), core.recordHello(token, frame));
                }
            },
            .sample_stale => {
                if (stale_tokens[job_index]) |token| {
                    const frame = makeSampleFrame(@intCast(step + 1));
                    try std.testing.expectEqual(model.recordSample(job, token.stream_generation, frame), core.recordSample(token, frame));
                }
            },
            .mark_gone => {
                try core.markGone(testJobId(job));
                try model.markGone(job);
            },
        }

        try model.expectMatchesCore(&core);
    }
}

test "stale stream events are ignored" {
    var core = TestCore{};
    try core.discover("job-a", "/run/job-a.sock");

    const first = try core.beginStream("job-a");
    try std.testing.expect(core.recordConnected(first));
    try std.testing.expect(core.recordHello(first, makeHelloFrame(1)));

    const second = try core.beginStream("job-a");
    try std.testing.expect(core.recordConnected(second));
    try std.testing.expect(!core.recordSample(first, makeSampleFrame(2)));
    try std.testing.expect(core.recordSample(second, makeSampleFrame(3)));

    var snapshot_buf: [test_jobs.len]TestCore.SnapshotVm = undefined;
    const snapshot = core.snapshot(snapshot_buf[0..]);
    try std.testing.expectEqual(@as(usize, 1), snapshot.vms.len);
    const vm = snapshot.vms[0];
    try std.testing.expectEqual(second.stream_generation, vm.stream_generation);
    try std.testing.expect(vm.sample != null);
    try std.testing.expectEqual(@as(u32, 3), vm.sample.?.seq);
}

test "event ring overflow requires a resnapshot" {
    const SmallCore = HostCoreType(2, 4);

    var core = SmallCore{};
    try core.discover("job-a", "/run/job-a.sock");
    var cursor = core.cursor(.oldest);

    const token = try core.beginStream("job-a");
    try std.testing.expect(core.recordConnected(token));
    try std.testing.expect(core.recordHello(token, makeHelloFrame(1)));
    try std.testing.expect(core.recordSample(token, makeSampleFrame(2)));
    try std.testing.expect(core.recordSample(token, makeSampleFrame(3)));

    var out: [4]SmallCore.EventRecord = undefined;
    const drained = core.drain(&cursor, out[0..]);
    try std.testing.expectEqual(.overflow, drained.status);
    try std.testing.expectEqual(@as(usize, 0), drained.count);
    try std.testing.expectEqual(core.cursor(.next).next_seq, drained.next_seq);
}

test "snapshot plus drained events reconstructs live state" {
    var core = TestCore{};
    try core.discover("job-a", "/run/job-a.sock");
    try core.discover("job-b", "/run/job-b.sock");

    const token_a = try core.beginStream("job-a");
    _ = core.recordConnected(token_a);
    _ = core.recordHello(token_a, makeHelloFrame(1));
    _ = core.recordSample(token_a, makeSampleFrame(2));

    var base_snapshot_buf: [test_jobs.len]TestCore.SnapshotVm = undefined;
    const base_snapshot = core.snapshot(base_snapshot_buf[0..]);
    var mirror = Model.initFromSnapshot(base_snapshot);
    var cursor = core.cursorFromSeq(base_snapshot.next_event_seq);

    const token_b = try core.beginStream("job-b");
    _ = core.recordConnected(token_b);
    _ = core.recordHello(token_b, makeHelloFrame(10));
    _ = core.recordSample(token_a, makeSampleFrame(11));
    _ = core.recordDisconnect(token_a, .bridge_closed);
    _ = core.recordSample(token_b, makeSampleFrame(12));

    var drained_buf: [16]TestCore.EventRecord = undefined;
    while (true) {
        const drained = core.drain(&cursor, drained_buf[0..]);
        try std.testing.expectEqual(.ok, drained.status);
        if (drained.count == 0) break;
        for (drained_buf[0..drained.count]) |record| {
            try mirror.applyEvent(record);
        }
    }

    try mirror.expectMatchesCore(&core);
}

test "seeded swarm simulation preserves fleet state invariants" {
    const seeds = [_]u64{
        0x4d3d_0001,
        0x4d3d_0002,
        0x4d3d_0003,
        0x4d3d_0004,
        0x4d3d_0005,
    };

    inline for (seeds) |seed| {
        try runSwarmSimulation(seed);
    }
}
