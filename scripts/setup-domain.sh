#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SECRETS_FILE="$REPO_ROOT/ansible/group_vars/all/secrets.sops.yml"
VARS_FILE="$REPO_ROOT/ansible/group_vars/all/main.yml"

# ── Accept domain as argument or prompt ────────────────────────────
DOMAIN="${1:-}"
if [[ -z "$DOMAIN" ]]; then
  read -rp "Enter your Cloudflare-managed domain (e.g. anveio.com): " DOMAIN
fi

if [[ -z "$DOMAIN" ]]; then
  echo "ERROR: Domain is required."
  exit 1
fi

echo "Domain: $DOMAIN"
echo "  admin.$DOMAIN  → ClickStack dashboard"
echo "  git.$DOMAIN    → Forgejo (when enabled)"
echo ""

# ── Check dependencies ─────────────────────────────────────────────
for cmd in sops ansible-playbook; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: '$cmd' not found. Run: nix develop"
    exit 1
  fi
done

# ── Check SOPS / secrets ──────────────────────────────────────────
if [[ ! -f "$REPO_ROOT/.sops.yaml" ]] || [[ ! -f "$SECRETS_FILE" ]]; then
  echo "Secrets not initialized. Setting up now..."
  echo ""
  "$REPO_ROOT/scripts/setup-sops.sh"
  echo ""
fi

# ── Ensure valid Cloudflare API token ──────────────────────────────
prompt_and_save_token() {
  echo ""
  echo "┌─────────────────────────────────────────────────────────────┐"
  echo "│  Cloudflare API token required                              │"
  echo "├─────────────────────────────────────────────────────────────┤"
  echo "│                                                             │"
  echo "│  1. Log into Cloudflare, go to your profile → API Tokens   │"
  echo "│  2. Click 'Create Token'                                    │"
  echo "│  3. Use the 'Edit zone DNS' template                        │"
  echo "│  4. Under Zone Resources, select: $DOMAIN"
  echo "│  5. Click 'Continue to summary' → 'Create Token'            │"
  echo "│  6. Copy the token and paste it below                       │"
  echo "│                                                             │"
  echo "└─────────────────────────────────────────────────────────────┘"
  echo ""
  read -rsp "Cloudflare API token: " CF_TOKEN_INPUT
  echo ""

  # Trim whitespace
  CF_TOKEN_INPUT=$(echo "$CF_TOKEN_INPUT" | tr -d '[:space:]')

  if [[ -z "$CF_TOKEN_INPUT" ]]; then
    echo "ERROR: Token cannot be empty."
    return 1
  fi

  # Save to encrypted secrets
  sops --set "[\"cloudflare_api_token\"] \"$CF_TOKEN_INPUT\"" "$SECRETS_FILE"
  CF_TOKEN="$CF_TOKEN_INPUT"
}

validate_token() {
  local token="$1"
  # Validate by checking the token can see the actual zone — this works with
  # scoped zone tokens (unlike /user/tokens/verify which rejects them).
  local response
  response=$(curl -s \
    -H "Authorization: Bearer $token" \
    "https://api.cloudflare.com/client/v4/zones?name=$DOMAIN")
  echo "$response" | grep -q '"success":true' && \
  echo "$response" | grep -q "\"name\":\"$DOMAIN\""
}

CF_TOKEN=$(sops -d --extract '["cloudflare_api_token"]' "$SECRETS_FILE" 2>/dev/null || echo "")

# If token exists, validate it first
if [[ -n "$CF_TOKEN" ]]; then
  echo "Validating existing token..."
  if validate_token "$CF_TOKEN"; then
    echo "Cloudflare API token: valid"
  else
    echo "Existing token is invalid."
    CF_TOKEN=""
  fi
fi

# Prompt if no valid token
while [[ -z "$CF_TOKEN" ]]; do
  prompt_and_save_token || continue
  echo "Validating..."
  if validate_token "$CF_TOKEN"; then
    echo "Cloudflare API token: valid"
  else
    echo "ERROR: Cloudflare rejected that token. Try again."
    CF_TOKEN=""
  fi
done

# ── Write domain to group vars ─────────────────────────────────────
if grep -q '^forge_metal_domain:' "$VARS_FILE"; then
  sed -i "s|^forge_metal_domain:.*|forge_metal_domain: \"$DOMAIN\"|" "$VARS_FILE"
else
  echo "" >> "$VARS_FILE"
  echo "forge_metal_domain: \"$DOMAIN\"" >> "$VARS_FILE"
fi

echo "Updated: $VARS_FILE"
echo ""
echo "┌─────────────────────────────────────────────────────────────┐"
echo "│  Domain setup complete                                      │"
echo "├─────────────────────────────────────────────────────────────┤"
echo "│                                                             │"
echo "│  Domain:  $DOMAIN"
echo "│  Token:   valid                                             │"
echo "│                                                             │"
echo "│  Next: make deploy                                          │"
echo "│                                                             │"
echo "│  This will:                                                 │"
echo "│    1. Create A records in Cloudflare for your subdomains    │"
echo "│    2. Caddy obtains Let's Encrypt TLS certificates          │"
echo "│    3. Dashboard live at https://admin.$DOMAIN"
echo "│                                                             │"
echo "└─────────────────────────────────────────────────────────────┘"
