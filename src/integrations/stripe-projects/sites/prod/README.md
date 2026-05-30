# Stripe Projects: prod

Run Stripe Projects through the Aspect wrapper from the repository root:

```text
aspect integrations stripe-projects --site=prod --action=status
aspect integrations stripe-projects --site=prod --action=init --confirm
aspect integrations stripe-projects --site=prod --action=env-pull --confirm
```

This directory is the Stripe Projects worktree for the prod provider project.
Commit `.projects/state.json` and `.projects/state.local.json` after
initialization. Do not commit `.env`, `.projects/vault/`, or
`.projects/cache/`.
