import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
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
import { Switch } from "@verself/ui/components/ui/switch";
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
  useInviteMemberMutation,
  usePutMemberCapabilitiesMutation,
  useUpdateOrganizationMutation,
  useUpdateMemberRolesMutation,
} from "../mutations.ts";
import {
  organizationMembersQuery,
  organizationMemberCapabilitiesQuery,
  organizationQuery,
} from "../queries.ts";
import { useSignedInAuth } from "../../react.ts";
import { useIAMApi } from "../iam-api.ts";
import type { Member, MemberCapabilities, Organization } from "../types.ts";
import { PermissionAlert } from "./error-alert.tsx";

const PERMISSION_ORGANIZATION_WRITE = "iam:organization:write";
const PERMISSION_MEMBER_INVITE = "iam:member:invite";
const PERMISSION_MEMBER_ROLES_WRITE = "iam:member:roles:write";
const PERMISSION_MEMBER_CAPABILITIES_WRITE = "iam:member_capabilities:write";

// Customer-facing roles. "owner" is intentionally omitted from the picker —
// ownership is assigned at org creation and protected server-side; the UI
// surfaces it as a read-only option if the current member holds it.
const ASSIGNABLE_ROLES = [
  { role_key: "admin", display_name: "Admin" },
  { role_key: "member", display_name: "Member" },
] as const;

const DEFAULT_INVITE_ROLE = "member";
const ACTIVE_MEMBER_STATE = "USER_STATE_ACTIVE";

function hasPermission(permissions: ReadonlyArray<string>, permission: string): boolean {
  return permissions.includes(permission);
}

function primaryRoleKey(roleKeys: ReadonlyArray<string>): string {
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
  const memberCapabilities = useSuspenseQuery(organizationMemberCapabilitiesQuery(auth, api)).data;

  const canInvite = hasPermission(organization.permissions, PERMISSION_MEMBER_INVITE);
  const canUpdateOrganization = hasPermission(
    organization.permissions,
    PERMISSION_ORGANIZATION_WRITE,
  );
  const canUpdateRoles = hasPermission(organization.permissions, PERMISSION_MEMBER_ROLES_WRITE);
  const canEditCapabilities = hasPermission(
    organization.permissions,
    PERMISSION_MEMBER_CAPABILITIES_WRITE,
  );

  const activeMembers = members.filter((member) => member.state === ACTIVE_MEMBER_STATE);

  return (
    <PageSections>
      <OrganizationSettingsSection
        canUpdateOrganization={canUpdateOrganization}
        key={organization.version}
        organization={organization}
      />
      <InviteMemberSection canInvite={canInvite} />
      <MembersSection
        canUpdateRoles={canUpdateRoles}
        members={activeMembers}
        orgAclVersion={organization.org_acl_version}
      />
      <CapabilitySection
        canEditCapabilities={canEditCapabilities}
        key={memberCapabilities.document.version}
        memberCapabilities={memberCapabilities}
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

function InviteMemberSection({ canInvite }: { canInvite: boolean }) {
  const inviteMutation = useInviteMemberMutation();
  const form = useForm({
    defaultValues: {
      email: "",
      roleKey: DEFAULT_INVITE_ROLE,
    },
    // Preconditions are enforced at submit time, not by disabling the button.
    // Every failure mode surfaces as a specific toast so the user knows why
    // the action didn't land.
    onSubmit: async ({ value }) => {
      if (!canInvite) {
        toast.error("You don't have permission to invite members.");
        return;
      }
      const trimmedEmail = value.email.trim();
      if (!trimmedEmail) {
        toast.error("Enter an email address to send the invite.");
        return;
      }
      if (inviteMutation.isPending) {
        toast.info("Still sending the last invite…");
        return;
      }
      try {
        await inviteMutation.mutateAsync({
          email: trimmedEmail,
          roleKeys: [value.roleKey],
        });
        toast.success("Invite sent", {
          description: `${trimmedEmail} will receive an email invite shortly.`,
        });
        form.reset();
      } catch (error) {
        toast.error("Invite failed", {
          description: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });

  const inviteDescriptionId = "invite-member-permission-hint";

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Invite member</SectionTitle>
          <SectionDescription>New members receive an email invite.</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      {!canInvite ? (
        <PermissionAlert id={inviteDescriptionId} title="Invite permission required">
          Your current role can view members but cannot invite users.
        </PermissionAlert>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
        className="flex flex-col gap-3 sm:flex-row sm:items-end"
      >
        <form.Field name="email">
          {(field) => (
            <div className="flex-1 space-y-1.5">
              <Label htmlFor={field.name}>Email</Label>
              <Input
                id={field.name}
                type="email"
                placeholder="teammate@example.com"
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="roleKey">
          {(field) => (
            <div className="space-y-1.5 sm:w-40">
              <Label htmlFor={field.name}>Role</Label>
              <Select
                id={field.name}
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              >
                {ASSIGNABLE_ROLES.map((role) => (
                  <option key={role.role_key} value={role.role_key}>
                    {role.display_name}
                  </option>
                ))}
              </Select>
            </div>
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.isSubmitting}>
          {(isSubmitting) => (
            <Button
              type="submit"
              aria-busy={isSubmitting || inviteMutation.isPending}
              aria-describedby={!canInvite ? inviteDescriptionId : undefined}
              className="sm:shrink-0"
            >
              {isSubmitting || inviteMutation.isPending ? "Inviting…" : "Invite"}
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
                    Invited users appear here once they accept the invite.
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

  async function syncRole(nextRole: string) {
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
          onChange={(event) => void syncRole(event.target.value)}
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

function CapabilitySection({
  canEditCapabilities,
  memberCapabilities,
}: {
  canEditCapabilities: boolean;
  memberCapabilities: MemberCapabilities;
}) {
  const mutation = usePutMemberCapabilitiesMutation();
  const initialEnabled = new Set(memberCapabilities.document.enabled_keys);
  const defaultValues = Object.fromEntries(
    memberCapabilities.catalog.map((capability) => [
      capability.key,
      initialEnabled.has(capability.key),
    ]),
  ) as Record<string, boolean>;
  const [enabledByKey, setEnabledByKey] = useState(defaultValues);

  async function syncCapability(key: string, next: boolean) {
    if (!canEditCapabilities) {
      toast.error("You don't have permission to edit member capabilities.");
      return;
    }
    if (mutation.isPending) {
      toast.info("Still syncing capabilities.");
      return;
    }
    const previous = enabledByKey;
    const nextValue = { ...enabledByKey, [key]: next };
    setEnabledByKey(nextValue);
    const enabledKeys = Object.entries(nextValue)
      .filter(([, enabled]) => enabled)
      .map(([capabilityKey]) => capabilityKey)
      .sort();
    try {
      await mutation.mutateAsync({
        enabled_keys: enabledKeys,
        version: memberCapabilities.document.version,
      });
      toast.success("Capabilities synced");
    } catch (error) {
      setEnabledByKey(previous);
      toast.error("Capabilities sync failed", {
        description: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Member capabilities</SectionTitle>
          <SectionDescription>
            Toggle which actions the member role can take. Owners and admins always have full
            access.
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      {!canEditCapabilities ? (
        <PermissionAlert title="Capability edit permission required">
          Your current role can view member capabilities but cannot edit them.
        </PermissionAlert>
      ) : null}

      <div
        className="divide-y divide-border rounded-md border border-border"
        aria-busy={mutation.isPending}
      >
        {memberCapabilities.catalog.map((capability) => {
          const switchId = `capability-${capability.key}`;
          return (
            <div className="flex items-start justify-between gap-4 p-4" key={capability.key}>
              <div className="space-y-1">
                <Label htmlFor={switchId} className="text-sm font-medium">
                  {capability.label}
                </Label>
                <p className="text-sm text-muted-foreground">{capability.description}</p>
              </div>
              <Switch
                id={switchId}
                checked={Boolean(enabledByKey[capability.key])}
                onClick={(event) => {
                  event.preventDefault();
                  void syncCapability(capability.key, !enabledByKey[capability.key]);
                }}
                aria-label={capability.label}
                aria-readonly={!canEditCapabilities || undefined}
              />
            </div>
          );
        })}
      </div>
    </PageSection>
  );
}
