"""Bazel macros for OpenAPI artifact generation.

Two boundaries, each modeled as a Bazel action so the action graph
propagates input changes (Huma route source → spec, spec → client):

  verself_openapi_yaml         — invoke a service's <svc>-openapi binary
                                 to render its spec to a Bazel-tracked
                                 YAML file. Mnemonic OpenAPISpec.
  verself_openapi_go_client    — invoke oapi-codegen against a YAML spec
                                 and wrap the emitted client.gen.go in a
                                 go_library. Mnemonic OAPICodegen.

Both mnemonics flow through src/otel/cmd/bazel-profile-to-otel into
default.otel_traces and are queryable per deploy_run_key — see the
deploy.codegen_actions and deploy.rebuild_blast_radius observe queries
for the operator-facing surface.
"""

load("@bazel_lib//lib:run_binary.bzl", "run_binary")
load("@io_bazel_rules_go//go:def.bzl", "go_library")

# In-process wrapper around oapi-codegen + goimports. The upstream
# binary shells out to the Go toolchain at format time, which the
# action sandbox does not provide on PATH. The wrapper keeps the
# OAPICodegen action self-contained, deterministic, and cacheable.
OAPI_CODEGEN_TOOL = "//tools/codegen/cmd/oapicodegen"

def verself_openapi_yaml(name, openapi_binary, format, out, visibility = None):
    """Render the OpenAPI YAML for one service+format into a Bazel-tracked file.

    `openapi_binary` must accept `--format <fmt> --output <path>` and
    write the YAML bytes to <path>. run_binary cannot capture stdout,
    so the binary owns writing to the path Bazel allocated.
    """
    run_binary(
        name = name,
        tool = openapi_binary,
        args = [
            "--format",
            format,
            "--output",
            "$(execpath %s)" % out,
        ],
        outs = [out],
        mnemonic = "OpenAPISpec",
        progress_message = "Rendering OpenAPI %s spec %%{output}" % format,
        visibility = visibility,
    )

def verself_openapi_go_client(
        name,
        spec,
        package_name,
        importpath,
        out = "client.gen.go",
        visibility = None):
    """Generate a Go OpenAPI client (types + client) from `spec`.

    Args:
      name: name of the wrapping go_library target. Consumers depend on
        this label.
      spec: Bazel label of the OpenAPI YAML (typically a verself_openapi_yaml
        output) that oapi-codegen reads.
      package_name: Go package name embedded in the generated file.
      importpath: full Go import path consumers use.
      out: filename for the generated source. Default "client.gen.go".
      visibility: visibility of the wrapping go_library.
    """
    gen_target = name + "_gen"
    run_binary(
        name = gen_target,
        tool = OAPI_CODEGEN_TOOL,
        srcs = [spec],
        args = [
            "--spec",
            "$(execpath %s)" % spec,
            "--package",
            package_name,
            "--output",
            "$(execpath %s)" % out,
        ],
        outs = [out],
        mnemonic = "OAPICodegen",
        progress_message = "Generating Go OpenAPI client %s" % importpath,
    )
    go_library(
        name = name,
        srcs = [":" + gen_target],
        importpath = importpath,
        visibility = visibility or ["//visibility:public"],
        deps = ["@com_github_oapi_codegen_runtime//:runtime"],
    )
