"""Bazel helpers for owner-local deployment declarations."""

def deploy_declarations(name = "deploy_declarations", visibility = None):
    native.filegroup(
        name = name,
        srcs = native.glob(["deploy/**"], allow_empty = True),
        visibility = visibility,
    )
