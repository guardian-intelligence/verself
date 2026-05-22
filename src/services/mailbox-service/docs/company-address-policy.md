# Company Email Address Policy

Company email addresses are durable product resources. They should encode a stable function, a human identity, or a system sender. They should not encode executive titles, temporary reporting structure, or fundraising posture.

## Standards baseline

- `postmaster@<domain>` is required for any domain that accepts SMTP delivery. [RFC 5321](https://www.rfc-editor.org/rfc/rfc5321.html) requires SMTP systems that support mail delivery or relay to support the reserved `postmaster` local-part case-insensitively.
- [RFC 2142](https://www.rfc-editor.org/rfc/rfc2142.html) defines conventional role mailbox names for common functions, including `abuse`, `security`, `hostmaster`, `webmaster`, `info`, `sales`, and `support`.
- [RFC 9116](https://www.rfc-editor.org/rfc/rfc9116.html) `security.txt` requires a `Contact` field for vulnerability reporting. `security@<domain>` is the clean mail contact for that file when email is exposed.

## Address classes

| Class | Owner | Used for | Product behavior |
| --- | --- | --- | --- |
| Personal | One human identity | Human correspondence and account ownership, for example `anveio@verself.sh` | Provision as a mailbox bound to a Zitadel user. Disable when the user leaves or the address is rotated. |
| Role | Company function | External or internal traffic for a durable function, for example `security@verself.sh` | Route to a shared mailbox, ticket queue, or verified forwarding destination. Grant humans explicit membership and send-as permission. |
| System sender | Product automation | Transactional messages, product notifications, waitlist messages, broadcasts, and receipts | Send through the email-service provider abstraction. Store delivery attempts, webhooks, suppression state, and provider message IDs. |
| Operator mailbox | Mail infrastructure operations | Required and conventional mail operations such as `postmaster@verself.sh` | Restrict membership to operators. Monitor separately from ordinary support traffic. |

## Excluded aliases

Do not provision these addresses:

| Address | Reason |
| --- | --- |
| `ceo@<domain>` | Use a real personal address for the founder and role addresses for company functions. |
| `founder@<domain>` | Same handling as `ceo@`; it is a title alias with unclear routing and reply ownership. |
| `founders@<domain>` | Same handling as `founder@`; use `hello@`, `press@`, `sales@`, or a personal address depending on the context. |

## Primary addresses

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `hello@<domain>` | General inbound that does not have a more specific function. This is the safe public contact for early-stage site visitors, partners, and people with unclear intent. | Shared company mailbox; temporary verified forward to the founder is acceptable while volume is low. | Founder and designated company operators. |
| `sales@<domain>` | Prospective customers, procurement, pricing questions, pilots, partnership sales, and VC introductions that are explicitly commercial. | Shared sales mailbox or CRM ingestion. | Founder and sales operators. |
| `support@<domain>` | Product users asking for help, broken workflows, account issues, operational incidents visible to customers, and support tickets. | Support queue or shared support mailbox. | Support operators. |
| `security@<domain>` | Vulnerability reports, coordinated disclosure, security questions, security.txt contact, and reports that require restricted handling. | Restricted security mailbox with local retention. External forwarding requires verification and local copy retention. | Security responders only. |
| `abuse@<domain>` | Spam reports, harmful use of the platform, policy violations, deliverability complaints from network operators, and user-safety escalations. | Restricted trust-and-safety or operator mailbox. | Abuse responders only. |
| `privacy@<domain>` | Data subject requests, privacy-policy contact, deletion/export requests, regulator privacy contact, and questions about personal data handling. | Restricted privacy/legal mailbox. | Privacy and legal operators only. |
| `legal@<domain>` | Legal notices, contract redlines, subpoenas, registered-agent routing, terms questions, and vendor legal mail. | Restricted legal mailbox. | Legal operators and approved founder identity. |
| `billing@<domain>` | Invoices, receipts, payment failures, procurement paperwork, tax forms, purchase orders, and customer billing questions. | Billing queue or finance mailbox. | Billing operators. |
| `careers@<domain>` | Candidate inbound, recruiter messages, referrals, interview logistics, and hiring process questions. | Hiring mailbox or applicant-tracking integration. | Hiring operators. |
| `press@<domain>` | Journalists, podcasts, conference organizers, analyst requests, and public-relations inbound. | Founder or communications mailbox. | Founder and communications operators. |
| `postmaster@<domain>` | SMTP delivery problems, mail server reports, blocklist operators, and standards-required mail operations. | Restricted mail-ops mailbox; monitored even if the domain has low traffic. | Mail operators only. |
| `hostmaster@<domain>` | DNS, registrar, zone delegation, and domain-operator contact. | Restricted infrastructure mailbox. | Infrastructure operators only. |
| `webmaster@<domain>` | Website availability problems, broken public pages, crawler issues, and legacy website-operator contact. | Website operations queue or shared company mailbox. | Website operators. |
| `noreply@<domain>` | Transactional mail where replies cannot be handled in the same thread, for example auth codes, machine-generated receipts, and one-way system events. | Reject at SMTP with a clear support pointer, or accept into a low-priority monitored mailbox. Never silently discard while the address is public. | Email-service automation only. Humans should not reply as `noreply`. |

## Compatibility aliases

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `info@<domain>` | Vendor forms, standards-oriented senders, or legacy contacts require an `info` mailbox. Public site copy should prefer `hello@`. | Alias to `hello@`. | Same grants as `hello@`; avoid using as the visible sender. |
| `marketing@<domain>` | A third-party service or partner specifically asks for a marketing contact. Broadcasts should use `updates@`. | Alias to `press@` or `sales@` depending on source. | No default human send-as grant. |

## High-volume audience mail

Use a dedicated role sender for waitlist and newsletter traffic:

| Address | Use when | Default route | Send-as policy |
| --- | --- | --- | --- |
| `updates@<domain>` | Product updates, newsletter sends, waitlist lifecycle messages, launch announcements, and broadcast campaigns. | Replies route to `hello@` until a dedicated community or marketing queue exists. Delivery events route to provider webhooks. | Email-service automation. Humans may reply only through an explicit `SendAsGrant`. |

`updates@` should be the visible `From` address for audience mail. `noreply@` should be reserved for product events where a reply would be misleading. Audience mail needs unsubscribe handling, suppression-list checks, bounce and complaint webhooks, idempotent send jobs, and per-campaign evidence rows.

## Provisioning rules

- Every human platform user may have one or more personal email addresses. The primary address is a mailbox identity; secondary addresses are aliases unless an explicit mailbox is required.
- Every role address must have an `EmailAddress` record, one or more `AddressRoute` records, and an owner mailbox or integration target.
- Human access to a role mailbox is represented by `MailboxMembership`. Permission to reply from a role address is represented by `SendAsGrant`.
- Forwarding destinations require verification before activation. Keep a local copy for company and security mail unless a retention policy explicitly says otherwise.
- Service accounts may provision and reconcile email resources. They should not own human correspondence or act as the default reader for company mail.
- Provider bindings for sender domains must carry SPF, DKIM, DMARC, bounce-domain, webhook, and suppression-list metadata.
- All inbound and outbound mail that affects company state should emit audit and delivery evidence rows.

## Default Verself seed

For `verself.sh`, the default company seed should create:

| Resource | Value |
| --- | --- |
| Founder personal mailbox | `anveio@verself.sh` |
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

Until the internal official-inbox UI exists, role addresses may forward to a verified external mailbox such as `integrations.anveio@gmail.com` while also retaining local copies. Replying from a role address should go through email-service so sent copies, send-as grants, provider metadata, and audit evidence stay in Verself.
