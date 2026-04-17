# policies/

Machine-readable source of truth for every customer-facing policy commitment
Forge Metal makes. One file per domain, each consumed by both the rendered
`/policy/*` pages in `src/viteplus-monorepo/apps/platform` and by Go enforcement
code in `src/platform/internal/policyspec`.

Files here are public by intent: they encode commitments we make and surfaces we
are accountable to. A PR that touches any file here should also update
`versions.yml` with a new entry and summary of what changed; merge triggers the
30-day notice flow to organization administrators.

| File | Shape | Consumers |
|---|---|---|
| `retention.yml` | Account lifecycle, per-data-class retention windows, export, legal hold, deletion method. | `/policy/data-retention` page; `policyspec` Go package; ClickHouse TTL validation in CI. |
| `subprocessors.yml` | Active subprocessor catalog. | `/policy/subprocessors` page; `/policy/dpa`; RSS feed. |
| `ropa.yml` | Record of Processing Activities (GDPR Art. 30): role (controller vs processor), purposes, data categories, lawful bases. | `/policy/privacy`; auditor export. |
| `contacts.yml` | Policy mailbox local-parts; resolved to the operator's deployment domain at render time. | Every `/policy/*` page's Contact section. |
| `versions.yml` | Append-only changelog. Each entry records effective date, affected policies, one-line summary. | `/policy/changelog`; RSS feed. |

Validation: the Go side uses `go-playground/validator` on unmarshaled structs;
the TypeScript side parses with Valibot schemas defined in
`src/viteplus-monorepo/apps/platform/src/lib/policy-catalog.ts`. Schema changes
must land on both sides in the same commit.
