const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const bench_optimize = b.option(std.builtin.OptimizeMode, "bench-optimize", "Optimization mode for benchmark executable") orelse .ReleaseSafe;
    const guest_target = b.resolveTargetQuery(.{
        .cpu_arch = .x86_64,
        .os_tag = .linux,
        .abi = .musl,
    });

    const mod = b.addModule("vm_guest_telemetry", .{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
    });

    const guest = b.addExecutable(.{
        .name = "vm-guest-telemetry",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/guest.zig"),
            .target = guest_target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "vm_guest_telemetry", .module = mod },
            },
        }),
    });
    b.installArtifact(guest);

    const guest_step = b.step("guest", "Build the guest binary");
    guest_step.dependOn(&guest.step);

    const gen_vectors = b.addExecutable(.{
        .name = "generate-vectors",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/generate_vectors.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "vm_guest_telemetry", .module = mod },
            },
        }),
    });
    b.installArtifact(gen_vectors);

    const run_gen_vectors_step = b.step("run-generate-vectors", "Regenerate protocol/vectors.json from canonical encoder");
    const run_gen_vectors = b.addRunArtifact(gen_vectors);
    run_gen_vectors_step.dependOn(&run_gen_vectors.step);

    const bench_guest_mod = b.createModule(.{
        .root_source_file = b.path("src/guest.zig"),
        .target = target,
        .optimize = bench_optimize,
        .imports = &.{
            .{ .name = "vm_guest_telemetry", .module = mod },
        },
    });
    const bench = b.addExecutable(.{
        .name = "vm-guest-telemetry-bench",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/bench.zig"),
            .target = target,
            .optimize = bench_optimize,
            .imports = &.{
                .{ .name = "vm_guest_telemetry", .module = mod },
                .{ .name = "guest_agent", .module = bench_guest_mod },
            },
        }),
    });
    const run_bench = b.addRunArtifact(bench);
    if (b.args) |args| {
        run_bench.addArgs(args);
    }
    const bench_step = b.step("bench", "Run vm-guest-telemetry benchmarks");
    bench_step.dependOn(&run_bench.step);

    const mod_tests = b.addTest(.{
        .root_module = mod,
    });
    const run_mod_tests = b.addRunArtifact(mod_tests);

    const guest_tests = b.addTest(.{
        .root_module = guest.root_module,
    });
    const run_guest_tests = b.addRunArtifact(guest_tests);

    const vectors_tests_mod = b.createModule(.{
        .root_source_file = b.path("src/generate_vectors.zig"),
        .target = target,
        .optimize = optimize,
        .imports = &.{
            .{ .name = "vm_guest_telemetry", .module = mod },
        },
    });
    // Expose protocol/vectors.json as a named import so the staleness test can
    // @embedFile it. Without this, src/ has no visibility into ../protocol/.
    vectors_tests_mod.addAnonymousImport("vectors_json", .{
        .root_source_file = b.path("protocol/vectors.json"),
    });
    const vectors_tests = b.addTest(.{
        .root_module = vectors_tests_mod,
    });
    const run_vectors_tests = b.addRunArtifact(vectors_tests);

    const test_step = b.step("test", "Run vm-guest-telemetry tests");
    test_step.dependOn(&run_mod_tests.step);
    test_step.dependOn(&run_guest_tests.step);
    test_step.dependOn(&run_vectors_tests.step);
}
