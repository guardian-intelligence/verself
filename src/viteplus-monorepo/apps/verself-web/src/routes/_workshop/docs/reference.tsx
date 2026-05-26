import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import { SERVICE_CATALOG } from "~/lib/openapi-catalog";

export const Route = createFileRoute("/_workshop/docs/reference")({
  component: ApiReference,
  head: () => ({
    meta: [{ title: "API Reference - Verself" }],
  }),
});

function ApiReference() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  if (path !== "/docs/reference") {
    return <Outlet />;
  }

  return (
    <Page variant="full">
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>API Reference</PageTitle>
          <PageDescription>
            Public HTTP APIs and service-owned diagnostic references.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection id="iam-auth-errors">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>IAM Auth Errors</SectionTitle>
              <SectionDescription>
                Stable problem codes for account resolution, device sessions, provider linking, and
                device-code login.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <Link
            className="w-fit text-sm font-medium underline underline-offset-4"
            to="/docs/reference/iam/errors"
          >
            Open the error catalog
          </Link>
        </PageSection>

        <PageSection id="github-integration-errors">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>GitHub Integration Errors</SectionTitle>
              <SectionDescription>
                Stable lifecycle problem codes emitted while processing GitHub Actions jobs.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <Link
            className="w-fit text-sm font-medium underline underline-offset-4"
            to="/docs/reference/github-integration/errors"
          >
            Open the error catalog
          </Link>
        </PageSection>

        <PageSection id="hosted-runner-runtime">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Hosted Runner Runtime</SectionTitle>
              <SectionDescription>
                Guest filesystem paths and writable-state boundaries for hosted CI runners.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <Link
            className="w-fit text-sm font-medium underline underline-offset-4"
            to="/docs/reference/hosted-runners/runtime"
          >
            Open the runtime reference
          </Link>
        </PageSection>

        <PageSection id="schemas">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Service APIs</SectionTitle>
              <SectionDescription>
                Generated OpenAPI reference sections available in this build.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <div className="grid gap-2 sm:grid-cols-2">
            {SERVICE_CATALOG.map((service) => (
              <div key={service.id} className="rounded-md border border-border px-3 py-2 text-sm">
                <div className="font-medium">{service.title}</div>
                <div className="text-xs text-muted-foreground">
                  {service.subdomain ?? "internal"}
                </div>
              </div>
            ))}
          </div>
        </PageSection>
      </PageSections>
    </Page>
  );
}
