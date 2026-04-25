# Vite+ Frontend Workspace

This workspace is the canonical home for the frontend applications, including
the product console at `apps/console`.

## Layout

- `apps/company`: root-domain company and marketing site.
- `apps/platform`: public docs/legal app until those surfaces are folded into
  the root site or console.
- `apps/console`: authenticated TanStack Start product console on
  `console.<domain>`.
- `packages/ui`: shared UI primitives plus a small test target to prove workspace tooling

## Commands

```bash
vp install
vp check
vp test run
vp run -r typecheck
vp run -r build
vp run @forge-metal/console#dev
```

`vp check`, `vp test run`, `vp run -r typecheck`, and `vp run -r build` are the baseline gates for changes in this workspace.
