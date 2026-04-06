#!/usr/bin/env bash
# Apply or verify fail-closed network egress policy.
# Adapted from bare-metal-ci-bench-v2.
#
# apply:  block all outbound except localhost services
# remove: restore default (allow all)
# verify: test that policy is working
set -euo pipefail

usage() {
    echo "Usage: $0 {apply|remove|verify}"
    exit 1
}

[[ $# -eq 1 ]] || usage

case "$1" in
    apply)
        # Flush existing rules
        nft flush ruleset

        nft add table inet filter
        nft add chain inet filter output '{ type filter hook output priority 0; policy drop; }'

        # Allow loopback
        nft add rule inet filter output oifname lo accept

        # Allow established connections
        nft add rule inet filter output ct state established,related accept

        # Allow Verdaccio (localhost:4873)
        nft add rule inet filter output ip daddr 127.0.0.1 tcp dport 4873 accept

        # Allow Forgejo (localhost:3000)
        nft add rule inet filter output ip daddr 127.0.0.1 tcp dport 3000 accept

        # Allow systemd-resolved (localhost:53)
        nft add rule inet filter output ip daddr 127.0.0.53 udp dport 53 accept
        nft add rule inet filter output ip daddr 127.0.0.53 tcp dport 53 accept

        # Allow WireGuard (UDP on configured port)
        nft add rule inet filter output udp dport 51820 accept

        echo "Network policy applied (fail-closed)."
        ;;
    remove)
        nft flush ruleset
        echo "Network policy removed (allow all)."
        ;;
    verify)
        echo -n "Testing external connectivity (should fail)... "
        if curl -s --connect-timeout 3 https://httpbin.org/get > /dev/null 2>&1; then
            echo "FAIL: external access is allowed"
            exit 1
        else
            echo "PASS: external access blocked"
        fi

        echo -n "Testing Verdaccio (should succeed)... "
        if curl -s --connect-timeout 3 http://127.0.0.1:4873/ > /dev/null 2>&1; then
            echo "PASS"
        else
            echo "FAIL: Verdaccio unreachable"
            exit 1
        fi
        ;;
    *)
        usage
        ;;
esac
