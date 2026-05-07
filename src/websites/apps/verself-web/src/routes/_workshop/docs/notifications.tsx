import { createFileRoute } from "@tanstack/react-router";

import {
  DefinitionCard,
  DefinitionGrid,
  Prose,
  SectionHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

export const Route = createFileRoute("/_workshop/docs/notifications")({
  component: NotificationsDocs,
  head: () => ({
    meta: [
      { title: "Notifications - Verself Platform" },
      {
        name: "description",
        content:
          "Use the Notifications API, SDK, CLI, and console inbox for org-scoped operational events.",
      },
    ],
  }),
});

function NotificationsDocs() {
  return (
    <article className="flex flex-col gap-10 [&_h2]:scroll-mt-[var(--header-scroll-offset)] [&_h3]:scroll-mt-[var(--header-scroll-offset)]">
      <header className="flex flex-col gap-4 border-b border-border pb-8">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Docs
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Notifications</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Read operational events, update inbox state, and configure delivery preferences through
          the public Notifications API.
        </p>
      </header>

      <section className="flex flex-col gap-4">
        <SectionHeading id="overview">Overview</SectionHeading>
        <Prose>
          <p>
            Notifications are org-scoped, recipient-specific records produced by Verself services
            when an operator or customer needs to act on runtime state. The console inbox, SDKs, and
            CLI all call <code>notifications.api.&lt;domain&gt;</code> through the same public
            OpenAPI contract.
          </p>
        </Prose>
        <SummaryPanel>
          <SummaryItem term="Inbox">
            List unread and recent notifications for the caller.
          </SummaryItem>
          <SummaryItem term="Summary">
            Read the unread count, latest sequence, read cursor, and delivery preferences in one
            document.
          </SummaryItem>
          <SummaryItem term="State">
            Advance the monotonic read cursor, mark one notification read, dismiss one notification,
            or clear the inbox.
          </SummaryItem>
          <SummaryItem term="Delivery">
            Store web, email, push, and SMS preferences as an observed-version write.
          </SummaryItem>
        </SummaryPanel>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="resources">Resources</SectionHeading>
        <DefinitionGrid>
          <DefinitionCard
            term="Notification"
            definition="A recipient-sequenced message with kind, priority, title, body, optional action URL, resource reference, and read or dismissed timestamps."
          />
          <DefinitionCard
            term="Summary"
            definition="The caller's inbox projection: unread count, latest sequence, read cursor, delivery preferences, and latest notification."
          />
          <DefinitionCard
            term="Preferences"
            definition="A versioned document controlling global delivery plus web, email, push, and SMS channels."
          />
          <DefinitionCard
            term="Accepted event"
            definition="The publish-test acknowledgement containing the accepted event identifier and downstream trace context."
          />
        </DefinitionGrid>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="commands">Commands</SectionHeading>
        <Prose>
          <p>
            The CLI uses the Go SDK and the TypeScript console uses the TypeScript SDK. Both SDKs
            wrap generated clients from the public OpenAPI projection and own bearer auth,
            idempotency keys, tracing headers, schema validation, and error normalization.
          </p>
        </Prose>
        <Command>{`verself notifications list --limit 20
verself notifications summary --json
verself notifications preferences set --version 1 --enabled true --web-enabled true
verself notifications read-cursor 42
verself notifications read <notification-id>
verself notifications dismiss <notification-id>
verself notifications clear
verself notifications test --title "Canary" --body "Notifications API" --json`}</Command>
      </section>

      <section className="flex flex-col gap-4">
        <SectionHeading id="events">Events</SectionHeading>
        <Prose>
          <p>
            Notification writes produce PostgreSQL state changes and ClickHouse event rows. E2E
            verification should assert the API status, the notification or cursor row, and the
            ordered trace sequence for publish, projection, and inbox reads.
          </p>
        </Prose>
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
