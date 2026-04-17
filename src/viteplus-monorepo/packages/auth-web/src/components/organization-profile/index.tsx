import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Input } from "@forge-metal/ui/components/ui/input";
import { Label } from "@forge-metal/ui/components/ui/label";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { Select } from "@forge-metal/ui/components/ui/select";
import { Switch } from "@forge-metal/ui/components/ui/switch";
import { toast } from "@forge-metal/ui/components/ui/sonner";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import {
  useInviteMemberMutation,
  usePutMemberCapabilitiesMutation,
  useUpdateMemberRolesMutation,
} from "../mutations.ts";
import {
  organizationMembersQuery,
  organizationMemberCapabilitiesQuery,
  organizationQuery,
} from "../queries.ts";
import { useSignedInAuth } from "../../react.ts";
import { useIdentityApi } from "../identity-api.ts";
import type { Member, MemberCapabilities } from "../types.ts";
import { PermissionAlert } from "./error-alert.tsx";

const PERMISSION_MEMBER_INVITE = "identity:member:invite";
const PERMISSION_MEMBER_ROLES_WRITE = "identity:member:roles:write";
const PERMISSION_MEMBER_CAPABILITIES_WRITE = "identity:member_capabilities:write";

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
  const api = useIdentityApi();
  const organization = useSuspenseQuery(organizationQuery(auth, api)).data;
  const members = useSuspenseQuery(organizationMembersQuery(auth, api)).data;
  const memberCapabilities = useSuspenseQuery(organizationMemberCapabilitiesQuery(auth, api)).data;

  const canInvite = hasPermission(organization.permissions, PERMISSION_MEMBER_INVITE);
  const canUpdateRoles = hasPermission(organization.permissions, PERMISSION_MEMBER_ROLES_WRITE);
  const canEditCapabilities = hasPermission(
    organization.permissions,
    PERMISSION_MEMBER_CAPABILITIES_WRITE,
  );

  const activeMembers = members.filter((member) => member.state === ACTIVE_MEMBER_STATE);

  return (
    <PageSections>
      <InviteMemberSection canInvite={canInvite} />
      <MembersSection canUpdateRoles={canUpdateRoles} members={activeMembers} />
      <CapabilitySection
        canEditCapabilities={canEditCapabilities}
        // Remount on server version bump so the initial form state is
        // re-seeded from the authoritative document after save.
        key={memberCapabilities.document.version}
        memberCapabilities={memberCapabilities}
      />
    </PageSections>
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
              aria-disabled={!canInvite || undefined}
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
}: {
  canUpdateRoles: boolean;
  members: ReadonlyArray<Member>;
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
                <MemberRow canUpdateRoles={canUpdateRoles} key={member.user_id} member={member} />
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

function MemberRow({ canUpdateRoles, member }: { canUpdateRoles: boolean; member: Member }) {
  const mutation = useUpdateMemberRolesMutation();
  const initialRole = primaryRoleKey(member.role_keys);
  const isOwner = initialRole === "owner";
  const form = useForm({
    defaultValues: { roleKey: initialRole },
    onSubmit: async ({ value }) => {
      if (!canUpdateRoles) {
        toast.error("You don't have permission to change roles.");
        return;
      }
      if (isOwner) {
        toast.error("The organization owner's role can't be changed here.");
        return;
      }
      if (value.roleKey === initialRole) {
        toast.info("No role change to save.");
        return;
      }
      if (mutation.isPending) {
        toast.info("Still saving the last role change…");
        return;
      }
      try {
        await mutation.mutateAsync({
          roleKeys: [value.roleKey],
          userId: member.user_id,
        });
        toast.success("Role updated", {
          description: `${member.email} is now ${value.roleKey}.`,
        });
      } catch (error) {
        toast.error("Role update failed", {
          description: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });

  return (
    <TableRow>
      <TableCell className="align-middle">
        <div className="font-medium">{member.display_name || member.email}</div>
        <div className="break-all text-xs text-muted-foreground">{member.email}</div>
      </TableCell>
      <TableCell className="align-middle">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
          className="flex items-center gap-2"
        >
          <form.Field name="roleKey">
            {(field) => (
              <Select
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
                aria-label={`Role for ${member.email}`}
                aria-readonly={isOwner || undefined}
                className="flex-1"
              >
                {isOwner ? <option value="owner">Owner</option> : null}
                {ASSIGNABLE_ROLES.map((role) => (
                  <option key={role.role_key} value={role.role_key}>
                    {role.display_name}
                  </option>
                ))}
              </Select>
            )}
          </form.Field>

          <form.Subscribe selector={(state) => [state.isDirty, state.isSubmitting]}>
            {([isDirty, isSubmitting]) =>
              isDirty ? (
                <Button type="submit" size="sm" aria-busy={isSubmitting || mutation.isPending}>
                  {isSubmitting || mutation.isPending ? "Saving…" : "Save"}
                </Button>
              ) : null
            }
          </form.Subscribe>
        </form>
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

  // Capabilities are a set of keys; tanstack form models each key as its own
  // boolean field so isDirty light-up is per-field and we can submit the
  // reconstructed set in onSubmit.
  const initialEnabled = new Set(memberCapabilities.document.enabled_keys);
  const defaultValues = Object.fromEntries(
    memberCapabilities.catalog.map((capability) => [
      capability.key,
      initialEnabled.has(capability.key),
    ]),
  ) as Record<string, boolean>;

  const form = useForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      if (!canEditCapabilities) {
        toast.error("You don't have permission to edit member capabilities.");
        return;
      }
      if (mutation.isPending) {
        toast.info("Still saving capabilities…");
        return;
      }
      const enabledKeys = Object.entries(value)
        .filter(([, enabled]) => enabled)
        .map(([key]) => key)
        .sort();
      try {
        await mutation.mutateAsync({
          enabled_keys: enabledKeys,
          version: memberCapabilities.document.version,
        });
        toast.success("Capabilities updated");
      } catch (error) {
        toast.error("Capabilities save failed", {
          description: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });

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
          Your current role can view member capabilities but cannot save changes.
        </PermissionAlert>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
        className="space-y-4"
      >
        <div className="divide-y divide-border rounded-md border border-border">
          {memberCapabilities.catalog.map((capability) => {
            const switchId = `capability-${capability.key}`;
            return (
              <form.Field key={capability.key} name={capability.key}>
                {(field) => (
                  <div className="flex items-start justify-between gap-4 p-4">
                    <div className="space-y-1">
                      <Label htmlFor={switchId} className="text-sm font-medium">
                        {capability.label}
                      </Label>
                      <p className="text-sm text-muted-foreground">{capability.description}</p>
                    </div>
                    <Switch
                      id={switchId}
                      checked={Boolean(field.state.value)}
                      onCheckedChange={(next) => {
                        if (!canEditCapabilities) {
                          toast.error("You don't have permission to edit member capabilities.");
                          return;
                        }
                        field.handleChange(next);
                      }}
                      aria-label={capability.label}
                      aria-readonly={!canEditCapabilities || undefined}
                    />
                  </div>
                )}
              </form.Field>
            );
          })}
        </div>

        <form.Subscribe selector={(state) => [state.isDirty, state.isSubmitting]}>
          {([isDirty, isSubmitting]) =>
            isDirty ? (
              <div className="flex justify-end">
                <Button type="submit" aria-busy={isSubmitting || mutation.isPending}>
                  {isSubmitting || mutation.isPending ? "Saving…" : "Save capabilities"}
                </Button>
              </div>
            ) : null
          }
        </form.Subscribe>
      </form>
    </PageSection>
  );
}
