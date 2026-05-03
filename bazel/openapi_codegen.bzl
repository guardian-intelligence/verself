load("@bazel_lib//lib:copy_to_bin.bzl", "copy_to_bin")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

OAPI_CODEGEN = "@com_github_oapi_codegen_oapi_codegen_v2//cmd/oapi-codegen"

def _spec_rule(name, out, tool, fmt):
    native.genrule(
        name = name,
        outs = [out],
        cmd = "$(location %s) --format %s > $@" % (tool, fmt),
        tools = [tool],
    )

def verself_openapi_specs(public_tool = None, internal_tool = None):
    native.package(default_visibility = ["//visibility:public"])
    if public_tool:
        _spec_rule(
            name = "spec_3_0",
            out = "openapi-3.0.yaml",
            tool = public_tool,
            fmt = "3.0",
        )
        _spec_rule(
            name = "spec_3_1",
            out = "openapi-3.1.yaml",
            tool = public_tool,
            fmt = "3.1",
        )
        copy_to_bin(
            name = "openapi-3.1.yaml.bin",
            srcs = [":openapi-3.1.yaml"],
        )
    if internal_tool:
        _spec_rule(
            name = "internal_spec_3_0",
            out = "internal-openapi-3.0.yaml",
            tool = internal_tool,
            fmt = "3.0",
        )
        _spec_rule(
            name = "internal_spec_3_1",
            out = "internal-openapi-3.1.yaml",
            tool = internal_tool,
            fmt = "3.1",
        )

def verself_oapi_go_client(
        name,
        package,
        spec,
        importpath,
        deps = None,
        extra_srcs = None,
        response_type_suffix = "",
        visibility = None):
    deps = deps or []
    extra_srcs = extra_srcs or []
    visibility = visibility or ["//visibility:public"]
    suffix_flag = ""
    if response_type_suffix:
        suffix_flag = " -response-type-suffix %s" % response_type_suffix
    native.genrule(
        name = "client_gen",
        srcs = [spec],
        outs = ["client.gen.go"],
        cmd = "PATH=/usr/local/go/bin:/usr/bin:/bin $(location %s) -generate types,client%s -package %s -o $@ $(location %s)" % (OAPI_CODEGEN, suffix_flag, package, spec),
        tools = [OAPI_CODEGEN],
    )
    go_library(
        name = name,
        srcs = [":client_gen"] + extra_srcs,
        importpath = importpath,
        visibility = visibility,
        deps = deps,
    )
