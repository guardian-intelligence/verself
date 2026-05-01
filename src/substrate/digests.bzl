"""Per-layer substrate-digest macro.

Each layer of the substrate converge (L1 OS, L2 userspace, L3 substrate
daemons, L4a per-component + API reconciliation) gets its own content-hash
artifact. aspect deploy uses the artifact to decide whether to skip or run
the layer's playbook on a given converge.
"""

# Footgun: `case` patterns rendered into the genrule cmd are unquoted so bash
# applies them as globs; `*` matches across slashes inside `case`. Patterns
# must use workspace-relative paths because that is what `$(SRCS)` expands to.
_LAYER_DIGEST_CMD = """
set -eu
{{
  for f in $(SRCS); do
    case "$$f" in
          {patterns} )
            if [ -f "$$f" ]; then
              sha256sum "$$f"
            fi
            ;;
    esac
  done
  cat "$(location :prod_rendered_substrate)"
}} | LC_ALL=C sort | sha256sum | cut -d' ' -f1 > "$@"
"""

def layer_digest_genrule(name, output, patterns):
    case_patterns = " | \\\n          ".join(patterns)
    native.genrule(
        name = name,
        srcs = [
            ":substrate_sources",
            ":prod_rendered_substrate",
        ],
        outs = [output],
        cmd = _LAYER_DIGEST_CMD.format(patterns = case_patterns),
    )
