#!/usr/bin/env bash
# aspect ssh cert — issue a short-lived SSH cert via OpenBao's OIDC-bound
# SSH CA. The cert authorises the operator to log into the bare-metal
# node's `ubuntu` account for `--role`'s `max_ttl_seconds` window.
#
# First-run bootstrap: this fetches the `bao` CLI and OpenBao's TLS cert
# from the host over SSH. Pre-cutover both work via the static
# authorized_keys path; after the cutover lands they work via the cert
# this script just issued. Once cached, subsequent runs are SSH-free.
#
# Source of truth for principals/CA paths: src/cue-renderer/instances/prod/config.cue.

set -euo pipefail

ROLE=operator
while [[ $# -gt 0 ]]; do
    case "$1" in
    --role)
        ROLE="$2"
        shift 2
        ;;
    --role=*)
        ROLE="${1#--role=}"
        shift
        ;;
    *)
        echo "ssh-cert.sh: unknown argument: $1" >&2
        exit 2
        ;;
    esac
done

# All wg-ops endpoints live in 10.66.66.0/24 in this instance. Hard-coding
# the bare-metal address avoids needing the topology vars on the
# operator's controller.
HOST_WG_IP="${VERSELF_HOST_WG:-10.66.66.1}"
SSH_USER="${VERSELF_SSH_USER:-ubuntu}"

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/verself"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/verself"
BAO="${BAO_BIN:-$CACHE_DIR/bin/bao}"
BAO_CACERT="${BAO_CACERT:-$CONFIG_DIR/openbao-ca.pem}"
BAO_ADDR="https://${HOST_WG_IP}:8200"

KEY="${VERSELF_SSH_KEY:-$HOME/.ssh/id_verself}"
CERT="${KEY}-cert.pub"

bootstrap_bao_binary() {
    if [[ -x "$BAO" ]]; then
        return
    fi
    echo "ssh-cert: bootstrapping bao binary from ${SSH_USER}@${HOST_WG_IP}" >&2
    mkdir -p "$(dirname "$BAO")"
    # /opt/verself/profile/bin/bao is the substrate path on the box.
    scp -q "${SSH_USER}@${HOST_WG_IP}:/opt/verself/profile/bin/bao" "$BAO"
    chmod +x "$BAO"
}

refresh_bao_cacert_via_ssh() {
    mkdir -p "$(dirname "$BAO_CACERT")"
    ssh -q -o BatchMode=yes "${SSH_USER}@${HOST_WG_IP}" 'sudo cat /etc/openbao/tls/cert.pem' >"$BAO_CACERT"
}

ensure_bao_cacert() {
    # Try the cached cert first. The cert rotates rarely (only on SAN
    # changes), so a recent cache is usually correct. The SSH-based
    # refresh is the fallback — and it depends on having a working
    # auth path to the host, which post-cutover is the cert this very
    # script is about to sign. Falling back to refresh on every run
    # would deadlock when the cached cert is fine but the operator's
    # cert just expired.
    if [[ -f "$BAO_CACERT" ]] && \
       curl -fsS --max-time 5 --cacert "$BAO_CACERT" \
            "${BAO_ADDR}/v1/sys/health" >/dev/null 2>&1; then
        return
    fi
    echo "ssh-cert: refreshing OpenBao CA cert from ${SSH_USER}@${HOST_WG_IP}" >&2
    refresh_bao_cacert_via_ssh
}

ensure_keypair() {
    if [[ -f "$KEY" ]]; then
        return
    fi
    echo "ssh-cert: generating new ed25519 keypair at $KEY" >&2
    mkdir -p "$(dirname "$KEY")"
    ssh-keygen -t ed25519 -f "$KEY" -N "" -C "verself-${ROLE}-$(whoami)@$(hostname -s)"
}

bootstrap_bao_binary
ensure_bao_cacert
ensure_keypair

export BAO_ADDR
export BAO_CACERT
export VAULT_ADDR="$BAO_ADDR"
export VAULT_CACERT="$BAO_CACERT"

# `bao login -method=oidc` opens a browser, listens on
# 127.0.0.1:8250/oidc/callback (its default), exchanges the Zitadel code
# for a Vault token, and writes the token to ~/.bao-token. -no-print
# keeps the token off the operator's terminal.
echo "ssh-cert: signing in via OpenBao OIDC (browser will open)" >&2
"$BAO" login -method=oidc -path=oidc-ssh-ca -no-print "role=${ROLE}"

# `bao write -field=signed_key` returns just the cert string; > redirect
# captures it. valid_principals tells the CA to stamp this cert with
# the role name; the on-host AuthorizedPrincipalsFile must list the same
# name or sshd refuses the connection.
echo "ssh-cert: signing public key for role=${ROLE}" >&2
"$BAO" write -field=signed_key "ssh-ca/sign/${ROLE}" \
    "public_key=@${KEY}.pub" \
    "valid_principals=${ROLE}" >"$CERT"

echo
ssh-keygen -L -f "$CERT"
echo
echo "ssh-cert: cert written to $CERT"
echo "ssh-cert: use: ssh -i $KEY ${SSH_USER}@${HOST_WG_IP}"
