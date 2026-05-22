"""Bazel rule for the controller-local Ruby dev tool."""

def _ruby_tool_impl(ctx):
    runtime = ctx.actions.declare_directory(ctx.label.name + ".runtime")
    launcher = ctx.actions.declare_file(ctx.label.name)
    runtime_basename = ctx.label.name + ".runtime"
    args = ctx.actions.args()
    args.add(ctx.file.src)
    args.add(runtime.path)
    args.add(launcher)
    args.add(runtime_basename)
    args.add(ctx.attr.version)
    args.add(ctx.attr.ruby_api_version)
    ctx.actions.run_shell(
        inputs = [ctx.file.src],
        outputs = [runtime, launcher],
        arguments = [args],
        mnemonic = "BuildRubyTool",
        command = r"""
set -euo pipefail
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

source_archive="$1"
runtime="$2"
launcher="$3"
runtime_basename="$4"
ruby_version="$5"
ruby_api_version="$6"

runtime_abs="$PWD/$runtime"
launcher_abs="$PWD/$launcher"

rm -rf "$runtime_abs"
mkdir -p "$runtime_abs"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

tar -xzf "$source_archive" -C "$tmp" --strip-components=1
cd "$tmp"

jobs="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)"
./configure \
  --prefix=/ \
  --enable-load-relative \
  --disable-install-doc \
  --disable-yjit \
  --disable-zjit \
  --with-libffi-dir=/usr \
  --with-libyaml-dir=/usr \
  --with-openssl-dir=/usr \
  --with-zlib-dir=/usr
make -j"$jobs"
make install DESTDIR="$runtime_abs"
"$runtime_abs/bin/ruby" -e 'abort "expected #{ARGV[0]}, got #{RUBY_VERSION}" unless RUBY_VERSION == ARGV[0]' "$ruby_version"
"$runtime_abs/bin/ruby" -e 'require "fiddle"; require "openssl"; require "psych"; require "readline"; require "zlib"'

cat > "$launcher_abs" <<EOF
#!/usr/bin/env bash
set -euo pipefail

tool="\$(basename "\$0")"
case "\$tool" in
  ruby|gem|bundle|bundler|erb|irb|rake|rdoc|ri) ;;
  *) tool="ruby" ;;
esac

self="\${BASH_SOURCE[0]}"
while [ -L "\$self" ]; do
  self_dir="\$(cd -P "\$(dirname "\$self")" >/dev/null 2>&1 && pwd)"
  target="\$(readlink "\$self")"
  case "\$target" in
    /*) self="\$target" ;;
    *) self="\$self_dir/\$target" ;;
  esac
done

launcher_dir="\$(cd -P "\$(dirname "\$self")" >/dev/null 2>&1 && pwd)"
runtime="\$launcher_dir/$runtime_basename"
ruby="\$runtime/bin/ruby"
target="\$runtime/bin/\$tool"
runtime_gem_home="\$runtime/lib/ruby/gems/$ruby_api_version"

if [ ! -x "\$ruby" ]; then
  echo "ruby runtime is missing at \$ruby" >&2
  exit 127
fi
if [ "\$tool" != "ruby" ] && [ ! -f "\$target" ]; then
  echo "ruby tool \$tool is missing at \$target" >&2
  exit 127
fi

if [ -n "\${HOME:-}" ]; then
  export GEM_HOME="\${GEM_HOME:-\$HOME/.local/share/verself/ruby/$ruby_version}"
  export GEM_PATH="\${GEM_PATH:-\$GEM_HOME:\$runtime_gem_home}"
  export GEM_SPEC_CACHE="\${GEM_SPEC_CACHE:-\$HOME/.cache/verself/ruby/$ruby_version/gem-specs}"
  mkdir -p "\$GEM_HOME" "\$GEM_SPEC_CACHE"
  export PATH="\$GEM_HOME/bin:\$runtime/bin:\${PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}"
else
  export PATH="\$runtime/bin:\${PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}"
fi
if [ "\$tool" = "ruby" ]; then
  exec "\$ruby" "\$@"
fi
exec "\$ruby" "\$target" "\$@"
EOF

chmod 0755 "$launcher_abs"
""",
    )
    return [DefaultInfo(
        executable = launcher,
        files = depset([launcher]),
    )]

ruby_tool = rule(
    implementation = _ruby_tool_impl,
    attrs = {
        "src": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "version": attr.string(mandatory = True),
        "ruby_api_version": attr.string(mandatory = True),
    },
    executable = True,
)
