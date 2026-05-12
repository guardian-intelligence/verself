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
import { Select } from "@verself/ui/components/ui/select";
import { toast } from "@verself/ui/components/ui/sonner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@verself/ui/components/ui/table";
import {
  useUpdateOrganizationMutation,
  useUpdateMemberRolesMutation,
} from "../mutations.ts";
import { organizationMembersQuery, organizationQuery } from "../queries.ts";
import { useSignedInAuth } from "../../react.ts";
import { useIAMApi } from "../iam-api.ts";
import type { Member, Organization, OrganizationRoleKey } from "../types.ts";
import { PermissionAlert } from "./error-alert.tsx";

const PERMISSION_ORGANIZATION_UPDATE = "iam:organization:update";
const PERMISSION_MEMBER_ROLE_UPDATE = "iam:member:update_role";

// Customer-facing roles. "owner" is intentionally omitted from the picker —
// ownership is assigned at org creation and protected server-side; the UI
// surfaces it as a read-only option if the current member holds it.
const ASSIGNABLE_ROLES = [
  { role_key: "admin", display_name: "Admin" },
  { role_key: "member", display_name: "Member" },
] as const;

const ACTIVE_MEMBER_STATE = "active";

function hasPermission(permissions: ReadonlyArray<string>, permission: string): boolean {
  return permissions.includes(permission);
}

function primaryRoleKey(roleKeys: ReadonlyArray<OrganizationRoleKey>): OrganizationRoleKey {
  // Highest-privilege role wins for display. Keeps row UI collapsed to a
  // single value even if the backend returns an array with historical grants.
  if (roleKeys.includes("owner")) return "owner";
  if (roleKeys.includes("admin")) return "admin";
  return "member";
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
  const canUpdateRoles = hasPermission(organization.permissions, PERMISSION_MEMBER_ROLE_UPDATE);

  const activeMembers = members.filter((member) => member.state === ACTIVE_MEMBER_STATE);

  return (
    <PageSections>
      <OrganizationSettingsSection
        canUpdateOrganization={canUpdateOrganization}
        key={organization.version}
        organization={organization}
      />
      <MembersSection
        canUpdateRoles={canUpdateRoles}
        members={activeMembers}
        orgAclVersion={organization.org_acl_version}
      />
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
          Your current role can view the organization but cannot edit it.
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
              {isSubmitting || mutation.isPending ? "Saving…" : "Save"}
            </Button>
          )}
        </form.Subscribe>
      </form>
    </PageSection>
  );
}

function MembersSection({
  canUpdateRoles,
  members,
  orgAclVersion,
}: {
  canUpdateRoles: boolean;
  members: ReadonlyArray<Member>;
  orgAclVersion: number;
}) {
  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Members</SectionTitle>
          <SectionDescription>Change a member's role to adjust their access.</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Member</TableHead>
              <TableHead className="w-[22rem]">Role</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.length > 0 ? (
              members.map((member) => (
                <MemberRow
                  canUpdateRoles={canUpdateRoles}
                  key={member.user_id}
                  member={member}
                  orgAclVersion={orgAclVersion}
                />
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={2} className="py-8 text-center align-middle">
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

function MemberRow({
  canUpdateRoles,
  member,
  orgAclVersion,
}: {
  canUpdateRoles: boolean;
  member: Member;
  orgAclVersion: number;
}) {
  const mutation = useUpdateMemberRolesMutation();
  const currentRole = primaryRoleKey(member.role_keys);
  const isOwner = currentRole === "owner";

  async function syncRole(nextRole: OrganizationRoleKey) {
    if (!canUpdateRoles) {
      toast.error("You don't have permission to change roles.");
      return;
    }
    if (isOwner) {
      toast.error("The organization owner's role can't be changed here.");
      return;
    }
    if (nextRole === currentRole) {
      return;
    }
    if (mutation.isPending) {
      toast.info("Still syncing the last role change.");
      return;
    }
    try {
      await mutation.mutateAsync({
        expectedOrgAclVersion: orgAclVersion,
        expectedRoleKeys: [...member.role_keys],
        roleKeys: [nextRole],
        userId: member.user_id,
      });
      toast.success("Role synced", {
        description: `${member.email} is now ${nextRole}.`,
      });
    } catch (error) {
      toast.error("Role sync failed", {
        description: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return (
    <TableRow>
      <TableCell className="align-middle">
        <div className="font-medium">{member.display_name || member.email}</div>
        <div className="break-all text-xs text-muted-foreground">{member.email}</div>
      </TableCell>
      <TableCell className="align-middle">
        <Select
          value={currentRole}
          onChange={(event) => void syncRole(assignableRoleKey(event.target.value))}
          aria-busy={mutation.isPending}
          aria-label={`Role for ${member.email}`}
          aria-readonly={isOwner || undefined}
          className="w-full"
        >
          {isOwner ? <option value="owner">Owner</option> : null}
          {ASSIGNABLE_ROLES.map((role) => (
            <option key={role.role_key} value={role.role_key}>
              {role.display_name}
            </option>
          ))}
        </Select>
      </TableCell>
    </TableRow>
  );
}

function assignableRoleKey(value: string): OrganizationRoleKey {
  if (value === "admin" || value === "member") {
    return value;
  }
  throw new Error(`unsupported organization role: ${value}`);
}
