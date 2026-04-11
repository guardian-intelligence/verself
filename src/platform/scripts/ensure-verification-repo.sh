#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

output_json="${1:-}"
repo_name="${VERIFICATION_REPO_NAME:-sandbox-verification-metadata}"
fixture_dir="${VERIFICATION_FIXTURE_DIR:-${VERIFICATION_PLATFORM_ROOT}/test/fixtures/metadata-repo}"
revision="${VERIFICATION_REPO_REVISION:-seed}"

if [[ ! -d "${fixture_dir}" ]]; then
  echo "fixture directory not found: ${fixture_dir}" >&2
  exit 1
fi

owner="${VERIFICATION_REPO_OWNER:-forgejo-automation}"
public_base_url="${FORGEJO_PUBLIC_URL:-https://git.${VERIFICATION_DOMAIN}}"
loopback_base_url="${FORGEJO_LOOPBACK_URL:-http://127.0.0.1:3000}"

password="$(
  verification_remote_sudo_cat /etc/credstore/forgejo/automation-token
)"

if [[ -z "${owner}" || -z "${password}" ]]; then
  echo "failed to resolve Forgejo verification repo credentials" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
repo_check_body="${tmp_dir}/repo-check.json"
cleanup() {
  rm -rf "${tmp_dir}"
}
trap cleanup EXIT

repo_get_code="$(
  curl -sS -o "${repo_check_body}" -w '%{http_code}' \
    -u "${owner}:${password}" \
    "${public_base_url}/api/v1/repos/${owner}/${repo_name}"
)"

case "${repo_get_code}" in
  200)
    ;;
  404)
    create_body="${tmp_dir}/create-repo.json"
    python3 - "${repo_name}" >"${create_body}" <<'PY'
import json
import sys

repo_name = sys.argv[1]
payload = {
    "name": repo_name,
    "auto_init": False,
    "default_branch": "main",
    "description": "Public metadata-only repository for sandbox live verification",
    "private": False,
}
json.dump(payload, sys.stdout)
PY
    curl -fsS \
      -u "${owner}:${password}" \
      -H 'Content-Type: application/json' \
      -d @"${create_body}" \
      "${public_base_url}/api/v1/user/repos" >/dev/null
    ;;
  *)
    echo "unexpected Forgejo repo lookup status ${repo_get_code}" >&2
    cat "${repo_check_body}" >&2
    exit 1
    ;;
esac

push_dir="${tmp_dir}/repo"
mkdir -p "${push_dir}"
cp -R "${fixture_dir}/." "${push_dir}/"
mkdir -p "${push_dir}/.verification"
printf '%s\n' "${revision}" >"${push_dir}/.verification/revision.txt"

git -C "${push_dir}" init --initial-branch=main >/dev/null
git -C "${push_dir}" config user.name "forge-metal verification"
git -C "${push_dir}" config user.email "verification@forge-metal.local"
git -C "${push_dir}" add .
git -C "${push_dir}" commit --allow-empty -m "Seed sandbox verification fixture ${revision}" >/dev/null

mapfile -t encoded_creds < <(python3 - "${owner}" "${password}" <<'PY'
import sys
from urllib.parse import quote

print(quote(sys.argv[1], safe=""))
print(quote(sys.argv[2], safe=""))
PY
)
encoded_owner="${encoded_creds[0]}"
encoded_password="${encoded_creds[1]}"
push_url="${public_base_url/https:\/\//https://${encoded_owner}:${encoded_password}@}/${owner}/${repo_name}.git"

git -C "${push_dir}" remote add origin "${push_url}"
GIT_TERMINAL_PROMPT=0 git -C "${push_dir}" push --force origin HEAD:refs/heads/main >/dev/null

commit_sha="$(git -C "${push_dir}" rev-parse HEAD)"
public_repo_url="${public_base_url}/${owner}/${repo_name}.git"
loopback_repo_url="${loopback_base_url}/${owner}/${repo_name}.git"
browse_url="${public_base_url}/${owner}/${repo_name}"

if [[ -n "${output_json}" ]]; then
  mkdir -p "$(dirname "${output_json}")"
fi

python3 - "${output_json}" "${owner}" "${repo_name}" "${commit_sha}" "${public_base_url}" "${public_repo_url}" "${loopback_repo_url}" "${browse_url}" <<'PY'
import json
import os
import sys

output_path, owner, repo_name, commit_sha, public_base_url, public_repo_url, loopback_repo_url, browse_url = sys.argv[1:9]
payload = {
    "owner": owner,
    "repo_name": repo_name,
    "public_base_url": public_base_url,
    "public_repo_url": public_repo_url,
    "loopback_repo_url": loopback_repo_url,
    "browse_url": browse_url,
    "ref": "refs/heads/main",
    "commit_sha": commit_sha,
}

serialized = json.dumps(payload, indent=2)
if output_path:
    with open(output_path, "w", encoding="utf-8") as fh:
        fh.write(serialized)
        fh.write("\n")
else:
    print(serialized)
PY
