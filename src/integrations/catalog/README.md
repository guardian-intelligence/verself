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
aspect integrations stripe-projects --site=gamma --action=search --query=resend
aspect integrations stripe-projects --site=gamma --action=env-pull --confirm
```

The wrapper does not install the Stripe Projects plugin. Install the pinned
plugin version named by `.aspect/tasks/integrations.axl` before using the
wrapper.
