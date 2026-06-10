"""Policy macro for Verself service OCI images.

`verself_service_image` is the single place that decides how a service binary
becomes an OCI image: base image, layer order, entrypoint path, and the child
target names that `//src/tools/deployment/nomad:component.bzl` derives labels
from. Per-service BUILD files get no knobs beyond the binary and the release
output name.

Naming contract, given an instance `name`:

    {name}          oci_image_index (its default output is the OCI layout)
    {name}.digest   index digest file, written as {name}.json.sha256
    {name}_push     oci_push; deployment-service binds --repository at runtime
    {name}_load     loads the linux/amd64 image into the local daemon

Image digests must be a function of build inputs only: no stamping and no
volatile labels, annotations, or env. Runtime config belongs in the owning
nomad.hcl; commit identity travels as an OCI referrer pushed by
deployment-service, never inside the image. Adding such an attribute here
would change every image digest on every commit and defeat the
skip-unchanged-component invariant.
"""

load("@rules_oci//oci:defs.bzl", "oci_image", "oci_image_index", "oci_load", "oci_push")
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

_BASE_LINUX_AMD64 = Label("@ubuntu_noble_base_linux_amd64")
_CA_CERTIFICATES_LAYER = Label("//src/tools/deployment/oci:ubuntu_ca_certificates_layer")

def _validate_output_name(output_name):
    # Mirrors deployengine's ociRepoSegmentRE (^[a-z0-9]+([._-][a-z0-9]+)*$)
    # so a bad OCI repository segment fails at load time, not at deploy time.
    boundary = True
    for ch in output_name.elems():
        if ch.isdigit() or (ch.isalpha() and ch.islower()):
            boundary = False
        elif ch in "._-":
            if boundary:
                fail("output_name %r is not a valid OCI repository segment" % output_name)
            boundary = True
        else:
            fail("output_name %r is not a valid OCI repository segment" % output_name)
    if boundary:
        fail("output_name %r is not a valid OCI repository segment" % output_name)

def _verself_service_image_impl(name, visibility, binary, output_name):
    _validate_output_name(output_name)
    install_path = "usr/local/bin/" + output_name

    pkg_tar(
        name = name + "_layer",
        files = {binary: install_path},
        modes = {install_path: "0755"},
    )

    oci_image(
        name = name + "_linux_amd64",
        base = _BASE_LINUX_AMD64,
        entrypoint = ["/" + install_path],
        tars = [
            _CA_CERTIFICATES_LAYER,
            ":" + name + "_layer",
        ],
    )

    oci_image_index(
        name = name,
        images = [":" + name + "_linux_amd64"],
        visibility = visibility,
    )

    oci_push(
        name = name + "_push",
        image = ":" + name,
        visibility = visibility,
    )

    oci_load(
        name = name + "_load",
        image = ":" + name + "_linux_amd64",
        repo_tags = [output_name + ":dev"],
    )

verself_service_image = macro(
    implementation = _verself_service_image_impl,
    doc = "Builds the deployable OCI image for one service binary.",
    attrs = {
        "binary": attr.label(
            configurable = False,
            mandatory = True,
            doc = "Service binary, installed at /usr/local/bin/<output_name> and used as the entrypoint.",
        ),
        "output_name": attr.string(
            configurable = False,
            mandatory = True,
            doc = "Release output name: the OCI repository segment and the verself-oci:// reference in the owning nomad.hcl.",
        ),
    },
)
