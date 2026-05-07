# apps/company

Guardian Intelligence company site. Deployed to `guardianintelligence.org`
(root) as a Bazel node-app artifact supervised by Nomad.

## Layout

- `src/routes/` — file-based TanStack Start routes. Structural only.
- `src/content/` — every user-facing string. Forker-editable.
- `src/brand/` — voice spec (`voice.md` + `voice.ts`). `tokens.css` lives at `src/styles/app.css` until Phase 3 splits it out.
- `src/components/` — presentation only. No copy in JSX.
- `src/lib/telemetry/` — mirror of the platform app's telemetry surface. `emitSpan(name, attrs)` for one-shot browser spans. Every route emits `company.route_view` from `TelemetryProbe`.

## Voice

`src/brand/voice.md` is the source of truth for humans. `src/brand/voice.ts` exports `assertVoice(input)` for the dynamic OG-card generator. Never mention a tech name (Zitadel, Firecracker, ZFS, etc.) in `src/content/**` — translate to product language.

## Telemetry

Every route emits a `company.route_view` span with `route.path`, `route.host`, and navigation metadata. Web Vitals (LCP/CLS/INP) fire independently via `web_vital.*` spans. Dynamic OG renders emit `company.og.render` with `og.voice_pass` (Phase 5).

Run `aspect observe --what=service --service=company-web` to see the live span surface once the app is deployed.
