const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const guest_target = b.resolveTargetQuery(.{
        .cpu_arch = .x86_64,
        .os_tag = .linux,
        .abi = .musl,
    });

    const mod = b.addModule("homestead_smelter", .{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
    });

    const host = b.addExecutable(.{
        .name = "homestead-smelter-host",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/host.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    b.installArtifact(host);

    const guest = b.addExecutable(.{
        .name = "homestead-smelter-guest",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/guest.zig"),
            .target = guest_target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    b.installArtifact(guest);

    const host_step = b.step("host", "Build the host binary");
    host_step.dependOn(&host.step);

    const guest_step = b.step("guest", "Build the guest binary");
    guest_step.dependOn(&guest.step);

    const gen_vectors = b.addExecutable(.{
        .name = "generate-vectors",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/generate_vectors.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    b.installArtifact(gen_vectors);

    const run_host_step = b.step("run-host", "Run the host binary");
    const run_host = b.addRunArtifact(host);
    run_host_step.dependOn(&run_host.step);
    run_host.step.dependOn(b.getInstallStep());
    if (b.args) |args| {
        run_host.addArgs(args);
    }

    const run_gen_vectors_step = b.step("run-generate-vectors", "Regenerate protocol/vectors.json from canonical encoder");
    const run_gen_vectors = b.addRunArtifact(gen_vectors);
    run_gen_vectors_step.dependOn(&run_gen_vectors.step);

    const mod_tests = b.addTest(.{
        .root_module = mod,
    });
    const run_mod_tests = b.addRunArtifact(mod_tests);

    const host_tests = b.addTest(.{
        .root_module = host.root_module,
    });
    const run_host_tests = b.addRunArtifact(host_tests);

    const host_proto_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/host_proto.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    const run_host_proto_tests = b.addRunArtifact(host_proto_tests);

    const host_core_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/host_core.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    const run_host_core_tests = b.addRunArtifact(host_core_tests);

    const host_ops_proto_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/host_ops_proto.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const run_host_ops_proto_tests = b.addRunArtifact(host_ops_proto_tests);

    const host_ops_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/host_ops.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });
    const run_host_ops_tests = b.addRunArtifact(host_ops_tests);

    const guest_tests = b.addTest(.{
        .root_module = guest.root_module,
    });
    const run_guest_tests = b.addRunArtifact(guest_tests);

    const vectors_tests = b.addTest(.{
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/generate_vectors.zig"),
            .target = target,
            .optimize = optimize,
            .imports = &.{
                .{ .name = "homestead_smelter", .module = mod },
            },
        }),
    });
    const run_vectors_tests = b.addRunArtifact(vectors_tests);

    const test_step = b.step("test", "Run homestead-smelter tests");
    test_step.dependOn(&run_mod_tests.step);
    test_step.dependOn(&run_host_tests.step);
    test_step.dependOn(&run_host_proto_tests.step);
    test_step.dependOn(&run_host_core_tests.step);
    test_step.dependOn(&run_host_ops_proto_tests.step);
    test_step.dependOn(&run_host_ops_tests.step);
    test_step.dependOn(&run_guest_tests.step);
    test_step.dependOn(&run_vectors_tests.step);
}
