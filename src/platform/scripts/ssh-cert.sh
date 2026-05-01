#!/usr/bin/env bash
# aspect ssh cert — issue (or refresh) a short-lived SSH cert via
# OpenBao's OIDC-bound SSH CA.
#
# Three states the script handles, fastest first:
#
#   1. Cert valid for >= refresh window: exit 0 silently. Safe to wire
#      into ssh's `Match exec` so every connection runs this and only
#      pays the cost when a refresh is actually needed.
#
#   2. Cert expired or expiring soon, but the cached Vault token at
#      ~/.bao-token is still valid: re-sign via the cached token.
#      Sub-second, no browser, no SSH.
#
#   3. Token also expired: full OIDC flow (`bao login -method=oidc`).
#      Opens a browser; takes a few seconds.
#
# First-run bootstrap also fetches the bao CLI and OpenBao TLS CA cert
# from the host over SSH (works pre-cutover via static keys; post-cutover
# via the cert this script just signed). Subsequent runs are SSH-free.
#
# Source of truth for principals/CA paths: src/cue-renderer/instances/prod/config.cue.

set -euo pipefail

ROLE=operator
# Refresh certs that expire within this many seconds. 5 min covers a
# normal interactive session; the cert TTL itself sets the upper bound
# on how long a re-signed cert lasts.
REFRESH_WINDOW_SECONDS=300
QUIET=0
IF_NEEDED=0

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
    --if-needed)
        # Idempotent mode: exit 0 silently when no refresh is required.
        # Designed for `Match exec` in ssh_config so every ssh runs the
        # script with near-zero cost when the cert is fresh.
        IF_NEEDED=1
        QUIET=1
        shift
        ;;
    --quiet)
        QUIET=1
        shift
        ;;
    *)
        echo "ssh-cert.sh: unknown argument: $1" >&2
        exit 2
        ;;
    esac
done

log() { [[ "$QUIET" == 1 ]] && return 0; echo "ssh-cert: $*" >&2; }

HOST_WG_IP="${VERSELF_HOST_WG:-10.66.66.1}"
SSH_USER="${VERSELF_SSH_USER:-ubuntu}"

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/verself"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/verself"
BAO="${BAO_BIN:-$CACHE_DIR/bin/bao}"
BAO_CACERT="${BAO_CACERT:-$CONFIG_DIR/openbao-ca.pem}"
BAO_ADDR="https://${HOST_WG_IP}:8200"

KEY="${VERSELF_SSH_KEY:-$HOME/.ssh/id_verself}"
CERT="${KEY}-cert.pub"
# OpenBao's CLI writes the post-login token to the Vault-default
# ~/.vault-token, not ~/.bao-token. The env override exists so an
# operator running multiple OpenBao environments can keep token files
# separate.
BAO_TOKEN_FILE="${BAO_TOKEN_FILE:-$HOME/.vault-token}"

# cert_remaining_seconds prints the seconds-until-expiry of the cert at
# $CERT, or 0 if there is no cert. Uses ssh-keygen -L to read the
# `Valid: from <ISO> to <ISO>` line — the only timestamps in the cert
# are seconds-resolution UTC, which `date -d` handles natively.
cert_remaining_seconds() {
    [[ -f "$CERT" ]] || { echo 0; return; }
    local until_iso now until
    until_iso=$(ssh-keygen -L -f "$CERT" 2>/dev/null \
        | awk '/Valid: from/ { for (i=1;i<=NF;i++) if ($i=="to") { print $(i+1); exit } }')
    [[ -n "$until_iso" ]] || { echo 0; return; }
    now=$(date -u +%s)
    until=$(date -u -d "$until_iso" +%s 2>/dev/null || echo 0)
    if (( until > now )); then
        echo $((until - now))
    else
        echo 0
    fi
}

# token_is_valid succeeds iff $BAO_TOKEN_FILE holds a Vault token that
# OpenBao still accepts. Cheap call — a 200 from /v1/auth/token/lookup-self
# is the standard liveness check.
token_is_valid() {
    [[ -f "$BAO_TOKEN_FILE" ]] || return 1
    local tok
    tok=$(<"$BAO_TOKEN_FILE")
    [[ -n "$tok" ]] || return 1
    curl -fsS --max-time 5 --cacert "$BAO_CACERT" \
        -H "X-Vault-Token: $tok" \
        "${BAO_ADDR}/v1/auth/token/lookup-self" >/dev/null 2>&1
}

bootstrap_bao_binary() {
    if [[ -x "$BAO" ]]; then
        return
    fi
    log "bootstrapping bao binary from ${SSH_USER}@${HOST_WG_IP}"
    mkdir -p "$(dirname "$BAO")"
    scp -q "${SSH_USER}@${HOST_WG_IP}:/opt/verself/profile/bin/bao" "$BAO"
    chmod +x "$BAO"
}

ensure_bao_cacert() {
    if [[ -f "$BAO_CACERT" ]] && \
       curl -fsS --max-time 5 --cacert "$BAO_CACERT" \
            "${BAO_ADDR}/v1/sys/health" >/dev/null 2>&1; then
        return
    fi
    log "refreshing OpenBao CA cert from ${SSH_USER}@${HOST_WG_IP}"
    mkdir -p "$(dirname "$BAO_CACERT")"
    ssh -q -o BatchMode=yes "${SSH_USER}@${HOST_WG_IP}" \
        'sudo cat /etc/openbao/tls/cert.pem' >"$BAO_CACERT"
}

ensure_keypair() {
    if [[ -f "$KEY" ]]; then
        return
    fi
    log "generating new ed25519 keypair at $KEY"
    mkdir -p "$(dirname "$KEY")"
    ssh-keygen -t ed25519 -f "$KEY" -N "" -C "verself-${ROLE}-$(whoami)@$(hostname -s)"
}

sign_with_token() {
    local tok="$1"
    "$BAO" write -field=signed_key "ssh-ca/sign/${ROLE}" \
        "public_key=@${KEY}.pub" \
        "valid_principals=${ROLE}" >"$CERT"
}

oidc_login() {
    log "signing in via OpenBao OIDC (browser will open)"
    "$BAO" login -method=oidc -path=oidc-ssh-ca -no-print "role=${ROLE}"
}

bootstrap_bao_binary
ensure_bao_cacert
ensure_keypair

export BAO_ADDR
export BAO_CACERT
export VAULT_ADDR="$BAO_ADDR"
export VAULT_CACERT="$BAO_CACERT"

remaining=$(cert_remaining_seconds)
if (( remaining >= REFRESH_WINDOW_SECONDS )); then
    if (( IF_NEEDED == 1 )); then
        exit 0
    fi
    log "cert valid for $remaining more seconds; nothing to do"
    exit 0
fi

# Cert needs refresh. Try cached token first.
if token_is_valid; then
    log "cert expiring in $remaining seconds; re-signing with cached Vault token"
    BAO_TOKEN=$(<"$BAO_TOKEN_FILE") sign_with_token "$(<"$BAO_TOKEN_FILE")"
else
    oidc_login
    sign_with_token "$(<"$BAO_TOKEN_FILE")"
fi

new_remaining=$(cert_remaining_seconds)
log "cert refreshed; valid for $new_remaining more seconds"

if (( QUIET == 0 )); then
    echo
    ssh-keygen -L -f "$CERT"
    echo
    echo "ssh-cert: cert written to $CERT"
    echo "ssh-cert: use: ssh -i $KEY ${SSH_USER}@${HOST_WG_IP}"
fi
