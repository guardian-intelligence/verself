# Vercel Docs Compatibility Inventory

Source crawl: `/usr/local/bin/agent-browser` against `https://vercel.com/docs` on May 6, 2026. The crawl used the public Vercel docs sitemap for complete URL coverage and the expanded docs sidebar for navigation order. The full machine-readable inventory is `docs/product/vercel-docs-compatibility-inventory.json`.

The inventory stores URLs, labels, path structure, last-modified timestamps, and a Verself coverage disposition. It intentionally does not copy Vercel page bodies, examples, diagrams, or prose.

## Coverage Status

| Status | Pages |
| --- | ---: |
| `coming_soon` | 809 |
| `documented` | 101 |
| `out_of_scope` | 283 |
| `product_review` | 352 |

`documented` means a current Verself public docs route covers the same product-domain concept. `coming_soon` is the recommended public placeholder set for the Verself compatibility surface. `product_review` needs a product decision before making a public commitment. `out_of_scope` is Vercel application hosting, frontend delivery, or Vercel-only platform material that conflicts with the current Verself product contract.

The status field is a documentation planning disposition. It is not a claim that a Verself endpoint already implements the matching Vercel wire protocol.

## Largest Vercel Docs Sections

| Section | Pages |
| --- | ---: |
| `rest-api` | 641 |
| `ai-gateway` | 91 |
| `integrations` | 84 |
| `errors` | 79 |
| `conformance` | 78 |
| `cli` | 58 |
| `functions` | 30 |
| `flags` | 27 |
| `frameworks` | 27 |
| `pricing` | 27 |
| `domains` | 23 |
| `agent-resources` | 22 |
| `vercel-firewall` | 16 |
| `deployments` | 15 |
| `vercel-sandbox` | 14 |
| `edge-config` | 13 |
| `deployment-protection` | 12 |
| `analytics` | 10 |
| `speed-insights` | 10 |
| `vercel-blob` | 10 |
| `environment-variables` | 9 |
| `microfrontends` | 9 |
| `project-configuration` | 9 |
| `vercel-toolbar` | 9 |
| `queues` | 8 |
| `sign-in-with-vercel` | 8 |
| `drains` | 7 |
| `plans` | 7 |
| `rbac` | 7 |
| `routing` | 7 |

## Coming Soon Sections

- `accounts`
- `activity-log`
- `agent`
- `audit-log`
- `cli`
- `errors`
- `functions`
- `limits`
- `logs`
- `notifications`
- `observability`
- `oidc`
- `plans`
- `pricing`
- `rbac`
- `regions`
- `rest-api`
- `saml`
- `security`
- `spend-management`
- `support-center`
- `two-factor-authentication`
- `two-factor-enforcement`
- `webhooks`
- `workflows`

## Product Review Sections

- `agent-resources`
- `ai-gateway`
- `ai-sdk`
- `checks`
- `conformance`
- `drains`
- `edge-config`
- `flags`
- `integrations`
- `marketplace-storage`
- `mcp`
- `query`
- `queues`
- `services`
- `sign-in-with-vercel`
- `tracing`

## Layout Notes

The Vercel docs layout has a fixed product header, a scrollable left docs sidebar, a central article column, and a right contextual rail. The right rail contains framework/context selectors, page anchors, and page actions such as copy and feedback. Mobile collapses the docs sidebar behind a top row while retaining search and a page action button.

For Verself, copy the information architecture pattern rather than Vercel's brand treatment: fixed public chrome, left navigation, central article, right page tools, mobile drawer, and no Ask AI entry until there is a real retrieval-backed assistant.
