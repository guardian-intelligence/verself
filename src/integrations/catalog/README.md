# Integration Catalog

The integration catalog is checked-in metadata for provider accounts,
credentials, public variables, bootstrap exceptions, storage targets, and
verification evidence. It contains no secret values.

Each site has one Stripe Projects worktree under
`src/integrations/stripe-projects/sites/<site>`. Stripe Projects owns
`.projects/state.json`, `.projects/state.local.json`, `.projects/vault/`, and
`.env` in that directory. Commit only the state files that Stripe documents as
shareable. The repo ignore rules exclude local credentials and caches.

Use the Aspect wrapper from the repo root:

```text
aspect integrations stripe-projects --site=gamma --action=status
aspect integrations stripe-projects --site=gamma --action=init --confirm
aspect integrations stripe-projects --site=gamma --action=search --query=webhook
aspect integrations stripe-projects --site=gamma --action=env-pull --confirm
```

The wrapper builds the pinned Stripe CLI and Projects plugin through Bazel,
keeps plugin state under ignored `.verself` paths, and passes the host Stripe
config with `--config` when one exists.
