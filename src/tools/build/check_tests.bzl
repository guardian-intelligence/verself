"""Small adapters that make build-style checks visible to `bazel test //...`."""

def _stamp_test_impl(ctx):
    files = ctx.files.target
    if len(files) == 0:
        fail("%s target produced no files to assert" % ctx.label)

    checks = []
    for f in files:
        checks.append("""if [ ! -e "$root/{path}" ]; then
  echo "missing declared check output: {path}" >&2
  missing=1
fi""".format(path = f.short_path))

    executable = ctx.actions.declare_file(ctx.label.name + ".sh")
    ctx.actions.write(
        output = executable,
        is_executable = True,
        content = """#!/usr/bin/env bash
set -euo pipefail
if [ -z "${{TEST_SRCDIR:-}}" ] || [ -z "${{TEST_WORKSPACE:-}}" ]; then
  echo "Bazel test runfiles environment is missing" >&2
  exit 1
fi
root="${{TEST_SRCDIR}}/${{TEST_WORKSPACE}}"
missing=0
{checks}
exit "$missing"
""".format(checks = "\n".join(checks)),
    )

    return [DefaultInfo(
        executable = executable,
        runfiles = ctx.runfiles(files = files),
    )]

stamp_test = rule(
    implementation = _stamp_test_impl,
    attrs = {
        "target": attr.label(mandatory = True),
    },
    test = True,
)
