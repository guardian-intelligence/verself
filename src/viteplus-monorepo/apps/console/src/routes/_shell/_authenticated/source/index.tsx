import { createFileRoute } from "@tanstack/react-router";
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
} from "@forge-metal/ui/components/ui/page";
import { SourceRepositoriesPanel, SourceRepositoryForm } from "~/features/source/components";
import { loadSourceRepositories } from "~/features/source/queries";

export const Route = createFileRoute("/_shell/_authenticated/source/")({
  loader: ({ context }) => loadSourceRepositories(context.queryClient, context.auth),
  component: SourcePage,
});

function SourcePage() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Source</PageTitle>
          <PageDescription>Private repositories managed through Forge Metal.</PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>New repository</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <SourceRepositoryForm />
        </PageSection>

        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Repositories</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          <SourceRepositoriesPanel />
        </PageSection>
      </PageSections>
    </Page>
  );
}
