import { createFileRoute } from "@tanstack/react-router";
import {
  Page,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import { BuildRepositoriesPanel } from "~/features/source/components";
import { loadBuildsDashboard } from "~/features/source/queries";

export const Route = createFileRoute("/_shell/_authenticated/builds/")({
  loader: ({ context }) => loadBuildsDashboard(context.queryClient, context.auth),
  component: BuildsPage,
});

function BuildsPage() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Builds</PageTitle>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Repositories</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <BuildRepositoriesPanel />
        </PageSection>
      </PageSections>
    </Page>
  );
}
