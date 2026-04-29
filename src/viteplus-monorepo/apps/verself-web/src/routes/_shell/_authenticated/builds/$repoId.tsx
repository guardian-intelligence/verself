import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { EmptyState } from "~/components/empty-state";
import {
  BuildRepositoryActiveBuildsPanel,
  buildRepositorySlug,
} from "~/features/source/components";
import { loadSourceRepositories, sourceRepositoriesQuery } from "~/features/source/queries";
import {
  Page,
  PageEyebrow,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";

export const Route = createFileRoute("/_shell/_authenticated/builds/$repoId")({
  loader: ({ context }) => loadSourceRepositories(context.queryClient, context.auth),
  component: BuildRepositoryPage,
});

function BuildRepositoryPage() {
  const auth = useSignedInAuth();
  const { repoId } = Route.useParams();
  const { repositories } = useSuspenseQuery(sourceRepositoriesQuery(auth)).data;
  const repo = repositories.find((candidate) => candidate.repo_id === repoId);
  const title = repo ? buildRepositorySlug(repo) : "Repository not found";

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageEyebrow>
            <Link to="/builds" className="hover:text-foreground">
              Builds
            </Link>
          </PageEyebrow>
          <PageTitle className={repo ? "font-mono" : undefined}>{title}</PageTitle>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection>
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Active Builds</SectionTitle>
            </SectionHeaderContent>
          </SectionHeader>
          {repo ? (
            <BuildRepositoryActiveBuildsPanel repo={repo} />
          ) : (
            <EmptyState title="Repository not found" body="This repository is not available." />
          )}
        </PageSection>
      </PageSections>
    </Page>
  );
}
