"""stainful — open-source, drop-in stainless.yml-compatible Python SDK generator.

Pipeline (DESIGN.md §1), four public functions, one per module:

    config.loader.load_config(path)   -> Config
    openapi.loader.load_spec(path)    -> OpenAPIDocument
    ir.builder.build_ir(spec, config) -> API
    emit.python.emit(api, out_dir)    -> None
"""

from importlib.metadata import PackageNotFoundError, version

try:  # single source of truth = pyproject; never drifts
    __version__ = version("stainful")
except PackageNotFoundError:  # not installed (raw source tree)
    __version__ = "0.0.0+dev"
