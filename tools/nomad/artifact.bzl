load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

def nomad_binary_artifact(name, binary, output, visibility = None):
    """Package one executable for Nomad artifact download.

    The archive extracts to local/bin/<output> inside the task working
    directory, so raw_exec tasks never depend on a host-global /opt path.
    """
    pkg_tar(
        name = name,
        out = output + ".tar",
        files = {binary: "bin/" + output},
        modes = {"bin/" + output: "0755"},
        visibility = visibility or ["//visibility:public"],
    )
