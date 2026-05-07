import { createFileRoute } from "@tanstack/react-router";

import {
  Prose,
  SectionHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/_workshop/docs/governance")({
  component: GovernanceDocs,
  head: () => ({
    meta: [
      { title: "Governance - Verself Platform" },
      {
        name: "description",
        content:
          "Use the Governance API, SDK, CLI, and console surfaces for audit events and organization data exports.",
      },
    ],
  }),
});

function GovernanceDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Governance</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Query organization audit events, investigate high-risk operations, and export
          organization data through the same public contract used by the console and CLI.
        </p>
      </header>

      <section className="flex flex-col gap-4">
        <SectionHeading id="overview">Overview</SectionHeading>
        <SummaryPanel>
          <SummaryItem term="Service API">
            <code>governance.api.&lt;domain&gt;</code> exposes audit event queries and data export
            lifecycle operations.
          </SummaryItem>
          <SummaryItem term="Curated SDKs">
            The TypeScript and Go SDKs own auth headers, trace propagation, idempotency keys, error
            normalization, response validation, and binary download handling.
          </SummaryItem>
          <SummaryItem term="Telemetry">
            Governance operations emit tamper-evident audit rows and OpenTelemetry traces with the
            caller-provided trace context when supplied.
          </SummaryItem>
        </SummaryPanel>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="audit-events">Audit Events</SectionHeading>
        <Prose>
          <p>
            Audit queries support exact filters for actor, credential, target, event name,
            operation ID, operation type, service, source product area, result, risk level, cursor,
            and ordering. The high-risk view expands to high or critical risk events, write,
            delete, export, denied, and error outcomes.
          </p>
        </Prose>
        <Command>{`const verself = new Verself({
  bearerToken: process.env.VERSELF_TOKEN!,
  governanceURL: "https://governance.api.verself.sh",
});

const page = await verself.governance.listAuditEvents({
  high_risk: true,
  limit: 50,
  operation_type: "export",
});`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="data-exports">Data Exports</SectionHeading>
        <Prose>
          <p>
            Data exports produce a tar.gz artifact with a manifest and scoped JSON or JSONL files.
            Export creation is idempotent, and artifact download requests ask for
            <code> application/gzip</code>.
          </p>
        </Prose>
        <Command>{`export, err := client.Governance.CreateDataExport(ctx, verself.CreateGovernanceExportInput{
    Scopes: []verself.GovernanceExportScope{verself.GovernanceExportScopeAudit},
    IdempotencyKey: "audit-export:2026-05-07",
})

artifact, err := client.Governance.DownloadDataExport(ctx, export.ExportID)`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="commands">Commands</SectionHeading>
        <SummaryPanel>
          <SummaryItem term="Audit search">
            <code>verself audit events</code> projects URL-style filters into the Governance API.
          </SummaryItem>
          <SummaryItem term="Export lifecycle">
            <code>verself audit exports</code> lists, creates, reads, and downloads export
            artifacts.
          </SummaryItem>
          <SummaryItem term="Trace joins">
            <code>--traceparent</code> joins CLI calls to an existing trace for ClickHouse
            evidence.
          </SummaryItem>
        </SummaryPanel>
        <Command>{`verself audit events --high-risk --limit 50
verself audit events --operation-type export --result allowed --json
verself audit exports list
verself audit exports create --scope audit --idempotency-key audit-export:2026-05-07
verself audit exports download <export-id> ./export.tar.gz --json`}</Command>
      </section>
    </article>
  );
}

function Command({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-4 font-mono text-sm leading-6 text-[var(--treatment-ink)]">
      <code>{children}</code>
    </pre>
  );
}
