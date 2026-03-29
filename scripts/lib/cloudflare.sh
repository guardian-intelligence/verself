#!/usr/bin/env bash
# Cloudflare token validation library.
# Sourced by setup-domain.sh and tests. No side effects on source.

# classify_cf_token <zones_api_response> <domain>
#
# Classifies a Cloudflare API token based on the /zones?name=<domain> response.
#
# Exit codes:
#   0 — valid token with DNS edit permissions for the domain
#   1 — not a valid Cloudflare API token
#   2 — valid token but missing required permissions
#
# Prints a human-readable message to stdout explaining the result.
classify_cf_token() {
  local response="$1"
  local domain="$2"

  # Check if the API call succeeded at all
  local success
  success=$(echo "$response" | jq -r '.success // false')
  if [[ "$success" != "true" ]]; then
    local error_msg
    error_msg=$(echo "$response" | jq -r '.errors[0].message // "unknown error"')
    echo "Invalid API token ($error_msg)."
    echo ""
    echo "Check that you copied the full token value."
    return 1
  fi

  # Check if the token can see the zone
  local zone_count
  zone_count=$(echo "$response" | jq -r '.result | length')
  if [[ "$zone_count" == "0" ]]; then
    echo "Token does not have access to zone \"$domain\"."
    echo ""
    echo "When creating the token, under Zone Resources select:"
    echo "  Include → Specific zone → $domain"
    return 2
  fi

  # Check for dns_records:edit permission
  local has_dns_edit
  has_dns_edit=$(echo "$response" | jq -r \
    --arg domain "$domain" \
    '.result[] | select(.name == $domain) | .permissions | index("#dns_records:edit") // empty')
  if [[ -z "$has_dns_edit" ]]; then
    local perms
    perms=$(echo "$response" | jq -r \
      --arg domain "$domain" \
      '.result[] | select(.name == $domain) | .permissions | join(", ")')
    echo "Token can access \"$domain\" but lacks DNS edit permission."
    echo "  Current permissions: $perms"
    echo ""
    echo "Create a new token using the 'Edit zone DNS' template:"
    echo "  1. Go to your Cloudflare profile → API Tokens"
    echo "  2. Click 'Create Token'"
    echo "  3. Use the 'Edit zone DNS' template"
    echo "  4. Under Zone Resources, select: $domain"
    return 2
  fi

  echo "Valid DNS token for $domain."
  return 0
}
