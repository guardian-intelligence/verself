# Billing: Stripe Sandbox Migration Roadmap

## Status quo

Tests use `stripe listen --forward-to localhost:4242/webhooks/stripe` to forward live test-mode webhooks to a locally running billing server over SSH tunnels. This requires:

- Stripe CLI installed and authenticated
- SSH tunnels to PG, TB, CH on the bare metal host
- Manual `stripe trigger` invocations
- The webhook signing secret to match the CLI's ephemeral secret (we hardcode it in SOPS)
- A human watching the terminal

The billing server's webhook handler (`internal/billing/webhook.go`) processes 6 Stripe event types:

| Event type | Handler action |
|---|---|
| `checkout.session.completed` | Correlate `stripe_customer_id` to org |
| `payment_intent.succeeded` | Enqueue `stripe_purchase_deposit` task |
| `invoice.paid` | Enqueue `stripe_subscription_credit_deposit` or `stripe_licensed_charge` task |
| `invoice.payment_failed` | Set subscription status to `past_due` |
| `charge.dispute.created` | Enqueue `stripe_dispute_debit` task |
| `customer.subscription.deleted` | Set subscription status to `cancelled` |

## Target state

A dedicated Stripe sandbox with:

1. A restricted API key (RAK) scoped to the minimum permissions the billing server needs
2. A persistent webhook endpoint configured as an event destination, receiving only the 6 event types above
3. No Stripe CLI in the test loop — Stripe delivers events directly to the billing server's public endpoint via Caddy

## Stripe Sandboxes: key facts

- Sandboxes are isolated environments, separate from test mode. Each has independent API keys, event destinations, and data.
- Maximum 16 event destinations per sandbox.
- Event destinations support event type filtering — configure the endpoint to receive only the 6 types we handle.
- Two event formats: **snapshot events** (complete `data.object`, what we use today via v1 API) and **thin events** (lightweight, unversioned, from v2 API). We use snapshot events exclusively.
- Signature verification uses the same HMAC-SHA256 scheme as test mode. The signing secret is per-endpoint and stable (not ephemeral like `stripe listen`).
- Sandboxes cannot establish Connect platform connections. Not relevant — we don't use Connect.

## Restricted API key permissions

The billing server calls these Stripe APIs:

| Operation | Stripe API | Required permission |
|---|---|---|
| `CreateCheckoutSession` | `POST /v1/checkout/sessions` | **Checkout Sessions: Write** |
| `CreateSubscription` (checkout mode) | `POST /v1/checkout/sessions` | **Checkout Sessions: Write** |
| Price lookup for subscription | `GET /v1/prices` | **Prices: Read** (used internally by checkout session creation) |
| Webhook signature verification | N/A (local crypto) | None |

Create a RAK via Dashboard > API keys > Create restricted key with:

| Resource | Permission |
|---|---|
| Checkout Sessions | Write |
| Prices | Read |
| Customers | Write (checkout auto-creates customers) |
| All others | None |

This is the minimum viable scope. If we later add direct subscription management (cancel, update), add **Subscriptions: Write**.

## Event destination configuration

Create via Dashboard > Webhooks > Add destination, or via API:

```
POST https://api.stripe.com/v2/core/event_destinations
{
  "type": "webhook_endpoint",
  "webhook_endpoint": {
    "url": "https://<domain>/webhooks/stripe"
  },
  "enabled_events": [
    "checkout.session.completed",
    "payment_intent.succeeded",
    "invoice.paid",
    "invoice.payment_failed",
    "charge.dispute.created",
    "customer.subscription.deleted"
  ]
}
```

The signing secret returned from this creation replaces the current `STRIPE_WEBHOOK_SECRET` in the credstore.

## Migration steps

### Phase 1: Create sandbox and deploy (manual, one-time)

1. Create a Stripe sandbox from Dashboard account picker
2. In the sandbox, create a RAK with the permissions above
3. Create a webhook event destination pointing to `https://<domain>/webhooks/stripe` with the 6 event types
4. Copy the signing secret
5. Update SOPS secrets:
   - `stripe_secret_key` → sandbox RAK
   - `stripe_webhook_secret` → sandbox endpoint signing secret
6. Deploy billing server (`ansible-playbook playbooks/dev-single-node.yml --tags billing`)
7. Verify: create a checkout session via API, complete with test card `4000003720000278`, watch billing events appear in PG

### Phase 2: Automated integration test flow

Replace the current `stripe listen` test flow with direct API calls:

```
# No stripe CLI needed. The sandbox endpoint is live.
curl -X POST https://<domain>/internal/billing/v1/checkout \
  -d '{"org_id":..., "product_id":..., "amount_cents":1000, ...}'
# → Returns Stripe checkout URL

# Complete checkout programmatically via Stripe API:
# 1. Create PaymentMethod with test card
# 2. Confirm the CheckoutSession's PaymentIntent
# → Stripe delivers checkout.session.completed + payment_intent.succeeded webhooks
# → Worker processes stripe_purchase_deposit task
# → TigerBeetle balance increases
# → Metering row written to ClickHouse
```

The test verifies end-to-end by polling PG for the completed task and checking TB balance.

### Phase 3: Subscription lifecycle tests

Same pattern for subscriptions:

1. Create subscription checkout via API
2. Complete via test card
3. Stripe delivers `invoice.paid` webhook
4. Worker processes credit deposit
5. Test verifies grant exists in PG and balance in TB
6. Trigger `invoice.payment_failed` via Stripe test helper (test clock or failed card)
7. Verify subscription status transitions to `past_due`

### Phase 4: Dispute flow tests

1. Create a successful payment (Phase 2 flow)
2. Create a dispute via Stripe API (`POST /v1/disputes` with test dispute data)
3. Stripe delivers `charge.dispute.created` webhook
4. Worker processes `stripe_dispute_debit` task
5. Verify org balance debited, suspension logic if balance insufficient

### Phase 5: Remove Stripe CLI dependency

- Delete `stripe listen` instructions from AGENTS.md
- Remove `stripe` from dev-tools.json (unless needed for other purposes)
- Update CI fixtures to use sandbox directly if applicable

## Open questions

1. **Test data cleanup**: Sandboxes accumulate data across test runs. Stripe has no "reset sandbox" API. Options: (a) create ephemeral sandboxes per test run (overkill), (b) scope tests by unique org IDs (current approach, works fine), (c) periodically delete sandbox and recreate.

2. **CI integration**: If CI needs to run Stripe integration tests, the sandbox RAK and webhook secret must be available as CI secrets. The webhook endpoint URL must be reachable from Stripe — either use a public domain or a tunnel service (ngrok, Cloudflare Tunnel).

3. **Rate limits**: Stripe test mode has generous rate limits but they exist. Parallel test runs hitting the same sandbox could hit them. Unlikely for our scale.
