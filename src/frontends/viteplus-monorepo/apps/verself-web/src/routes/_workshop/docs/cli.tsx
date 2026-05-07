import { createFileRoute } from "@tanstack/react-router";

import { Prose, SectionHeading, SummaryItem, SummaryPanel } from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/_workshop/docs/cli")({
  component: CLIDocs,
  head: () => ({
    meta: [
      { title: "CLI - Verself Platform" },
      {
        name: "description",
        content:
          "Use the Verself CLI with auth profiles, organization context, API credentials, and Projects.",
      },
    ],
  }),
});

function CLIDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">CLI</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Operate Verself, cloned installations, organization state, API credentials, and Projects
          from one binary.
        </p>
      </header>

      <section className="flex flex-col gap-4">
        <SectionHeading id="profiles">Profiles</SectionHeading>
        <Prose>
          <p>
            A profile stores service endpoints and selected organization metadata. Bearer material
            is stored by reference in the local credential store.
          </p>
        </Prose>
        <Command>{`verself auth login --token-file /run/secrets/verself-token \
  --iam-url https://iam.api.example.com \
  --projects-url https://projects.api.example.com \
  --source-url https://source.api.example.com
verself auth whoami
verself auth token`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="organizations">Organizations</SectionHeading>
        <Prose>
          <p>
            Organization commands call IAM through the curated SDK. Mutating commands send
            idempotency keys and use the observed version from the current organization document.
          </p>
        </Prose>
        <Command>{`verself orgs list
verself orgs use guardian
verself orgs inspect --json
verself orgs update --version 4 --display-name "Guardian Intelligence"`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="api-credentials">API credentials</SectionHeading>
        <Prose>
          <p>
            API credentials are org-scoped IAM resources. Secret material is returned on create and
            roll responses only.
          </p>
        </Prose>
        <Command>{`verself orgs credentials create "CI" --permission projects:project:read --json
verself orgs credentials list
verself orgs credentials roll cred_123 --json
verself orgs credentials revoke cred_123`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="projects">Projects</SectionHeading>
        <SummaryPanel>
          <SummaryItem term="SDK first">
            CLI project commands use the Go SDK, which wraps the public Projects OpenAPI contract.
          </SummaryItem>
          <SummaryItem term="Auth context">
            The active profile provides the bearer token and service URL unless explicit flags or
            environment variables override them.
          </SummaryItem>
          <SummaryItem term="Trace context">
            Every SDK call can join an existing trace with <code>--traceparent</code>.
          </SummaryItem>
        </SummaryPanel>
        <Command>{`verself projects list
verself projects create "Console" --slug console
verself projects environments list <project-id>`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="repositories">Repositories</SectionHeading>
        <SummaryPanel>
          <SummaryItem term="Source API">
            Repository commands use the Go SDK over the public Source OpenAPI contract.
          </SummaryItem>
          <SummaryItem term="Checkout grants">
            Short-lived checkout tokens are minted through Source, so consumers do not handle
            durable Git credentials.
          </SummaryItem>
          <SummaryItem term="Workflow dispatch">
            Dispatch records are created through Source before sandbox execution consumes them.
          </SummaryItem>
        </SummaryPanel>
        <Command>{`verself repos list --project-id <project-id>
verself repos create <project-id> --default-branch main
verself repos credentials create --scope repo:read --scope repo:write --json
verself repos checkout-grants create <repo-id> --ref main --json
verself repos workflow-runs dispatch <repo-id> \
  --project-id <project-id> \
  --workflow-path .github/workflows/build.yml \
  --input target=linux`}</Command>
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
