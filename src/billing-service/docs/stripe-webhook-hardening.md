# Stripe Webhook Hardening

This package treats Stripe's webhook guidance as the contract for the billing ingress.

Primary Stripe sources:

- Webhooks: <https://docs.stripe.com/webhooks>
- Integration security guide: <https://docs.stripe.com/security/guide>
- Domains and IP addresses: <https://docs.stripe.com/ips>
- Process undelivered webhook events: <https://docs.stripe.com/webhooks/process-undelivered-events>

## Stripe Requirements

Stripe's current webhook guidance requires these controls:

- Use HTTPS/TLS for live webhook endpoints. Stripe documents HTTPS webhook endpoints and recommends TLS 1.2+. Source: <https://docs.stripe.com/webhooks>, <https://docs.stripe.com/security/guide>
- Verify the `Stripe-Signature` header against the raw request body and the endpoint secret. Source: <https://docs.stripe.com/webhooks>
- Allowlist Stripe's published webhook source IPs. Stripe explicitly says to use both IP allowlisting and signature verification. Source: <https://docs.stripe.com/webhooks>, <https://docs.stripe.com/security/guide>, <https://docs.stripe.com/ips>
- Return `2xx` before complex logic and process events asynchronously. Stripe's example guidance says to return `200` before updating downstream systems. Source: <https://docs.stripe.com/webhooks>
- Handle duplicate deliveries and automatic retries. Stripe retries for up to 3 days and recommends recording processed events. Source: <https://docs.stripe.com/webhooks>, <https://docs.stripe.com/webhooks/process-undelivered-events>
- Subscribe only to the event types the integration actually needs. Source: <https://docs.stripe.com/webhooks>
- Rotate endpoint signing secrets periodically. Source: <https://docs.stripe.com/webhooks>
- Exempt the webhook route from CSRF middleware if the framework applies it. Source: <https://docs.stripe.com/webhooks>

## Repo Mapping

Implemented in this repo:

- HTTPS-only public webhook ingress at `https://billing.<domain>/webhooks/stripe` through Caddy: [`../../platform/ansible/roles/caddy/templates/Caddyfile.j2`](../../platform/ansible/roles/caddy/templates/Caddyfile.j2)
- Stripe source IP allowlist at the edge, pinned to Stripe's published webhook IP list: [`../../platform/ansible/roles/caddy/defaults/main.yml`](../../platform/ansible/roles/caddy/defaults/main.yml), [`../../platform/ansible/roles/caddy/templates/Caddyfile.j2`](../../platform/ansible/roles/caddy/templates/Caddyfile.j2)
- Raw-body signature verification with Stripe's official Go library: [`../cmd/billing-service/main.go`](../cmd/billing-service/main.go), [`../internal/billing/stripe.go`](../internal/billing/stripe.go)
- Public method restriction and edge-side body cap: [`../../platform/ansible/roles/caddy/templates/Caddyfile.j2`](../../platform/ansible/roles/caddy/templates/Caddyfile.j2)
- Blocking WAF policy on the billing hostname: [`../../platform/ansible/roles/caddy/templates/Caddyfile.j2`](../../platform/ansible/roles/caddy/templates/Caddyfile.j2)
- Webhook handling is intentionally inline for provider-state mutation and grant issuance. Grant side effects are idempotent because `source_reference_id` is unique per `(org_id, source, scope_type, scope_product_id, scope_bucket_id, source_reference_id)` in [`../postgresql-migrations/001_billing_schema.up.sql`](../postgresql-migrations/001_billing_schema.up.sql). Stripe is one source namespace, not the global grant identity model.
- Subscription webhook behavior is testable without constructing signed Stripe payloads through the provider-event boundary in [`../internal/billing/stripe.go`](../internal/billing/stripe.go). Fault-injection tests should drive `ApplySubscriptionProviderEvent` with delayed, duplicated, failed, and terminal subscription events, then verify PostgreSQL state and `forge_metal.billing_events` projection.
- Billing-service keeps the Stripe webhook secret out of process args and env by loading it through systemd credentials: [`../../platform/ansible/roles/billing_service/tasks/credentials.yml`](../../platform/ansible/roles/billing_service/tasks/credentials.yml), [`../cmd/billing-service/main.go`](../cmd/billing-service/main.go)

Not automated in this repo yet:

- Stripe webhook endpoint provisioning in Workbench or via the Stripe API
- Endpoint secret rotation in Stripe plus corresponding secret rotation in `secrets.sops.yml`
- Stripe-side enforcement that the endpoint is subscribed only to the required event types

Those are still operator responsibilities because the repo currently consumes a user-provided `stripe_webhook_secret`: [`../../platform/ansible/group_vars/all/secrets.example.yml`](../../platform/ansible/group_vars/all/secrets.example.yml)

## Stripe Event Set

Stripe says to subscribe only to the event types the integration requires. The billing code currently handles exactly these event types:

- `checkout.session.completed`
- `payment_intent.succeeded`
- `invoice.paid`
- `invoice.payment_failed`
- `customer.subscription.updated`
- `customer.subscription.deleted`

Source in code: [`../internal/billing/stripe.go`](../internal/billing/stripe.go)

The Stripe webhook endpoint should be configured to send only that set. Do not subscribe the billing endpoint to all events.

## Published Webhook IPs

Stripe's current webhook source IP list is documented here: <https://docs.stripe.com/ips#webhook-notifications>

The repo pins that published list in [`../../platform/ansible/roles/caddy/defaults/main.yml`](../../platform/ansible/roles/caddy/defaults/main.yml). Stripe also publishes machine-readable copies and says IP changes are announced with seven days' notice on the API announce mailing list:

- <https://stripe.com/files/ips/ips_webhooks.txt>
- <https://stripe.com/files/ips/ips_webhooks.json>
- <https://groups.google.com/a/lists.stripe.com/g/api-announce>

## Operator Checklist

For a production billing rollout, the operator should verify all of the following:

- Stripe Workbench has a webhook endpoint at `https://billing.<domain>/webhooks/stripe`
- The endpoint is subscribed only to the event types listed above
- The endpoint signing secret in Stripe matches `stripe_webhook_secret`
- The billing hostname remains DNS-only in Cloudflare so origin IP allowlisting still sees Stripe's real source IPs
- Billing deploys continue to return `403` for non-Stripe public webhook probes and `200` for valid Stripe-signed loopback probes
