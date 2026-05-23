# Company Email Address Policy

Company email addresses are durable product resources. They encode a stable function, a named human identity, or a system sender. They do not encode executive titles, temporary reporting structure, or fundraising posture.

## Standards baseline

- `postmaster@<domain>` is required for any domain that accepts SMTP delivery. [RFC 5321](https://www.rfc-editor.org/rfc/rfc5321.html) requires SMTP systems that support mail delivery or relay to support the reserved `postmaster` local-part case-insensitively.
- [RFC 2142](https://www.rfc-editor.org/rfc/rfc2142.html) defines conventional role mailbox names for common functions, including `abuse`, `security`, `hostmaster`, `webmaster`, `info`, `sales`, and `support`.
- [RFC 9116](https://www.rfc-editor.org/rfc/rfc9116.html) `security.txt` requires a `Contact` field for vulnerability reporting. `security@<domain>` is the clean mail contact for that file when email is exposed.

## Address classes

| Class | Owner | Used for | Product behavior |
| --- | --- | --- | --- |
| Personal | One named human identity | Human correspondence and account ownership, for example `anveio@verself.sh` | Provision one Verself identity, one Zitadel user, one address, and one mailbox. Disable the identity when the person leaves or the address is rotated. |
| Role | One managed role identity | External or internal traffic for a durable function, for example `security@verself.sh` | Provision one non-human Verself identity and route to its mailbox, ticket queue, integration, or verified forwarding destination. Grant humans explicit membership and send-as permission. |
| System sender | One managed system identity | Transactional messages, product notifications, waitlist messages, broadcasts, and receipts | Send through the email-service provider abstraction. Store delivery attempts, webhooks, suppression state, and provider message IDs. |
| Operator mailbox | One managed operator identity | Required and conventional mail operations such as `postmaster@verself.sh` | Restrict membership to operators. Monitor separately from ordinary support traffic. |

## Excluded aliases

Do not provision these addresses:

| Address | Reason |
| --- | --- |
| `ceo@<domain>` | Use a named personal address for the human and role addresses for company functions. |
| `founder@<domain>` | Title aliases have unclear routing, unclear reply ownership, and weak long-term stability. |
| `founders@<domain>` | Use `hello@`, `press@`, `sales@`, or a named personal address depending on the context. |

## Primary addresses

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `hello@<domain>` | General inbound that does not have a more specific function. This is the safe public contact for early-stage site visitors, partners, and people with unclear intent. | Role mailbox; temporary verified forward to the platform owner is acceptable while volume is low. Keep a local copy. | Platform owner and designated company operators. |
| `sales@<domain>` | Prospective customers, procurement, pricing questions, pilots, partnership sales, and VC introductions that are explicitly commercial. | Sales role mailbox or CRM ingestion. | Platform owner and sales operators. |
| `support@<domain>` | Product users asking for help, broken workflows, account issues, operational incidents visible to customers, and support tickets. | Support queue or shared support mailbox. | Support operators. |
| `security@<domain>` | Vulnerability reports, coordinated disclosure, security questions, security.txt contact, and reports that require restricted handling. | Restricted security mailbox with local retention. External forwarding requires verification and local copy retention. | Security responders only. |
| `abuse@<domain>` | Spam reports, harmful use of the platform, policy violations, deliverability complaints from network operators, and user-safety escalations. | Restricted trust-and-safety or operator mailbox. | Abuse responders only. |
| `privacy@<domain>` | Data subject requests, privacy-policy contact, deletion/export requests, regulator privacy contact, and questions about personal data handling. | Restricted privacy/legal mailbox. | Privacy and legal operators only. |
| `legal@<domain>` | Legal notices, contract redlines, subpoenas, registered-agent routing, terms questions, and vendor legal mail. | Restricted legal mailbox. | Legal operators and approved platform-owner identity. |
| `billing@<domain>` | Invoices, receipts, payment failures, procurement paperwork, tax forms, purchase orders, and customer billing questions. | Billing queue or finance mailbox. | Billing operators. |
| `careers@<domain>` | Candidate inbound, recruiter messages, referrals, interview logistics, and hiring process questions. | Hiring mailbox or applicant-tracking integration. | Hiring operators. |
| `press@<domain>` | Journalists, podcasts, conference organizers, analyst requests, and public-relations inbound. | Platform-owner or communications mailbox. | Platform owner and communications operators. |
| `postmaster@<domain>` | SMTP delivery problems, mail server reports, blocklist operators, and standards-required mail operations. | Restricted mail-ops mailbox; monitored even if the domain has low traffic. | Mail operators only. |
| `hostmaster@<domain>` | DNS, registrar, zone delegation, and domain-operator contact. | Restricted infrastructure mailbox. | Infrastructure operators only. |
| `webmaster@<domain>` | Website availability problems, broken public pages, crawler issues, and legacy website-operator contact. | Website operations queue or website role mailbox. | Website operators. |
| `noreply@<domain>` | Transactional mail where replies cannot be handled in the same thread, for example auth codes, machine-generated receipts, and one-way system events. | Reject at SMTP with a clear support pointer, or accept into a low-priority monitored mailbox. Never silently discard while the address is public. | Email-service automation only. Humans should not reply as `noreply`. |

## Compatibility addresses

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `info@<domain>` | Vendor forms, standards-oriented senders, or legacy contacts require an `info` mailbox. Public site copy should prefer `hello@`. | Separate compatibility identity routed to `hello@`. | Same grants as `hello@`; avoid using as the visible sender. |
| `marketing@<domain>` | A third-party service or partner specifically asks for a marketing contact. Broadcasts should use `updates@`. | Separate compatibility identity routed to `press@` or `sales@` depending on source. | No default human send-as grant. |

## High-volume audience mail

Use a dedicated role sender for waitlist and newsletter traffic:

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `updates@<domain>` | Product updates, newsletter sends, waitlist lifecycle messages, launch announcements, and broadcast campaigns. | Replies route to `hello@` until a dedicated community or marketing queue exists. Delivery events route to provider webhooks. | Email-service automation. Humans may reply only through an explicit `SendAsGrant`. |

`updates@` should be the visible `From` address for audience mail. `noreply@` should be reserved for product events where a reply would be misleading. Audience mail needs unsubscribe handling, suppression-list checks, bounce and complaint webhooks, idempotent send jobs, and per-campaign evidence rows.

## Provisioning rules

- Every provisioned email address creates exactly one org-scoped Verself email identity. Personal addresses create human identities. Role, system, compatibility, and operator addresses create managed non-human identities.
- A Verself email identity has exactly one primary `EmailAddress`. Do not attach multiple provisioned addresses to one identity. If another externally visible address is needed, provision another email identity and route or delegate it explicitly.
- Every inbound-capable email identity must have one or more `AddressRoute` records and a local mailbox, integration target, verified forwarding destination, or explicit SMTP rejection policy.
- Human access to a role, compatibility, or operator mailbox is represented by `MailboxMembership`. Permission to compose or reply from that address is represented by `SendAsGrant`.
- Forwarding destinations require verification before activation. Keep a local copy for company and security mail unless a retention policy explicitly says otherwise.
- Service accounts may provision and reconcile email resources. They should not own human correspondence or act as the default reader for company mail.
- Provider bindings for sender domains must carry SPF, DKIM, DMARC, bounce-domain, webhook, and suppression-list metadata.
- All inbound and outbound mail that affects company state should emit audit and delivery evidence rows.

## Resource model

| Resource | Cardinality | Purpose |
| --- | --- | --- |
| `EmailIdentity` | One per provisioned address | Organization-scoped Verself principal for a personal, role, system, compatibility, or operator address. |
| `EmailAddress` | Exactly one primary address per `EmailIdentity` | Canonical `(domain, local_part)` tuple. The tuple is unique after domain ownership is resolved. |
| `MailboxAccount` | Zero or one per `EmailIdentity` | Local JMAP-backed mailbox. Omit only for explicit outbound-only or SMTP-reject-only senders such as a strict `noreply@` policy. |
| `AddressRoute` | One or more per inbound-capable `EmailIdentity` | Ordered delivery actions: local mailbox, integration, verified forward, queue, reject, or quarantine. |
| `MailboxMembership` | Many actors to one mailbox identity | Read, triage, and mailbox mutation permission for humans or service accounts. |
| `SendAsGrant` | Many actors to one email identity | Permission to submit outbound mail with the identity's address in `From` or `Reply-To`. |
| `ForwardingDestination` | One verified external destination | External mailbox such as `integrations.anveio@gmail.com`, with verification, status, and retention policy. |
| `ForwardingRule` | Source identity to destination | Copy or redirect policy. Company role mail defaults to `copy` so Verself retains the canonical mailbox record. |
| `ProviderBinding` | Domain or sender identity to provider | Resend, Cloudflare, or future provider metadata, including DKIM/SPF/DMARC state, webhook state, rate limits, and provider sender IDs. |

The model intentionally avoids a general many-to-many relationship between identities and addresses. Delegation is expressed through mailbox membership, send-as grants, forwarding, and routing.

## Reply and forwarding model

Inbound mail is delivered to the source email identity's route graph. For role addresses, the local mailbox remains the canonical copy even when a forwarding rule sends a copy to an external inbox. Forwarding is a notification and continuity path; it is not the authority for mailbox state, retention, or reply permission.

Replies should go through email-service. A signed-in human actor opens a mailbox through `MailboxMembership`, chooses an address allowed by `SendAsGrant`, and submits the reply through email-service. Email-service records the actor, selected email identity, provider, provider message ID, thread reference, sent copy, and audit evidence. The provider adapter can be Resend, Cloudflare, Stalwart submission, or another implementation without changing the visible user workflow.

External Gmail forwarding is acceptable as a temporary read path. Replying from Gmail as a Verself role address should be treated as an interim operational shortcut because it weakens sent-copy retention, authorization, revocation, provider metadata capture, and audit joins.

## Default Verself seed

For `verself.sh`, the default company seed should create:

| Resource | Value |
| --- | --- |
| Platform owner personal mailbox | `anveio@verself.sh` |
| Public catch-all contact | `hello@verself.sh` |
| Commercial contact | `sales@verself.sh` |
| Customer support contact | `support@verself.sh` |
| Security disclosure contact | `security@verself.sh` |
| Abuse contact | `abuse@verself.sh` |
| Privacy contact | `privacy@verself.sh` |
| Legal contact | `legal@verself.sh` |
| Billing contact | `billing@verself.sh` |
| Hiring contact | `careers@verself.sh` |
| Press contact | `press@verself.sh` |
| Mail operations contact | `postmaster@verself.sh` |
| DNS operations contact | `hostmaster@verself.sh` |
| Website operations contact | `webmaster@verself.sh` |
| Transactional sender | `noreply@verself.sh` |
| Audience-mail sender | `updates@verself.sh` |
| External forwarding destination | `integrations.anveio@gmail.com` |
| Email resource reconciler | `platform-email-reconciler` service account |

The Aspect platform seed should create these email identities in the correct Zitadel organization, bind `anveio@verself.sh` to the platform-owner human identity, create managed non-human identities for the role and system addresses, create a verified forwarding destination for `integrations.anveio@gmail.com`, attach copy-forwarding rules for low-volume company role mail, and grant the platform owner explicit mailbox membership and send-as permissions where appropriate. The `platform-email-reconciler` service account may create and reconcile these resources but should not be the mailbox owner or default reader.
