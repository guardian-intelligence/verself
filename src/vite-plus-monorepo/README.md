# Vite+ Rent-a-Sandbox Baseline

This workspace is the staging area for moving `src/rent-a-sandbox/` onto Vite+ without doing the application port in the same change.

## Layout

- `apps/rent-a-sandbox`: minimal TanStack Start app wired for Nitro, React Query, and Tailwind 4
- `packages/ui`: shared UI primitives plus a small test target to prove workspace tooling

## Commands

```bash
vp install
vp check
vp test run
vp run -r typecheck
vp run -r build
vp run @forge-metal/rent-a-sandbox#dev
```

`vp check`, `vp test run`, `vp run -r typecheck`, and `vp run -r build` are the baseline gates that need to stay green before the existing frontend is moved over.
