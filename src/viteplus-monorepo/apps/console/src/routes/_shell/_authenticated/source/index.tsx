import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { deriveHTTPSOrigin, requireOperatorDomain } from "@verself/web-env";
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
import { SourceRepositoriesPanel } from "~/features/source/components";
import { loadSourceDashboard } from "~/features/source/queries";

const getGitOrigin = createServerFn({ method: "GET" }).handler(() =>
  deriveHTTPSOrigin("git", requireOperatorDomain()),
);

export const Route = createFileRoute("/_shell/_authenticated/source/")({
  loader: async ({ context }) => {
    const [gitOrigin] = await Promise.all([
      getGitOrigin(),
      loadSourceDashboard(context.queryClient, context.auth),
    ]);
    return { gitOrigin };
  },
  component: SourcePage,
});

function SourcePage() {
  const { gitOrigin } = Route.useLoaderData();

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Source</PageTitle>
          <PageDescription>
            Project-scoped repositories, branches, and hosted Git credentials.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Repositories</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <SourceRepositoriesPanel gitOrigin={gitOrigin} />
        </PageSection>
      </PageSections>
    </Page>
  );
}
