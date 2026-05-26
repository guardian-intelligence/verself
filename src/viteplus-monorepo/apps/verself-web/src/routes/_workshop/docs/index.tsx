import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";

export const Route = createFileRoute("/_workshop/docs/")({
  component: DocsOverview,
  head: () => ({
    meta: [{ title: "Docs - Verself" }],
  }),
});

function DocsOverview() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Docs</PageTitle>
          <PageDescription>
            Product, SDK, policy, and API reference material for Verself.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Reference</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <div className="flex flex-col gap-2 text-sm">
            <Link className="font-medium underline underline-offset-4" to="/docs/reference">
              Public API reference
            </Link>
            <Link
              className="font-medium underline underline-offset-4"
              to="/docs/reference/iam/errors"
            >
              IAM auth error catalog
            </Link>
            <Link
              className="font-medium underline underline-offset-4"
              to="/docs/reference/github-integration/errors"
            >
              GitHub integration error catalog
            </Link>
          </div>
        </PageSection>
      </PageSections>
    </Page>
  );
}
