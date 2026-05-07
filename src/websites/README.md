# Vite+ Frontend Workspace

This workspace is the canonical home for the frontend applications: the
verself-web app at `apps/verself-web` (signed-in console, public docs, public
policy — all served from the verself.sh apex) and the company marketing site
at `apps/company` (guardianintelligence.org).

## Layout

- `apps/company`: root-domain company and marketing site.
- `apps/verself-web`: TanStack Start app served at `verself.sh` — landing,
  auth entry routes (`/login`, `/logout`), authenticated console
  (`_shell/_authenticated/*`), and public docs/policy under the `_workshop`
  pathless layout (`/docs`, `/policy`, plus the same-origin OTLP forwarder
  at `/api/otel/v1/traces`).
- `packages/ui`, `packages/auth-web`, `packages/brand`, `packages/web-env`,
  `packages/nitro-plugins`: shared workspace packages.

## Commands

```bash
vp install
vp check
vp test run
vp run -r typecheck
vp run -r build
vp run @verself/verself-web#dev
```

`vp check`, `vp test run`, `vp run -r typecheck`, and `vp run -r build` are the baseline gates for changes in this workspace.
