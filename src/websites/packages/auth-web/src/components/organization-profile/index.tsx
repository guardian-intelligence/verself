import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import { toast } from "@verself/ui/components/ui/sonner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@verself/ui/components/ui/table";
import { useSignedInAuth } from "../../react.ts";
import { useIAMApi } from "../iam-api.ts";
import { useUpdateOrganizationMutation } from "../mutations.ts";
import { organizationMembersQuery, organizationQuery } from "../queries.ts";
import type { Member, Organization } from "../types.ts";
import { PermissionAlert } from "./error-alert.tsx";

const PERMISSION_ORGANIZATION_UPDATE = "iam:organization:update";
const ACTIVE_MEMBER_STATE = "active";

function hasPermission(permissions: ReadonlyArray<string>, permission: string): boolean {
  return permissions.includes(permission);
}

export interface OrganizationProfileProps {
  readonly heading?: string;
}

export function OrganizationProfile(_props: OrganizationProfileProps = {}) {
  const auth = useSignedInAuth();
  const api = useIAMApi();
  const organization = useSuspenseQuery(organizationQuery(auth, api)).data;
  const members = useSuspenseQuery(organizationMembersQuery(auth, api)).data;

  const canUpdateOrganization = hasPermission(
    organization.permissions,
    PERMISSION_ORGANIZATION_UPDATE,
  );

  const activeMembers = members.filter((member) => member.state === ACTIVE_MEMBER_STATE);

  return (
    <PageSections>
      <OrganizationSettingsSection
        canUpdateOrganization={canUpdateOrganization}
        key={organization.version}
        organization={organization}
      />
      <MembersSection members={activeMembers} />
    </PageSections>
  );
}

function OrganizationSettingsSection({
  canUpdateOrganization,
  organization,
}: {
  canUpdateOrganization: boolean;
  organization: Organization;
}) {
  const mutation = useUpdateOrganizationMutation();
  const form = useForm({
    defaultValues: {
      displayName: organization.display_name,
      slug: organization.slug,
    },
    onSubmit: async ({ value }) => {
      if (!canUpdateOrganization) {
        toast.error("You don't have permission to update the organization.");
        return;
      }
      if (mutation.isPending) {
        toast.info("Still syncing the last organization change.");
        return;
      }
      const displayName = value.displayName.trim();
      const slug = value.slug.trim().toLowerCase();
      if (!displayName || !slug) {
        toast.error("Display name and slug are required.");
        return;
      }
      if (displayName === organization.display_name && slug === organization.slug) {
        toast.info("Organization is already up to date.");
        return;
      }
      try {
        await mutation.mutateAsync({
          display_name: displayName,
          slug,
          version: organization.version,
        });
        toast.success("Organization synced");
      } catch (error) {
        toast.error("Organization sync failed", {
          description: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Organization</SectionTitle>
          <SectionDescription>
            Friendly names used across the console and Git remotes.
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      {!canUpdateOrganization ? (
        <PermissionAlert title="Organization edit permission required">
          Your current access can view the organization but cannot edit it.
        </PermissionAlert>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
        className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end"
      >
        <form.Field name="displayName">
          {(field) => (
            <div className="space-y-1.5">
              <Label htmlFor={field.name}>Display name</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="slug">
          {(field) => (
            <div className="space-y-1.5">
              <Label htmlFor={field.name}>Slug</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </div>
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.isSubmitting}>
          {(isSubmitting) => (
            <Button
              type="submit"
              aria-busy={isSubmitting || mutation.isPending}
              className="sm:shrink-0"
            >
              {isSubmitting || mutation.isPending ? "Saving..." : "Save"}
            </Button>
          )}
        </form.Subscribe>
      </form>
    </PageSection>
  );
}

function MembersSection({ members }: { members: ReadonlyArray<Member> }) {
  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Members</SectionTitle>
          <SectionDescription>People provisioned in the identity provider.</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Member</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.length > 0 ? (
              members.map((member) => <MemberRow key={member.user_id} member={member} />)
            ) : (
              <TableRow>
                <TableCell className="py-8 text-center align-middle">
                  <p className="font-medium">No members</p>
                  <p className="text-sm text-muted-foreground">
                    Members appear here once they are provisioned in the identity provider.
                  </p>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </PageSection>
  );
}

function MemberRow({ member }: { member: Member }) {
  return (
    <TableRow>
      <TableCell className="align-middle">
        <div className="font-medium">{member.display_name || member.email}</div>
        <div className="break-all text-xs text-muted-foreground">{member.email}</div>
      </TableCell>
    </TableRow>
  );
}
