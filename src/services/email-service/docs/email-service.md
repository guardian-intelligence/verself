# Email Service

`email-service` owns company email addresses, inbound mail projection, forwarding policy, and outbound provider delivery. Stalwart remains the inbound mail store and JMAP source. Resend is the initial outbound sender; Cloudflare Email Sending is modeled as a pluggable sender behind the same internal service contract.

Primary references:

- Resend `POST /emails`, response `id`, `from`, `to`, `subject`, `html`, `text`, and `Idempotency-Key`: <https://resend.com/docs/api-reference/emails>
- Resend idempotency retention and conflict behavior: <https://resend.com/docs/dashboard/emails/idempotency-keys>
- Resend `POST /api-keys` child-key creation with `sending_access`: <https://resend.com/docs/api-reference/api-keys/create-api-key>
- Resend `DELETE /api-keys/:api_key_id` child-key revocation: <https://resend.com/docs/api-reference/api-keys/delete-api-key>
- Cloudflare Email Sending REST endpoint: <https://developers.cloudflare.com/email-service/api/send-emails/rest-api/>
- Cloudflare Email Sending API resource: <https://developers.cloudflare.com/api/resources/email_sending/methods/send/>

## Address Model

Each provisioned email address has exactly one Verself email identity. Multiple addresses per identity are intentionally unsupported. A human identity can still send as a role address through an explicit `send_as_grants` row.

Core tables:

- `email_identities`: durable identity row for a human, role, operator, or system address.
- `email_addresses`: unique normalized address, owning org, local part, domain, address type, inbound/outbound enablement, and status.
- `email_identity_memberships`: subjects allowed to read/manage the identity mailbox.
- `send_as_grants`: subjects allowed to submit outbound messages from an address.
- `forwarding_destinations`: verified external forwarding targets.
- `forwarding_rules`: source-address to destination routing with optional local retention.
- `provider_bindings`: active sender provider for a domain or address.
- `outbound_messages` and `email_delivery_attempts`: idempotent outbound message ledger and provider attempts.
- `audience_contacts`, `suppression_entries`, `campaigns`, and `campaign_recipients`: newsletter and waitlist storage sized for high-fanout sends.

## Company Addresses

Company-owned addresses are regular email identities under the owning Verself
org:

- `anveio@verself.sh`
- `hello@verself.sh`
- `feedback@verself.sh`
- `sales@verself.sh`
- `support@verself.sh`
- `security@verself.sh`
- `abuse@verself.sh`
- `privacy@verself.sh`
- `legal@verself.sh`
- `billing@verself.sh`
- `careers@verself.sh`
- `press@verself.sh`
- `postmaster@verself.sh`
- `hostmaster@verself.sh`
- `webmaster@verself.sh`
- `noreply@verself.sh`
- `updates@verself.sh`
- `agents@verself.sh`

The company seed creates forwarding policy from every company address to `integrations.anveio@gmail.com` with local-copy retention enabled. The owning org receives `owner` membership and `send_as` grants for each address.

The email bootstrap flow also creates `noreply@notify.verself.sh` as a system sender identity because `notify.verself.sh` is the currently verified Resend domain. This address is outbound-only and is not part of the company address inventory.

## Sending

Internal callers send through `POST /internal/v1/email/send` over SPIFFE mTLS. The caller supplies `org_id`, `from_address`, `to_address`, subject/body content, workflow metadata, and an idempotency key. `email-service` creates or reuses an `outbound_messages` row before provider delivery and records each provider attempt in `email_delivery_attempts`.

Public customer send APIs are intentionally absent. Customer-facing authenticated routes are limited to mailbox reads and mutations over `/api/v1/email/*`.

Notifications uses an internal email-service client and no provider credentials. Provider-specific request shape, idempotency headers, response IDs, retries, and rate-limit interpretation stay inside `email-service`.

## Resend Key Lifecycle

`email-service` owns the Resend implementation boundary. Generated bootstrap
vars contain no Resend runtime sending key. The `email-service-resend-keys` Nomad batch job
authenticates to OpenBao with workload identity, reads
`email-service.resend.full_access_api_key`, creates a `sending_access` Resend API
key, writes `email-service.resend.api_key`, writes the same SMTP-compatible value
to `zitadel.smtp.password`, and records non-secret key metadata in
`email-service.resend.key_metadata`. Rotation writes the replacement child key
to OpenBao before revoking the prior Resend key and records pending revocation in
metadata so retries complete the delete instead of creating another child key.

The full-access Resend key is a prod/global external provider authority copied
into site OpenBao under the email-service policy boundary during bootstrap or
rotation. Runtime services receive only the generated child credentials through
Nomad/OpenBao templates.

## Agent-To-Operator Email

Agents working in the repo email the operator through the same internal send path. The sender is `agents@guardianintelligence.org`, owned by the dogfood org; on boot `email-service` provisions that identity (the otherwise-unused `ProvisionAddress`) and a `resend` / `guardianintelligence.org` provider binding from `EMAIL_SERVICE_AGENT_SENDER_*`, so `EnsureOutboundAddress` accepts it. `guardianintelligence.org` is a verified Resend sending domain; `anveio@guardianintelligence.org` is delivered to the operator inbox by Cloudflare Email Routing reconciled through `cloudflare-integration-service`, which forwards to the verified Gmail destination.

The agent-facing tool is `src/tools/operator/cmd/agent-mail`. It must run as the `agent_mail` user on a node so the SPIRE workload API issues `spiffe://<td>/svc/agent-mail`, which `email-service`'s internal peer allowlist trusts. The HTML body is a React Email template (`@verself/agent-email`) rendered to a Go `text/template` at build time; the plaintext alternative is assembled in the tool. Operators invoke it as `aspect mail send --subject ... --body ...`, which uploads the binary via the operator SSH plane and runs it as `agent_mail`.

Provision the domains with `src/integrations/email/provision-email-domains.yml` (Resend verification for every `resend_sending_domains` entry plus Cloudflare Email Routing reconciliation). The company apex carries one merged SPF covering both Resend and Cloudflare; the Gmail destination must complete Cloudflare's verification click once. The full runbook for DNS layout, Cloudflare authority, the `agent_mail` identity, and send verification is in `src/integrations/email/README.md`.

## Receiving And Forwarding

Inbound mail is accepted by Stalwart, projected through JMAP sync workers, and stored in `mailboxes`, `emails`, `email_mailboxes`, `email_bodies`, and `threads`. Sync workers skip provisioned identities that lack a protocol credential; company role addresses still exist as address and forwarding policy rows.

Forwarding is represented in database policy first. SMTP/JMAP execution can use Stalwart mailing lists, Sieve, or an email-service delivery worker without changing the address, destination, or grant model.

## Observability

ClickHouse view `default.email_events` normalizes:

- outbound acceptance, provider acceptance, provider failure, and `send_as` denial from `email-service` logs;
- Stalwart delivery attempts from traces;
- JMAP sync bootstrap, eventsource, and change-application logs.

Deployment evidence should include `email-service` service logs, rows in `email_addresses`, `forwarding_rules`, `outbound_messages`, and `email_delivery_attempts`, and recent `default.email_events` rows for exercised flows.
