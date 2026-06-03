# Agent-to-operator email

Agents working in the repository send the operator notification email through
`email-service`'s internal send path. The sender is
`agents@guardianintelligence.org`, owned by the dogfood org; the operator
mailbox `anveio@guardianintelligence.org` is delivered to a personal inbox by
Cloudflare Email Routing.

## Path

```
agent  ──aspect mail send──▶  agent-mail (runs as the agent_mail workload)
                                   │  SPIFFE mTLS, peer = svc/agent-mail
                                   ▼
                             email-service  POST /internal/v1/email/send
                                   │  Resend, From: agents@guardianintelligence.org
                                   ▼
                             recipient (anveio@guardianintelligence.org)
                                   │  Cloudflare Email Routing
                                   ▼
                             operator inbox (Gmail)
```

`agent-mail` (`src/tools/operator/cmd/agent-mail`) renders a React Email
template (`@verself/agent-email`) to a Go `text/template` at build time, fills
subject and body, and calls `email-service`. Delivery, idempotency, and the
outbound ledger stay inside `email-service`. The internal endpoint is mutual-TLS
gated and trusts the `svc/agent-mail` SPIFFE ID, so the binary must run as the
`agent_mail` user on a node for the workload API to issue that SVID — the same
upload-and-run-as-user mechanism the discovery canary uses.

## Domains

`guardianintelligence.org` is both a Resend sending domain and a Cloudflare
Email Routing zone. Resend keeps its records on subdomains — DKIM on
`resend._domainkey`, the Return-Path MX and SPF on `send.guardianintelligence.org`
— so the apex carries only the Cloudflare Email Routing MX and a single SPF that
includes both providers. DMARC at the apex is `p=reject`; alignment holds
through Resend's DKIM signature.

`resend_sending_domains` in the site vars describes the sending domains.
Provider-side domain verification is controller/operator setup. Runtime Resend
sending credentials are created by `email-service` after site OpenBao is
available.

## Provisioning

```
ANSIBLE_ROLES_PATH=src/integrations/resend/domain-bootstrap:src/integrations/cloudflare/email-routing \
ANSIBLE_COLLECTIONS_PATH="$HOME/.ansible/collections" \
  ansible-playbook -i src/host/sites/prod/inventory.ini \
    -e verself_site=prod src/integrations/email/provision-email-domains.yml
```

The playbook registers and verifies every Resend sending domain, then enables
Cloudflare Email Routing for the operator mailboxes. It runs on the controller
against the Resend and Cloudflare APIs and is idempotent. The controller process
must supply `resend_full_access_api_key` and Cloudflare authority from OpenBao.
The playbook does not create or deliver the runtime Resend sending key.

### Cloudflare Credentials

Cloudflare DNS mutations run from the prod Cloudflare control-plane authority.
The controller may pass one account-admin token as
`cloudflare_account_admin_api_token` when provisioning Resend verification DNS.
That token is never written to generated artifacts, Nomad jobs, Ansible host
vars, or runtime service environments.

`cloudflare_company_api_token` is an explicit Email-Routing-scoped credential
for the Email Routing role. It needs, on `guardianintelligence.org`: Zone DNS
edit, Zone Email Routing Rules edit, and account-level Email Routing Addresses
edit.

A token missing Zone Email Routing Rules edit returns 403 on rule list/create.
Email Routing destination addresses are an account-level resource: a strictly
zone-scoped token returns 403 there, which the role accepts — add and verify the
destination in the dashboard (Account → Email Routing → Destination addresses)
instead.

### Destination verification

Cloudflare emails a one-time verification link to each forwarding destination
(`im.shovonhasan@gmail.com`); forwarding does not work until that link is
clicked, regardless of token scope.

## The agent_mail identity

`spire_identity` at `//src/tools/operator/cmd/agent-mail:spiffe_identity`
declares the `svc/agent-mail` workload (unix user `agent_mail`). Host
bootstrap must converge that SPIRE registration, which creates the user and the
SPIRE entry. `email-service`'s internal peer allowlist trusts `agent-mail`
alongside `billing-service` and `notifications-service`. On boot `email-service`
provisions the `agents@guardianintelligence.org` identity and a Resend provider
binding from `EMAIL_SERVICE_AGENT_SENDER_*`.

## Sending

```
aspect mail send --subject "build green" --body "all checks passed on main"
```

Defaults to `agents@guardianintelligence.org` → `anveio@guardianintelligence.org`
under the dogfood org; `--to`, `--from`, and `--org-id` override. The operator
subcommand uploads the `agent-mail` binary and runs it as the `agent_mail` user.

## Verifying a send

- Resend returns a `provider_message_id`; `email-service` writes
  `outbound_messages` and `email_delivery_attempts` rows.
- ClickHouse `default.email_events` normalizes acceptance and provider outcome.
- Received headers show DKIM, SPF, and DMARC pass; the message arrives in the
  operator inbox through the Cloudflare route.
