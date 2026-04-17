import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";

import { cn } from "@forge-metal/ui/lib/utils";
import { RETENTION, formatWindowValue, type Window } from "~/lib/policy-catalog";
import {
  ChangesSection,
  ContactSection,
  DefinitionCard,
  DefinitionGrid,
  PolicyArticle,
  PolicyHeader,
  Prose,
  SectionHeading,
  SubHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

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
    <PolicyArticle>
      <PolicyHeader title="Data Retention" policyId="data-retention" />
      <Summary />
      <Scope />
      <Roles />
      <Lifecycle />
      <RetentionWindows />
      <DataExport />
      <DataSubjectRights />
      <Extensions />
      <LegalHold />
      <FinalDeletion />
      <AnonymizedData />
      <IncidentData />
      <OperatorHandling />
      <ChangesSection policyId="data-retention" />
      <ContactSection operatorDomain={operatorDomain} primary="privacy" />
    </PolicyArticle>
  );
}

// ─── sections ────────────────────────────────────────────────────────────────

function Summary() {
  const durable = RETENTION.windows.find((w) => w.id === "durable_data");
  const billing = RETENTION.windows.find((w) => w.id === "billing_records");
  const pendingDays = extractDeleteAfterDays(durable);
  const billingYears = extractRetainYears(billing);
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Durable data">
          retained while your account is open; preserved for {pendingDays} days after closure.
        </SummaryItem>
        <SummaryItem term="Export">
          available throughout the first {RETENTION.export.post_closure_days} days after closure.
        </SummaryItem>
        <SummaryItem term="Billing records">
          retained for at least {billingYears} years for tax and accounting compliance, longer where
          law requires.
        </SummaryItem>
        <SummaryItem term="Backups">not provided — you are responsible for your own.</SummaryItem>
      </SummaryPanel>
      <Prose>
        <p>
          This page describes what Forge Metal keeps on your behalf, how long we keep it, and under
          what conditions it can be exported, preserved, or deleted. The retention windows listed
          here are commitments; they apply equally to every customer organization on this
          deployment, and the same windows drive the observability-store TTLs and deletion
          scheduler that execute them.
        </p>
        <p>
          Our own organizations — the platform tenant and its parent-company tenant — are subject to
          this policy on the same terms as any other customer. See{" "}
          <a href="#operator">Operator handling</a>.
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
          deployment. It does not cover data held by third-party subprocessors we integrate with;
          their retention is governed by their own DPAs, listed on{" "}
          <a href="/policy/subprocessors">our subprocessor page</a>.
        </p>
        <p>The categories below are referenced throughout this document.</p>
      </Prose>
      <DefinitionGrid>
        <DefinitionCard
          term="Durable data"
          definition={`Persistent VM disks, workspace volumes, and any object you have created with the expectation that it survives between sessions. Engineering calls this "durable customer data."`}
        />
        <DefinitionCard
          term="Operational data"
          definition="Logs, traces, metrics, and audit records generated as a byproduct of running your workloads. Each signal has its own TTL."
        />
        <DefinitionCard
          term="Billing records"
          definition="Invoices, payment events, ledger entries, and tax documents. Governed by statutory retention that outlives your account."
        />
        <DefinitionCard
          term="Backups"
          definition="Forge Metal does not currently offer a backup product. Customers are responsible for their own backups unless a backup product is purchased. Export during the retention window is not a substitute for backups."
        />
      </DefinitionGrid>
    </section>
  );
}

function Roles() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="roles">Roles under data-protection law</SectionHeading>
      <Prose>
        <p>
          Forge Metal plays two distinct roles with respect to the data it handles. Which role
          applies determines who is responsible for responding to data-subject requests and under
          what legal basis the data is processed.
        </p>
        <ul>
          <li>
            <strong>Controller</strong> for your organization's account and billing information —
            administrator contacts, authentication events, invoices, tax records, usage metering
            aggregates.
          </li>
          <li>
            <strong>Processor</strong> for the workload data your users place into the substrate —
            durable volumes, execution logs, mailboxes, and other objects your organization creates
            on our compute. Forge Metal processes that data only on your documented instructions, in
            line with GDPR Art. 28 and our <a href="/policy/dpa">Data Processing Addendum</a>.
          </li>
        </ul>
        <p>
          A full <a href="/policy/privacy#ropa">Record of Processing Activities</a> is published as
          part of the Privacy Policy.
        </p>
      </Prose>
    </section>
  );
}

function Lifecycle() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="lifecycle">Account lifecycle</SectionHeading>
      <Prose>
        <p>
          Every Forge Metal account passes through up to five observable states. The current state
          is visible on your billing page, and every transition is written to an audit log you can
          read at <code>Billing → Activity</code> or via the audit-events API.
        </p>
      </Prose>
      <ol className="flex flex-col gap-0 overflow-hidden rounded-lg border border-border bg-card md:flex-row">
        {RETENTION.state_machine.map((state, i) => (
          <li
            key={state.key}
            className={cn(
              "flex-1 border-border p-4",
              i < RETENTION.state_machine.length - 1 && "border-b md:border-b-0 md:border-r",
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
        <ul>
          {RETENTION.transitions.map((t) => (
            <li key={`${t.from}->${t.to}`}>
              <strong>
                {labelOf(t.from)} → {labelOf(t.to)}:
              </strong>{" "}
              {t.trigger}
            </li>
          ))}
        </ul>
      </Prose>
    </section>
  );
}

function RetentionWindows() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="retention">Retention windows</SectionHeading>
      <Prose>
        <p>
          Windows are measured from the state-transition timestamp recorded on your billing page. An{" "}
          <a href="#extensions">extension</a> granted by the operator supersedes the default window.
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
            {RETENTION.windows.map((row) => (
              <tr
                key={row.id}
                className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top"
              >
                <td className="font-medium">
                  {row.label}
                  {row.source ? (
                    <div className="mt-1 text-xs font-normal text-muted-foreground">
                      {row.source}
                    </div>
                  ) : null}
                </td>
                <td className="text-muted-foreground">{formatWindowValue(row.active)}</td>
                <td className="text-muted-foreground">{formatWindowValue(row.suspended)}</td>
                <td className="text-muted-foreground">{formatWindowValue(row.pending_deletion)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Prose>
        <p>
          Per-volume snapshot retention is a separate setting that governs how long snapshot
          generations are kept within each volume. It is independent from the account-lifecycle
          windows above; a volume with a 30-day snapshot policy still has its snapshots deleted
          with the parent volume when the account reaches final deletion.
        </p>
        <p>
          Jurisdictions with longer billing-records retention requirements — for example Germany (10
          years, HGB §257), France (10 years), and the United Kingdom (6 years) — are honored
          automatically where they apply to the customer's billing entity.
        </p>
      </Prose>
    </section>
  );
}

function DataExport() {
  const { export: exp } = RETENTION;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="export">Data export</SectionHeading>
      <Prose>
        <p>
          You can export your durable data throughout the{" "}
          {exp.available_during.map(labelOf).join(", ")} states. Export access continues for the
          first {exp.post_closure_days} days after your account enters pending deletion, then stops.
        </p>
        <p>
          Export is performed from your billing page via the <code>Export data</code> action or the
          equivalent API endpoint. {exp.delivery} Export is read-only and does not reset the
          retention clock; if you need more time to complete one, request an{" "}
          <a href="#extensions">extension</a> before the window closes.
        </p>
      </Prose>
      <SubHeading id="export-formats">Formats</SubHeading>
      <div className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/40 text-left">
              <th scope="col" className="px-4 py-3 font-medium">
                Class
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Format
              </th>
            </tr>
          </thead>
          <tbody>
            {exp.formats.map((f) => (
              <tr
                key={f.class}
                className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top"
              >
                <td className="font-medium">{f.class}</td>
                <td className="text-muted-foreground">{f.format}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function DataSubjectRights() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="dsr">Data-subject requests</SectionHeading>
      <Prose>
        <p>
          Under GDPR Articles 15–22 and comparable laws (California CPRA §1798.100 et seq., Virginia
          VCDPA, Colorado CPA, and others), end users have rights to access, correct, delete, and
          port personal data about them.
        </p>
        <ul>
          <li>
            <strong>Where Forge Metal is the processor</strong> — for data your organization
            processes about its own end users on our substrate — route requests through the customer
            (that is, your organization). We will forward requests received directly and cooperate
            with your response on the timeline the law requires.
          </li>
          <li>
            <strong>Where Forge Metal is the controller</strong> — billing contacts, organization
            administrators, support correspondence — requests go to the{" "}
            <a href="#contact">privacy mailbox</a>. We respond within the statutory window (30 days
            under GDPR, 45 days under CPRA) and record each request in the audit log.
          </li>
        </ul>
        <p>
          A deletion request served before the 90-day retention window closes is honored early; the
          same deletion guarantees and methods in <a href="#deletion">Final deletion</a> apply.
        </p>
      </Prose>
    </section>
  );
}

function Extensions() {
  const { extensions } = RETENTION;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="extensions">Extensions</SectionHeading>
      <Prose>
        <p>
          If you need more time to resolve a billing issue or complete an export, you can request an
          extension. Extensions are granted by Forge Metal operator staff and recorded with the{" "}
          {extensions.audited_fields.join(", ")} on the extension record.
        </p>
        <p>{extensions.clock_behavior}</p>
        <p>
          An extension does not restore compute and does not change the amount owed.{" "}
          {extensions.allow_multiple
            ? "Multiple extensions may be granted; each is audited independently. "
            : ""}
          {extensions.decline_conditions}
        </p>
      </Prose>
    </section>
  );
}

function LegalHold() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="legal-hold">Legal hold</SectionHeading>
      <Prose>
        <p>{RETENTION.legal_hold.behavior}</p>
      </Prose>
    </section>
  );
}

function FinalDeletion() {
  const { deletion } = RETENTION;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="deletion">Final deletion</SectionHeading>
      <Prose>
        <p>
          When the retention window closes, your durable data is deleted from our storage.
          {deletion.soft_delete ? "" : " This is not a soft delete."}{" "}
          {deletion.recoverable_after_execution
            ? "Deletion is recoverable by operator intervention."
            : "Once deletion has executed, Forge Metal cannot recover the data, even with operator intervention."}
        </p>
        <p>Deletion is implemented using:</p>
        <ul>
          {deletion.methods.map((m) => (
            <li key={m}>{m}</li>
          ))}
        </ul>
        <p>
          Billing records are retained beyond final deletion in accordance with applicable tax and
          accounting law. See <a href="#retention">Retention windows</a> for specifics.
        </p>
      </Prose>
    </section>
  );
}

function AnonymizedData() {
  const { anonymized_data } = RETENTION;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="anonymized">Anonymized and aggregated data</SectionHeading>
      <Prose>
        <p>
          {anonymized_data.description}{" "}
          {anonymized_data.retained_indefinitely
            ? "Such data falls outside the retention windows above."
            : ""}
        </p>
      </Prose>
    </section>
  );
}

function IncidentData() {
  const incident = RETENTION.windows.find((w) => w.id === "security_incident_artifacts");
  if (!incident) return null;
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="incident">Incident and forensic artifacts</SectionHeading>
      <Prose>
        <p>{incident.description}</p>
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
          subject to this policy on the same terms as any other customer. Internal usage accrues
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

// ─── helpers ─────────────────────────────────────────────────────────────────

function labelOf(key: string): string {
  return RETENTION.state_machine.find((s) => s.key === key)?.label ?? key;
}

function extractDeleteAfterDays(window: Window | undefined): number {
  if (!window) return 0;
  const v = window.pending_deletion;
  return v.kind === "delete_after" ? v.days : 0;
}

function extractRetainYears(window: Window | undefined): number {
  if (!window) return 0;
  const v = window.active;
  return v.kind === "retain_years" ? v.years : 0;
}
