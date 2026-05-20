# SDK Generator Vendor Notes

Vendored from `https://github.com/stainlu/stainful`.

- Upstream commit: `0c4c55a1036a32a2348ce3629703e38605639357`
- License: MIT, preserved in `LICENSE`
- Local package path: `src/sdk-generator`

Local deltas:

- Added Bazel package metadata and a `rules_python` dependency hub lock export.
- Pinned Python dependencies exactly in `pyproject.toml`, generated SDK manifests,
  `uv.lock`, and `requirements_lock.txt`.
- Added a Verself IAM OpenAPI probe under `examples/verself/iam`.
- Fixed emitted auth client option naming so the configured Stainless-compatible
  `client_settings.opts` name is respected instead of hardcoding `api_key`.
- Replaced the optional oracle-fetch shell helper with a Python equivalent to
  satisfy the monorepo no-new-shell-scripts rule.
