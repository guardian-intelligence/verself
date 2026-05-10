# Verself Checkout

This action is the Verself runner checkout path. It follows the high-level
`actions/checkout` flow:

1. prepare a target directory under `GITHUB_WORKSPACE`
2. initialize or reuse `.git`
3. fetch the requested workflow commit
4. check out the exact `GITHUB_SHA`
5. mark the checkout as a safe Git directory

Reference upstream implementation:

- `https://github.com/actions/checkout/blob/main/action.yml`
- `https://github.com/actions/checkout/blob/main/src/git-source-provider.ts`
- `https://github.com/actions/checkout/blob/main/src/git-directory-helper.ts`

The intentional difference is cleanup. Upstream `actions/checkout` defaults
`clean: true`, which runs `git clean -ffdx && git reset --hard HEAD` and may
recreate the directory. Verself workspaces are durable ZFS volumes, so this
action does not run `git clean -ffdx` by default. It reconciles tracked files to
the requested commit while preserving untracked build state.

The `bundle-cache-hit` output is scoped to the host-side Git pack bundle used
to update the working tree. Golden workspace selection is recorded by
sandbox-rental and vm-orchestrator traces, not by this action.

The action does not request `github.token` by default. If the host-side mirror
has to fetch from GitHub, sandbox-rental uses the repository's GitHub App
installation token.
