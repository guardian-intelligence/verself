import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";
import { Link as LinkIcon } from "lucide-react";

import { cn } from "@forge-metal/ui/lib/utils";

const getOperatorDomain = createServerFn({ method: "GET" }).handler(() => requireOperatorDomain());

export const Route = createFileRoute("/policy/data-retention")({
  component: DataRetentionPolicy,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Data Retention — Forge Metal Platform" },
      {
        name: "description",
        content:
          "How Forge Metal retains, exports, and deletes customer data across the billing lifecycle.",
      },
    ],
  }),
});

function DataRetentionPolicy() {
  const operatorDomain = Route.useLoaderData();
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)] [&_h3]:scroll-mt-[var(--header-scroll-offset)]">
      <PolicyHeader />
      <Summary />
      <Scope />
      <Lifecycle />
      <RetentionWindows />
      <DataExport />
      <Extensions />
      <FinalDeletion />
      <OperatorHandling />
      <Changes />
      <Contact operatorDomain={operatorDomain} />
    </article>
  );
}

function PolicyHeader() {
  return (
    <header className="flex flex-col gap-4 border-b border-border pb-8">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Platform Policy
      </p>
      <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Data Retention</h1>
      <dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
        <MetaPair label="Effective date" value="April 17, 2026" />
        <MetaPair label="Last updated" value="April 17, 2026" />
      </dl>
    </header>
  );
}

function MetaPair({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium tabular-nums">{value}</dd>
    </div>
  );
}

function SectionHeading({ id, children }: { id: string; children: React.ReactNode }) {
  return (
    <h2 id={id} className="group flex items-baseline gap-2 text-2xl font-semibold tracking-tight">
      <span>{children}</span>
      <a
        href={`#${id}`}
        aria-label="Anchor"
        className="opacity-0 transition-opacity group-hover:opacity-60"
      >
        <LinkIcon className="size-4" />
      </a>
    </h2>
  );
}

function Prose({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "flex flex-col gap-4 text-sm leading-6 text-foreground/90 md:text-[15px] md:leading-7",
        "[&_p]:max-w-[68ch] [&_ul]:max-w-[68ch] [&_ol]:max-w-[68ch]",
        "[&_a]:underline [&_a]:decoration-muted-foreground/60 [&_a]:underline-offset-4 [&_a:hover]:decoration-foreground",
        "[&_code]:rounded [&_code]:bg-secondary [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:text-[0.85em]",
        "[&_strong]:font-semibold [&_strong]:text-foreground",
        className,
      )}
    >
      {children}
    </div>
  );
}

// ─── sections ────────────────────────────────────────────────────────────────

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <aside className="relative rounded-lg border border-border bg-secondary/50 p-5 pl-6 before:absolute before:inset-y-3 before:left-0 before:w-0.5 before:rounded-full before:bg-foreground">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          At a glance
        </p>
        <ul className="mt-3 grid gap-x-6 gap-y-2 text-sm leading-6 md:grid-cols-2">
          <li>
            <strong className="font-medium">Durable data:</strong> retained while your account is
            open; preserved for 90 days after closure.
          </li>
          <li>
            <strong className="font-medium">Export:</strong> available throughout the first 30 days
            after closure.
          </li>
          <li>
            <strong className="font-medium">Billing records:</strong> retained for 7 years for tax
            and accounting compliance.
          </li>
          <li>
            <strong className="font-medium">Backups:</strong> not provided — you are responsible for
            your own.
          </li>
        </ul>
      </aside>
      <Prose>
        <p>
          This page describes what Forge Metal keeps on your behalf, how long we keep it, and under
          what conditions it can be exported, preserved, or deleted. The retention windows listed
          here are commitments; they apply equally to every customer organization on this
          deployment.
        </p>
        <p>
          Our own organizations — the platform tenant and its parent-company tenant — are subject to
          this policy in the same terms as any other customer. See{" "}
          <a href="#operator">Operator handling</a> for why.
        </p>
      </Prose>
    </section>
  );
}

function Scope() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="scope">Scope &amp; definitions</SectionHeading>
      <Prose>
        <p>
          This policy covers data Forge Metal stores in your organization's account on this
          deployment. It does not cover data held by third-party providers we integrate with; those
          retention rules are documented alongside each integration.
        </p>
        <p>The categories below are referenced throughout this document.</p>
      </Prose>
      <dl className="grid gap-4 md:grid-cols-2">
        <DefinitionCard
          term="Durable data"
          definition="Persistent VM disks, workspace volumes, and any object you have created with the expectation that it survives between sessions. Engineering calls this 'durable customer data.'"
        />
        <DefinitionCard
          term="Operational data"
          definition="Logs, traces, metrics, and audit records generated as a byproduct of running your workloads. Retained per-signal based on its usefulness and cost."
        />
        <DefinitionCard
          term="Billing records"
          definition="Invoices, payment events, ledger entries, and tax documents. Governed by separate statutory retention requirements that outlive your account."
        />
        <DefinitionCard
          term="Backups"
          definition="Forge Metal does not currently offer a backup product. You are responsible for your own backups unless a backup product is purchased. Export during the retention window is not a substitute for backups."
        />
      </dl>
    </section>
  );
}

function DefinitionCard({ term, definition }: { term: string; definition: string }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border bg-card p-4">
      <dt className="font-medium">{term}</dt>
      <dd className="text-sm leading-6 text-muted-foreground">{definition}</dd>
    </div>
  );
}

function Lifecycle() {
  const states: { key: string; label: string; blurb: string }[] = [
    {
      key: "active",
      label: "Active",
      blurb: "Your account is in good standing. All services run normally.",
    },
    {
      key: "past_due",
      label: "Past due",
      blurb: "A payment has failed. Services continue while retries run, up to 14 days.",
    },
    {
      key: "suspended",
      label: "Suspended",
      blurb: "Compute is stopped. Durable data is preserved and remains exportable.",
    },
    {
      key: "pending_deletion",
      label: "Pending deletion",
      blurb: "Account has closed. A 90-day countdown begins before deletion.",
    },
    {
      key: "deleted",
      label: "Deleted",
      blurb: "Durable data has been deleted. Billing records are retained.",
    },
  ];

  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="lifecycle">Account lifecycle</SectionHeading>
      <Prose>
        <p>
          Every Forge Metal account passes through up to five observable states. The current state
          is visible on your billing page, and every transition is written to an audit log you can
          read.
        </p>
      </Prose>
      <ol className="flex flex-col gap-0 overflow-hidden rounded-lg border border-border bg-card md:flex-row">
        {states.map((state, i) => (
          <li
            key={state.key}
            className={cn(
              "flex-1 border-border p-4",
              i < states.length - 1 && "border-b md:border-b-0 md:border-r",
            )}
          >
            <div className="flex items-center gap-2">
              <span className="flex size-5 items-center justify-center rounded-full bg-secondary text-[0.7rem] font-medium tabular-nums">
                {i + 1}
              </span>
              <span className="text-sm font-medium">{state.label}</span>
            </div>
            <p className="mt-2 text-xs leading-5 text-muted-foreground">{state.blurb}</p>
          </li>
        ))}
      </ol>
      <Prose>
        <p>
          The transition from <strong>active</strong> to <strong>past due</strong> happens when a
          payment attempt fails. The transition from <strong>past due</strong> to{" "}
          <strong>suspended</strong> happens after the Stripe retry window closes without payment
          (typically 14 days). The transition from <strong>suspended</strong> to{" "}
          <strong>pending deletion</strong> happens when your account is closed — either by you, by
          the operator for non-payment, or by contract expiration.
        </p>
      </Prose>
    </section>
  );
}

function RetentionWindows() {
  const rows: { data: string; active: string; suspended: string; pendingDeletion: string }[] = [
    {
      data: "Durable data",
      active: "Preserved",
      suspended: "Preserved",
      pendingDeletion: "Deleted at 90 days",
    },
    {
      data: "Per-volume snapshot generations",
      active: "Per your configured retention policy",
      suspended: "Per your configured retention policy",
      pendingDeletion: "Deleted with the parent volume",
    },
    {
      data: "Operational data (logs, traces, metrics)",
      active: "Per-signal TTL",
      suspended: "Per-signal TTL",
      pendingDeletion: "Per-signal TTL",
    },
    {
      data: "Billing records",
      active: "Retained for 7 years",
      suspended: "Retained for 7 years",
      pendingDeletion: "Retained for 7 years",
    },
    {
      data: "Backups",
      active: "Not provided",
      suspended: "Not provided",
      pendingDeletion: "Not provided",
    },
  ];

  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="retention">Retention windows</SectionHeading>
      <Prose>
        <p>
          Windows are measured from the date of the state transition recorded on your billing page.
          An <a href="#extensions">extension</a> granted by the operator supersedes the default
          window.
        </p>
      </Prose>
      <div className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/40 text-left">
              <th scope="col" className="px-4 py-3 font-medium">
                Data class
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Active
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Suspended
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Pending deletion
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr
                key={row.data}
                className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top"
              >
                <td className="font-medium">{row.data}</td>
                <td className="text-muted-foreground">{row.active}</td>
                <td className="text-muted-foreground">{row.suspended}</td>
                <td className="text-muted-foreground">{row.pendingDeletion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Prose>
        <p>
          Per-volume snapshot retention is a separate setting that governs how long ZFS snapshot
          generations are kept within each volume. It is independent from the account-lifecycle
          windows above; a volume with a 30-day snapshot policy still has its snapshots deleted with
          the parent volume when the account reaches final deletion.
        </p>
      </Prose>
    </section>
  );
}

function DataExport() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="export">Data export</SectionHeading>
      <Prose>
        <p>
          You can export your durable data throughout the active, past due, and suspended states.
          Export access continues for the first 30 days after your account enters pending deletion,
          then stops.
        </p>
        <p>
          Export is performed from your billing page via the <code>Export data</code> action or the
          equivalent API endpoint. Operational data and billing records can be exported through the
          same interface. Exports are delivered as signed URLs valid for 72 hours.
        </p>
        <p>
          Export is read-only and does not reset the retention clock. If you need more than 30 days
          of export access after closure, request an <a href="#extensions">extension</a> before the
          window closes.
        </p>
      </Prose>
    </section>
  );
}

function Extensions() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="extensions">Extensions</SectionHeading>
      <Prose>
        <p>
          If you need more time to resolve a billing issue or complete an export, you can request an
          extension. Extensions are granted by Forge Metal operator staff and are recorded with the
          requesting principal, the requested duration, and the justification.
        </p>
        <p>
          An extension resets the retention clock but does not restore compute and does not change
          the amount owed. You can request multiple extensions; each is audited independently. There
          is no default limit on the number of extensions, but repeated extension requests without a
          path to resolution may be declined.
        </p>
      </Prose>
    </section>
  );
}

function FinalDeletion() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="deletion">Final deletion</SectionHeading>
      <Prose>
        <p>
          When the retention window closes, your durable data is deleted from our storage. This is
          not a soft delete. Once deletion has executed, Forge Metal cannot recover the data, even
          with operator intervention.
        </p>
        <p>
          Billing records are retained beyond final deletion in accordance with applicable tax and
          accounting law. See <a href="#retention">Retention windows</a> for specifics.
        </p>
      </Prose>
    </section>
  );
}

function OperatorHandling() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="operator">Operator handling</SectionHeading>
      <Prose>
        <p>
          Forge Metal's own organizations — the platform tenant and its parent-company tenant — are
          subject to this policy in the same terms as any other customer. Internal usage accrues
          real invoices; those invoices are finalized with a 100% showback adjustment rather than
          being excluded from the billing pipeline.
        </p>
        <p>
          This keeps the account-lifecycle code path identical across customer and internal tenants,
          and ensures the true cost of operating the platform is visible in the same ledger
          customers see.
        </p>
      </Prose>
    </section>
  );
}

function Changes() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="changes">Changes to this policy</SectionHeading>
      <Prose>
        <p>
          This page is the source of truth for Forge Metal's data-retention commitments. Material
          changes take effect 30 days after they are announced by email to the account
          administrators on each affected organization. The date at the top of this page is the date
          the current version took effect.
        </p>
        <p>
          Previous versions of this policy are preserved in the platform repository's git history
          and can be retrieved on request.
        </p>
      </Prose>
    </section>
  );
}

function Contact({ operatorDomain }: { operatorDomain: string }) {
  const policyEmail = `policy@${operatorDomain}`;
  const securityEmail = `security@${operatorDomain}`;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="contact">Contact</SectionHeading>
      <Prose>
        <p>
          Questions about this policy, requests for data export, extension requests, or questions
          about a specific deletion timeline can be sent to{" "}
          <a href={`mailto:${policyEmail}`}>{policyEmail}</a>.
        </p>
        <p>
          Security reports should go to <a href={`mailto:${securityEmail}`}>{securityEmail}</a> and
          take precedence over routine policy questions.
        </p>
      </Prose>
    </section>
  );
}
