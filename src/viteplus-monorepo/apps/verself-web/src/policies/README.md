# policies/

Machine-readable source of truth for every customer-facing policy commitment
Verself makes. One file per domain, consumed directly by the rendered
`/policy/*` pages.

Files here are public by intent: they encode commitments we make and surfaces
we are accountable to. A PR that touches any file here should also update
`versions.yml` with a new entry and summary of what changed.

| File | Shape |
|---|---|
| `retention.yml` | Account lifecycle, per-data-class retention windows, export, legal hold, deletion method. |
| `subprocessors.yml` | Active subprocessor catalog. |
| `ropa.yml` | Record of Processing Activities (GDPR Art. 30): role (controller vs processor), purposes, data categories, lawful bases. |
| `contacts.yml` | Policy mailbox local-parts; resolved to the operator's deployment domain at render time. |
| `versions.yml` | Append-only changelog. Each entry records effective date, affected policies, one-line summary. |

Validation runs on the frontend parse: Valibot schemas in
`../lib/policy-catalog.ts` reject malformed edits at build time.
