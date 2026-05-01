#!/usr/bin/env bash
# Issue or refresh a short-lived SSH cert via OpenBao's OIDC-bound SSH CA.
set -euo pipefail

ROLE=operator
REFRESH_WINDOW_SECONDS=300
QUIET=0
IF_NEEDED=0
ALLOW_OIDC=1

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
        # ssh_config hook mode must never launch browser/OIDC. It may refresh
        # from a cached token, then lets SSH reuse an existing control master.
        IF_NEEDED=1
        QUIET=1
        ALLOW_OIDC=0
        shift
        ;;
    --allow-oidc)
        ALLOW_OIDC=1
        shift
        ;;
    --refresh-window-seconds)
        REFRESH_WINDOW_SECONDS="$2"
        shift 2
        ;;
    --refresh-window-seconds=*)
        REFRESH_WINDOW_SECONDS="${1#--refresh-window-seconds=}"
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

if [[ ! "$REFRESH_WINDOW_SECONDS" =~ ^[0-9]+$ ]]; then
    echo "ssh-cert.sh: --refresh-window-seconds must be an unsigned integer" >&2
    exit 2
fi

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
BAO_TOKEN_FILE="${BAO_TOKEN_FILE:-$HOME/.vault-token}"

cert_remaining_seconds() {
    [[ -f "$CERT" ]] || { echo 0; return; }
    local until_iso now until
    until_iso=$(ssh-keygen -L -f "$CERT" 2>/dev/null \
        | awk '/Valid: from/ { for (i=1;i<=NF;i++) if ($i=="to") { print $(i+1); exit } }')
    [[ -n "$until_iso" ]] || { echo 0; return; }
    now=$(date +%s)
    until=$(date -d "$until_iso" +%s 2>/dev/null || echo 0)
    if (( until > now )); then
        echo $((until - now))
    else
        echo 0
    fi
}

cert_has_required_extensions() {
    [[ -f "$CERT" ]] || return 1
    case "$ROLE" in
        operator|breakglass)
            ssh-keygen -L -f "$CERT" 2>/dev/null | grep -q 'permit-port-forwarding'
            ;;
        *)
            return 0
            ;;
    esac
}

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
    local tmp
    tmp=$(mktemp "${CERT}.tmp.XXXXXX")
    trap 'rm -f "$tmp"' RETURN
    # Do not truncate the last usable cert if OpenBao rejects the signing call.
    BAO_TOKEN="$tok" "$BAO" write -field=signed_key "ssh-ca/sign/${ROLE}" \
        "public_key=@${KEY}.pub" \
        "valid_principals=${ROLE}" >"$tmp"
    mv "$tmp" "$CERT"
    trap - RETURN
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
cert_needs_refresh=0
refresh_reason="cert expiring in $remaining seconds"
if ! cert_has_required_extensions; then
    cert_needs_refresh=1
    refresh_reason="cert missing required role extensions"
elif (( remaining < REFRESH_WINDOW_SECONDS )); then
    cert_needs_refresh=1
fi

if (( cert_needs_refresh == 0 )); then
    if (( IF_NEEDED == 1 )); then
        exit 0
    fi
    log "cert valid for $remaining more seconds; nothing to do"
    exit 0
fi

if token_is_valid; then
    log "${refresh_reason}; re-signing with cached Vault token"
    sign_with_token "$(<"$BAO_TOKEN_FILE")"
else
    if (( ALLOW_OIDC == 0 )); then
        log "${refresh_reason}; cached Vault token is unavailable, skipping interactive OIDC in hook mode"
        exit 0
    fi
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
