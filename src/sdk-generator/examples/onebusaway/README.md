# Example: OneBusAway

A **real, public** Stainless input — `stainless.yml` (note its
`$schema=https://app.stainlessapi.com/...` header) + `openapi.yml`, verbatim
from the Apache-2.0 [`OneBusAway/sdk-config`](https://github.com/OneBusAway/sdk-config)
repo. This is the single source of truth: the conformance suite tests against
these exact files, and `sdk/` is the SDK stainful generates from them,
**committed**.

## One command

```bash
uv run stainful generate \
  --spec examples/onebusaway/openapi.yml \
  --config examples/onebusaway/stainless.yml \
  --out examples/onebusaway/sdk
```

→ `Generated SDK at examples/onebusaway/sdk`. That's it. Run it again to
**regenerate** — same command, in place.

## Dogfood

`sdk/` is checked in. CI (`tests/test_dogfood.py`) regenerates it and fails
if it differs by a byte — so the command above is provably real, the example
can't drift, and "regenerate on spec change" is enforced, not claimed.

```bash
# silent proof: regenerate, nothing changes
uv run stainful generate --spec examples/onebusaway/openapi.yml \
  --config examples/onebusaway/stainless.yml --out examples/onebusaway/sdk
git diff --stat examples/onebusaway/sdk      # (empty — byte-identical)
```

## What you get

`sdk/onebusaway/` — an idiomatic Python SDK: `OnebusawaySDK` /
`AsyncOnebusawaySDK` (the same client class the real Stainless-generated
OneBusAway SDK exposes — existing imports keep working), nested resource
clients, per-file typed models, typed errors, retries, vendored runtime.
Measured vs the real Stainless SDK: **1.00** resource-method recall, **0.99**
signature match, **mypy-clean**, **28/29** of Stainless's own test files
import unchanged.
