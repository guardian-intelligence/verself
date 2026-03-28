#!/usr/bin/env bash
# Inspect cached npm tarballs for suspicious content.
# Adapted from bare-metal-ci-bench-v2.
#
# Checks for: lifecycle scripts (pre/post install), native builds (binding.gyp), bin entries.
set -euo pipefail

STORAGE="${1:-/var/lib/verdaccio/storage}"
ISSUES=0

echo "Inspecting tarballs in $STORAGE..."

find "$STORAGE" -name '*.tgz' -type f | while read -r tgz; do
    pkg=$(basename "$(dirname "$tgz")")

    # Extract package.json from tarball
    pjson=$(tar -xzf "$tgz" -O package/package.json 2>/dev/null) || continue

    # Check lifecycle scripts
    for hook in preinstall install postinstall; do
        if echo "$pjson" | jq -e ".scripts.\"$hook\"" > /dev/null 2>&1; then
            echo "WARNING: $pkg has $hook script"
            ISSUES=$((ISSUES + 1))
        fi
    done

    # Check for native builds
    if tar -tzf "$tgz" 2>/dev/null | grep -q 'binding.gyp'; then
        echo "WARNING: $pkg contains binding.gyp (native build)"
        ISSUES=$((ISSUES + 1))
    fi

    # Check bin entries
    if echo "$pjson" | jq -e '.bin' > /dev/null 2>&1; then
        echo "INFO: $pkg has bin entries"
    fi
done

if [[ $ISSUES -gt 0 ]]; then
    echo "$ISSUES issues found. Review before sealing registry."
    exit 1
else
    echo "No suspicious content found."
fi
