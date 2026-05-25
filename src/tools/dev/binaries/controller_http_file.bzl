"""Host-selected, checksum-pinned controller tool downloads."""

def _asset_for_controller(repository_ctx):
    os_name = repository_ctx.os.name.lower()
    arch = repository_ctx.os.arch.lower()

    if os_name == "linux" and arch in ["amd64", "x86_64"]:
        return struct(
            downloaded_file_path = repository_ctx.attr.linux_amd64_downloaded_file_path,
            sha256 = repository_ctx.attr.linux_amd64_sha256,
            url = repository_ctx.attr.linux_amd64_url,
        )

    if os_name in ["darwin", "mac os x"] and arch in ["aarch64", "arm64"]:
        return struct(
            downloaded_file_path = repository_ctx.attr.darwin_arm64_downloaded_file_path,
            sha256 = repository_ctx.attr.darwin_arm64_sha256,
            url = repository_ctx.attr.darwin_arm64_url,
        )

    fail("unsupported controller platform for dev tool download: {} {}; supported controllers are Linux x86_64 and macOS Apple Silicon".format(
        repository_ctx.os.name,
        repository_ctx.os.arch,
    ))

def _controller_http_file_impl(repository_ctx):
    asset = _asset_for_controller(repository_ctx)
    output = "file/" + asset.downloaded_file_path
    repository_ctx.download(
        output = output,
        sha256 = asset.sha256,
        url = asset.url,
    )
    repository_ctx.file(
        "file/BUILD.bazel",
        """package(default_visibility = ["//visibility:public"])

exports_files(["{downloaded_file_path}"])

filegroup(
    name = "file",
    srcs = ["{downloaded_file_path}"],
)
""".format(downloaded_file_path = asset.downloaded_file_path),
    )

controller_http_file = repository_rule(
    implementation = _controller_http_file_impl,
    attrs = {
        "darwin_arm64_downloaded_file_path": attr.string(mandatory = True),
        "darwin_arm64_sha256": attr.string(mandatory = True),
        "darwin_arm64_url": attr.string(mandatory = True),
        "linux_amd64_downloaded_file_path": attr.string(mandatory = True),
        "linux_amd64_sha256": attr.string(mandatory = True),
        "linux_amd64_url": attr.string(mandatory = True),
    },
)
