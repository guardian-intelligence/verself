import { useMemo, useReducer } from "react";
import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@forge-metal/ui/components/ui/card";
import { Input } from "@forge-metal/ui/components/ui/input";
import { Label } from "@forge-metal/ui/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { useSignedInAuth } from "../../react.ts";
import { useIdentityApi } from "../identity-api.ts";
import {
  useInviteMemberMutation,
  usePutPolicyMutation,
  useUpdateMemberRolesMutation,
} from "../mutations.ts";
import {
  organizationMembersQuery,
  organizationOperationsQuery,
  organizationPolicyQuery,
  organizationQuery,
} from "../queries.ts";
import type { Member, Operations, PolicyDocument } from "../types.ts";
import { ErrorAlert, formErrorText, PermissionAlert } from "./error-alert.tsx";
import {
  buildCatalogTree,
  PolicyMatrix,
  policyFormEqual,
  policyFormFromDocument,
  policyFormToRoles,
  policyReducer,
  type PolicyFormState,
} from "./policy/index.ts";
import { RoleCheckboxes } from "./role-checkboxes.tsx";

const PERMISSION_MEMBER_INVITE = "identity:member:invite";
const PERMISSION_MEMBER_ROLES_WRITE = "identity:member:roles:write";
const PERMISSION_POLICY_WRITE = "identity:policy:write";

function hasPermission(permissions: ReadonlyArray<string>, permission: string): boolean {
  return permissions.includes(permission);
}

function defaultRoleKeys(policy: PolicyDocument): Array<string> {
  const member = policy.roles.find((role) => role.role_key === "identity_org_member");
  const fallback = member ?? policy.roles[0];
  return fallback ? [fallback.role_key] : [];
}

function roleLabel(policy: PolicyDocument, roleKey: string): string {
  return policy.roles.find((role) => role.role_key === roleKey)?.display_name ?? roleKey;
}

export interface OrganizationProfileProps {
  /** Optional override for the heading shown above the org name. */
  readonly heading?: string;
}

export function OrganizationProfile(_props: OrganizationProfileProps = {}) {
  const auth = useSignedInAuth();
  const api = useIdentityApi();
  const organization = useSuspenseQuery(organizationQuery(auth, api)).data;
  const members = useSuspenseQuery(organizationMembersQuery(auth, api)).data;
  const operations = useSuspenseQuery(organizationOperationsQuery(auth, api)).data;
  const policy = useSuspenseQuery(organizationPolicyQuery(auth, api)).data;

  const canInvite = hasPermission(organization.permissions, PERMISSION_MEMBER_INVITE);
  const canUpdateRoles = hasPermission(organization.permissions, PERMISSION_MEMBER_ROLES_WRITE);
  const canWritePolicy = hasPermission(organization.permissions, PERMISSION_POLICY_WRITE);

  return (
    <div className="space-y-6">
      <GeneralSection organization={organization} policy={policy} />
      <InviteMemberSection canInvite={canInvite} policy={policy} />
      <MembersSection canUpdateRoles={canUpdateRoles} members={[...members]} policy={policy} />
      <PolicySection
        canWritePolicy={canWritePolicy}
        operations={operations}
        // Remount the editor whenever the server hands us a fresh policy
        // version (after save → invalidate → refetch). This is the React-
        // idiomatic way to reset reducer state on prop changes — see
        // https://react.dev/reference/react/useState#resetting-state-with-a-key.
        key={policy.version}
        policy={policy}
      />
    </div>
  );
}

interface GeneralSectionProps {
  readonly organization: {
    readonly org_id: string;
    readonly name: string;
    readonly caller: Member;
  };
  readonly policy: PolicyDocument;
}

function GeneralSection({ organization, policy }: GeneralSectionProps) {
  const callerRoles = organization.caller.role_keys;
  return (
    <Card>
      <CardHeader>
        <CardDescription>Organization</CardDescription>
        <CardTitle role="heading" aria-level={1} className="break-words text-2xl">
          {organization.name}
        </CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 md:grid-cols-3">
        <Metric label="Org ID" value={<code className="break-all">{organization.org_id}</code>} />
        <Metric label="Signed in as" value={organization.caller.email} />
        <Metric
          label="Your roles"
          value={
            callerRoles.length > 0 ? (
              <div className="flex flex-wrap gap-1">
                {callerRoles.map((roleKey) => (
                  <Badge key={roleKey} variant="secondary">
                    {roleLabel(policy, roleKey)}
                  </Badge>
                ))}
              </div>
            ) : (
              <span className="text-muted-foreground">No role</span>
            )
          }
        />
      </CardContent>
    </Card>
  );
}

function Metric({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-md border border-border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 break-words text-sm font-medium">{value}</div>
    </div>
  );
}

function InviteMemberSection({
  canInvite,
  policy,
}: {
  canInvite: boolean;
  policy: PolicyDocument;
}) {
  const inviteMutation = useInviteMemberMutation();
  const form = useForm({
    defaultValues: {
      email: "",
      familyName: "",
      givenName: "",
      roleKeys: defaultRoleKeys(policy),
    },
    onSubmit: async ({ value }) => {
      await inviteMutation.mutateAsync(value);
      form.reset();
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle role="heading" aria-level={2}>
          Invite member
        </CardTitle>
        <CardDescription>New members receive a Zitadel email code.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!canInvite ? (
          <PermissionAlert title="Invite permission required">
            Your current role can view members but cannot invite users.
          </PermissionAlert>
        ) : null}

        <form
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
          className="grid gap-4 lg:grid-cols-[1fr_1fr]"
        >
          <div className="space-y-4">
            <form.Field
              name="email"
              validators={{
                onChange: ({ value }: { value: string }) =>
                  !value.trim() ? "Email is required" : undefined,
              }}
            >
              {(field) => (
                <div className="space-y-1">
                  <Label htmlFor={field.name}>Email</Label>
                  <Input
                    id={field.name}
                    type="email"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    disabled={!canInvite}
                  />
                  {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
                    <p className="text-sm text-destructive">
                      {formErrorText(field.state.meta.errors[0])}
                    </p>
                  ) : null}
                </div>
              )}
            </form.Field>

            <div className="grid gap-4 sm:grid-cols-2">
              <form.Field name="givenName">
                {(field) => (
                  <div className="space-y-1">
                    <Label htmlFor={field.name}>Given name</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      disabled={!canInvite}
                    />
                  </div>
                )}
              </form.Field>
              <form.Field name="familyName">
                {(field) => (
                  <div className="space-y-1">
                    <Label htmlFor={field.name}>Family name</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      disabled={!canInvite}
                    />
                  </div>
                )}
              </form.Field>
            </div>
          </div>

          <form.Field
            name="roleKeys"
            validators={{
              onChange: ({ value }: { value: ReadonlyArray<string> }) =>
                value.length === 0 ? "Select at least one role" : undefined,
            }}
          >
            {(field) => (
              <RoleCheckboxes
                disabled={!canInvite}
                error={
                  field.state.meta.isTouched ? formErrorText(field.state.meta.errors[0]) : undefined
                }
                onChange={field.handleChange}
                roles={policy.roles}
                value={field.state.value}
                legend="Invite roles"
              />
            )}
          </form.Field>

          <div className="space-y-3 lg:col-span-2">
            {inviteMutation.error ? (
              <ErrorAlert error={inviteMutation.error} title="Invite failed" />
            ) : null}

            <div className="flex justify-end">
              <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
                {([canSubmit, isSubmitting]) => (
                  <Button
                    type="submit"
                    disabled={!canInvite || !canSubmit || isSubmitting || inviteMutation.isPending}
                  >
                    {isSubmitting || inviteMutation.isPending ? "Inviting…" : "Invite member"}
                  </Button>
                )}
              </form.Subscribe>
            </div>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function MembersSection({
  canUpdateRoles,
  members,
  policy,
}: {
  canUpdateRoles: boolean;
  members: Array<Member>;
  policy: PolicyDocument;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle role="heading" aria-level={2}>
          Members
        </CardTitle>
        <CardDescription>Role assignments are written to Zitadel.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Member</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Roles</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.length > 0 ? (
              members.map((member) => (
                <MemberRow
                  canUpdateRoles={canUpdateRoles}
                  key={`${member.user_id}:${member.role_keys.join(",")}`}
                  member={member}
                  policy={policy}
                />
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={3} className="py-8 text-center align-middle">
                  <p className="font-medium">No members</p>
                  <p className="text-sm text-muted-foreground">
                    Invited users appear here after Zitadel accepts the request.
                  </p>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function MemberRow({
  canUpdateRoles,
  member,
  policy,
}: {
  canUpdateRoles: boolean;
  member: Member;
  policy: PolicyDocument;
}) {
  const mutation = useUpdateMemberRolesMutation();
  const form = useForm({
    defaultValues: {
      roleKeys: [...member.role_keys],
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        roleKeys: value.roleKeys,
        userId: member.user_id,
      });
    },
  });

  return (
    <TableRow>
      <TableCell className="align-top">
        <div className="font-medium">{member.display_name || member.email}</div>
        <div className="break-all text-xs text-muted-foreground">{member.email}</div>
      </TableCell>
      <TableCell className="align-top text-muted-foreground">{member.state}</TableCell>
      <TableCell className="align-top">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
          className="space-y-2"
        >
          <form.Field
            name="roleKeys"
            validators={{
              onChange: ({ value }: { value: ReadonlyArray<string> }) =>
                value.length === 0 ? "Select at least one role" : undefined,
            }}
          >
            {(field) => (
              <RoleCheckboxes
                disabled={!canUpdateRoles}
                error={
                  field.state.meta.isTouched ? formErrorText(field.state.meta.errors[0]) : undefined
                }
                onChange={field.handleChange}
                roles={policy.roles}
                value={field.state.value}
                legend={`Roles for ${member.email}`}
              />
            )}
          </form.Field>

          {mutation.error ? <ErrorAlert error={mutation.error} title="Role update failed" /> : null}

          <div className="flex justify-end">
            <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
              {([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  variant="outline"
                  size="sm"
                  disabled={!canUpdateRoles || !canSubmit || isSubmitting || mutation.isPending}
                >
                  {isSubmitting || mutation.isPending ? "Saving…" : "Save roles"}
                </Button>
              )}
            </form.Subscribe>
          </div>
        </form>
      </TableCell>
    </TableRow>
  );
}

function PolicySection({
  canWritePolicy,
  operations,
  policy,
}: {
  canWritePolicy: boolean;
  operations: Operations;
  policy: PolicyDocument;
}) {
  const mutation = usePutPolicyMutation();
  const catalog = useMemo(() => buildCatalogTree(operations), [operations]);
  const initialState = useMemo<PolicyFormState>(() => policyFormFromDocument(policy), [policy]);
  const [state, dispatch] = useReducer(policyReducer, initialState);
  const isDirty = !policyFormEqual(state, initialState);

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    event.stopPropagation();
    void mutation.mutateAsync({
      roles: policyFormToRoles(state),
      version: state.version,
    });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle role="heading" aria-level={2}>
          Policy
        </CardTitle>
        <CardDescription>
          Service operations are grouped by namespace. Toggling a group sets every permission
          underneath it.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!canWritePolicy ? (
          <PermissionAlert title="Policy write permission required">
            Your current role can view the policy but cannot save changes.
          </PermissionAlert>
        ) : null}

        <form onSubmit={handleSubmit} className="space-y-4">
          <PolicyMatrix
            catalog={catalog}
            state={state}
            dispatch={dispatch}
            canEdit={canWritePolicy}
          />

          {mutation.error ? <ErrorAlert error={mutation.error} title="Policy save failed" /> : null}

          <div className="flex justify-end">
            <Button type="submit" disabled={!canWritePolicy || !isDirty || mutation.isPending}>
              {mutation.isPending ? "Saving…" : "Save policy"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
