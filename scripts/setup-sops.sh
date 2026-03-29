#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
AGE_KEY_FILE="${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}"

# ── Check dependencies ──────────────────────────────────────────────
for cmd in age-keygen sops; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: '$cmd' not installed."
    echo "  age:  https://github.com/FiloSottile/age"
    echo "  sops: https://github.com/getsops/sops"
    exit 1
  fi
done

# ── Generate age key ────────────────────────────────────────────────
if [[ ! -f "$AGE_KEY_FILE" ]]; then
  mkdir -p "$(dirname "$AGE_KEY_FILE")"
  age-keygen -o "$AGE_KEY_FILE" 2>&1
  chmod 600 "$AGE_KEY_FILE"
  echo "Age key created: $AGE_KEY_FILE"
else
  echo "Age key exists:  $AGE_KEY_FILE"
fi

AGE_PUB=$(grep -o 'age1[a-z0-9]*' "$AGE_KEY_FILE" | head -1)
echo "Public key:      $AGE_PUB"

# ── Write .sops.yaml ───────────────────────────────────────────────
cat > "$REPO_ROOT/.sops.yaml" <<EOF
# sops configuration for forge-metal secrets.
# The age public key is written by: make setup-sops
#
# Encrypted files (*.sops.yml) are safe to commit.
# The age private key (~/.config/sops/age/keys.txt) is NOT.

creation_rules:
  - path_regex: \\.sops\\.yml\$
    age: >-
      $AGE_PUB
EOF
echo "Wrote .sops.yaml with public key"

# ── Install Ansible collection ──────────────────────────────────────
echo "Installing community.sops Ansible collection..."
ansible-galaxy collection install -r "$REPO_ROOT/ansible/requirements.yml" --force-with-deps 2>&1

# ── Create and encrypt secrets ──────────────────────────────────────
SECRETS_FILE="$REPO_ROOT/ansible/group_vars/all/secrets.sops.yml"
if [[ ! -f "$SECRETS_FILE" ]]; then
  GENERATED_PW=$(openssl rand -base64 18)
  cat > "$SECRETS_FILE" <<EOF
clickstack_admin_email: admin@forge-metal.local
clickstack_admin_password: "${GENERATED_PW}"

# Cloudflare API token — required when forge_metal_domain is set.
# Create a token with Zone:DNS:Edit permission at:
#   https://dash.cloudflare.com/profile/api-tokens
# Use the "Edit zone DNS" template, scoped to your zone.
cloudflare_api_token: ""
EOF
  sops encrypt -i "$SECRETS_FILE"
  echo "Created and encrypted: $SECRETS_FILE"
  echo "  Generated admin password: $GENERATED_PW"
  echo "  (save this -- you'll need it to log in)"
else
  echo "Secrets file exists: $SECRETS_FILE (skipped)"
fi

echo ""
echo "Setup complete. Next steps:"
echo "  Edit secrets:     make edit-secrets"
echo "  Full provision:   make nuke-and-pave"
echo "  Share age key with teammates (securely, never in git):"
echo "    $AGE_KEY_FILE"
