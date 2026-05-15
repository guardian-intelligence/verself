"""Rules for exposing generated Verself route catalog artifacts."""

load("//src/smithy/build:openapi.bzl", "smithy_projection_file")

def verself_route_catalog(name, service, out):
    """Expose a generated route catalog for one Smithy service shape.

    Args:
      name: Bazel target name.
      service: Smithy service shape name, for example "Profile".
      out: Output file name.
    """

    smithy_projection_file(
        name = name,
        out = out,
        build = "//src/smithy/models/verself:smithy_build",
        path = "source/verselfRouteCatalog/route-catalog/%s.json" % service,
    )
