#!/usr/bin/env bash
# Seal or unseal the Verdaccio npm registry.
# Adapted from bare-metal-ci-bench-v2.
#
# seal:   removes proxy directives (no upstream fetches)
# unseal: restores proxy for cache warmup
# status: reports current state
set -euo pipefail

VERDACCIO_CONFIG="/etc/verdaccio/config.yaml"

usage() {
    echo "Usage: $0 {seal|unseal|status}"
    exit 1
}

[[ $# -eq 1 ]] || usage

case "$1" in
    seal)
        python3 -c "
import yaml, sys
with open('$VERDACCIO_CONFIG') as f:
    cfg = yaml.safe_load(f)
for pkg in cfg.get('packages', {}).values():
    pkg.pop('proxy', None)
with open('$VERDACCIO_CONFIG', 'w') as f:
    yaml.dump(cfg, f, default_flow_style=False)
"
        systemctl restart verdaccio
        echo "Registry sealed: no upstream proxy."
        ;;
    unseal)
        python3 -c "
import yaml, sys
with open('$VERDACCIO_CONFIG') as f:
    cfg = yaml.safe_load(f)
for pkg in cfg.get('packages', {}).values():
    pkg['proxy'] = 'npmjs'
with open('$VERDACCIO_CONFIG', 'w') as f:
    yaml.dump(cfg, f, default_flow_style=False)
"
        systemctl restart verdaccio
        echo "Registry unsealed: upstream proxy active."
        ;;
    status)
        if grep -q 'proxy' "$VERDACCIO_CONFIG"; then
            echo "UNSEALED"
        else
            echo "SEALED"
        fi
        ;;
    *)
        usage
        ;;
esac
