import { createFileRoute, Link } from "@tanstack/react-router";
import {
  PageEyebrow,
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import { orgPath } from "~/features/shell/org-routing";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/settings/organization/new")({
  component: CreateTeamPage,
});

function CreateTeamPage() {
  const { orgSlug } = Route.useParams();

  return (
    <PageSections>
      <PageSection>
        <PageEyebrow>
          <Link to={orgPath(orgSlug, "/settings/organization")} className="hover:text-foreground">
            ← Back to organization
          </Link>
        </PageEyebrow>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Create team</SectionTitle>
            <SectionDescription>
              Spin up a new team workspace. Members and billing are scoped per team.
            </SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>
      </PageSection>
    </PageSections>
  );
}
