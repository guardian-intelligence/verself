# Stripe Projects: gamma

Run Stripe Projects through the Aspect wrapper from the repository root:

```text
aspect integrations stripe-projects --site=gamma --action=status
aspect integrations stripe-projects --site=gamma --action=init --confirm
aspect integrations stripe-projects --site=gamma --action=env-pull --confirm
```

This directory is the Stripe Projects worktree for the gamma provider project.
Gamma uses separate provider resources and credentials from prod unless the
catalog marks a bootstrap exception. Do not commit `.env`, `.projects/vault/`,
or `.projects/cache/`.
