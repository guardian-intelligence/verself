import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Callout } from "~/components/callout";
import { ErrorCallout } from "~/components/error-callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import type { Member, Operations, PolicyDocument, PolicyRole } from "~/server-fns/api";
import {
  useInviteMemberMutation,
  usePutPolicyMutation,
  useUpdateMemberRolesMutation,
} from "./mutations";
import {
  organizationMembersQuery,
  organizationOperationsQuery,
  organizationPolicyQuery,
  organizationQuery,
} from "./queries";

const permissionMemberInvite = "identity:member:invite";
const permissionMemberRolesWrite = "identity:member:roles:write";
const permissionPolicyWrite = "identity:policy:write";
const identityOrgAdminRole = "identity_org_admin";

function hasPermission(permissions: Array<string>, permission: string) {
  return permissions.includes(permission);
}

function defaultRoleKeys(policy: PolicyDocument) {
  const member = policy.roles.find((role) => role.role_key === "identity_org_member");
  return [member?.role_key ?? policy.roles[0]?.role_key].filter((roleKey): roleKey is string =>
    Boolean(roleKey),
  );
}

function roleLabel(policy: PolicyDocument, roleKey: string) {
  return policy.roles.find((role) => role.role_key === roleKey)?.display_name ?? roleKey;
}

function permissionSet(permissions: Array<string>) {
  return new Set(permissions);
}

export function OrganizationWidget() {
  const auth = useSignedInAuth();
  const organization = useSuspenseQuery(organizationQuery(auth)).data;
  const members = useSuspenseQuery(organizationMembersQuery(auth)).data;
  const operations = useSuspenseQuery(organizationOperationsQuery(auth)).data;
  const policy = useSuspenseQuery(organizationPolicyQuery(auth)).data;

  const canInvite = hasPermission(organization.permissions, permissionMemberInvite);
  const canUpdateRoles = hasPermission(organization.permissions, permissionMemberRolesWrite);
  const canWritePolicy = hasPermission(organization.permissions, permissionPolicyWrite);

  return (
    <div className="space-y-8">
      <section className="space-y-2">
        <div>
          <p className="text-sm text-muted-foreground">Organization</p>
          <h1 className="break-words text-2xl font-bold">{organization.name}</h1>
        </div>
        <div className="grid gap-3 text-sm md:grid-cols-3">
          <OrganizationMetric label="Org ID" value={organization.org_id} />
          <OrganizationMetric label="Signed in as" value={organization.caller.email} />
          <OrganizationMetric
            label="Role"
            value={
              organization.caller.role_keys.length > 0
                ? organization.caller.role_keys
                    .map((roleKey) => roleLabel(policy, roleKey))
                    .join(", ")
                : "No role"
            }
          />
        </div>
      </section>

      <InviteMemberForm canInvite={canInvite} policy={policy} />
      <MembersTable canUpdateRoles={canUpdateRoles} members={members} policy={policy} />
      <PolicyEditor
        canWritePolicy={canWritePolicy}
        key={policy.version}
        operations={operations}
        policy={policy}
      />
    </div>
  );
}

function OrganizationMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 break-words font-medium">{value}</div>
    </div>
  );
}

function InviteMemberForm({ canInvite, policy }: { canInvite: boolean; policy: PolicyDocument }) {
  const inviteMutation = useInviteMemberMutation();
  const roles = policy.roles;
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
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Invite Member</h2>
        <p className="text-sm text-muted-foreground">New members receive a Zitadel email code.</p>
      </div>

      {!canInvite ? (
        <Callout title="Invite permission required">
          Your current role can view members but cannot invite users.
        </Callout>
      ) : null}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void form.handleSubmit();
        }}
        className="grid gap-4 lg:grid-cols-[1fr_1fr]"
      >
        <div className="space-y-4">
          <form.Field
            name="email"
            validators={{
              onChange: ({ value }) => (!value.trim() ? "Email is required" : undefined),
            }}
          >
            {(field) => (
              <div>
                <label htmlFor={field.name} className="text-sm font-medium">
                  Email
                </label>
                <input
                  id={field.name}
                  type="email"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  disabled={!canInvite}
                />
                {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
                  <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
                ) : null}
              </div>
            )}
          </form.Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <form.Field name="givenName">
              {(field) => (
                <div>
                  <label htmlFor={field.name} className="text-sm font-medium">
                    Given name
                  </label>
                  <input
                    id={field.name}
                    type="text"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                    disabled={!canInvite}
                  />
                </div>
              )}
            </form.Field>
            <form.Field name="familyName">
              {(field) => (
                <div>
                  <label htmlFor={field.name} className="text-sm font-medium">
                    Family name
                  </label>
                  <input
                    id={field.name}
                    type="text"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
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
            onChange: ({ value }) => (value.length === 0 ? "Select at least one role" : undefined),
          }}
        >
          {(field) => (
            <RoleCheckboxes
              disabled={!canInvite}
              error={field.state.meta.isTouched ? field.state.meta.errors[0] : undefined}
              onChange={field.handleChange}
              roles={roles}
              value={field.state.value}
            />
          )}
        </form.Field>

        <div className="space-y-3 lg:col-span-2">
          {inviteMutation.error ? (
            <ErrorCallout error={inviteMutation.error} title="Invite failed" />
          ) : null}

          <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
            {([canSubmit, isSubmitting]) => (
              <button
                type="submit"
                disabled={!canInvite || !canSubmit || isSubmitting || inviteMutation.isPending}
                className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
              >
                {isSubmitting || inviteMutation.isPending ? "Inviting..." : "Invite Member"}
              </button>
            )}
          </form.Subscribe>
        </div>
      </form>
    </section>
  );
}

function RoleCheckboxes({
  disabled,
  error,
  onChange,
  roles,
  value,
}: {
  disabled?: boolean;
  error?: unknown;
  onChange: (value: Array<string>) => void;
  roles: Array<PolicyRole>;
  value: Array<string>;
}) {
  const selected = new Set(value);

  return (
    <fieldset className="space-y-3">
      <legend className="text-sm font-medium">Roles</legend>
      <div className="grid gap-2">
        {roles.map((role) => (
          <label
            key={role.role_key}
            className="flex min-h-12 items-start gap-3 rounded-md border border-border px-3 py-2 text-sm"
          >
            <input
              type="checkbox"
              className="mt-1"
              checked={selected.has(role.role_key)}
              disabled={disabled}
              onChange={(event) => {
                const next = event.target.checked
                  ? Array.from(new Set([...value, role.role_key]))
                  : value.filter((roleKey) => roleKey !== role.role_key);
                onChange(next);
              }}
            />
            <span>
              <span className="block font-medium">{role.display_name}</span>
              <code className="break-all text-xs text-muted-foreground">{role.role_key}</code>
            </span>
          </label>
        ))}
      </div>
      {error ? <p className="text-sm text-destructive">{String(error)}</p> : null}
    </fieldset>
  );
}

function MembersTable({
  canUpdateRoles,
  members,
  policy,
}: {
  canUpdateRoles: boolean;
  members: Array<Member>;
  policy: PolicyDocument;
}) {
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Members</h2>
        <p className="text-sm text-muted-foreground">Role assignments are written to Zitadel.</p>
      </div>

      <div className="overflow-hidden rounded-lg border border-border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="px-4 py-2 text-left font-medium">Member</th>
              <th className="px-4 py-2 text-left font-medium">State</th>
              <th className="px-4 py-2 text-left font-medium">Roles</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
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
              <TableEmptyRow
                colSpan={3}
                title="No members"
                description="Invited users appear here after Zitadel accepts the request."
              />
            )}
          </tbody>
        </table>
      </div>
    </section>
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
      roleKeys: member.role_keys,
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        roleKeys: value.roleKeys,
        userId: member.user_id,
      });
    },
  });

  return (
    <tr>
      <td className="px-4 py-3 align-top">
        <div className="font-medium">{member.display_name || member.email}</div>
        <div className="break-all text-xs text-muted-foreground">{member.email}</div>
      </td>
      <td className="px-4 py-3 align-top text-muted-foreground">{member.state}</td>
      <td className="px-4 py-3 align-top">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            void form.handleSubmit();
          }}
          className="space-y-2"
        >
          <form.Field
            name="roleKeys"
            validators={{
              onChange: ({ value }) =>
                value.length === 0 ? "Select at least one role" : undefined,
            }}
          >
            {(field) => (
              <RoleCheckboxes
                disabled={!canUpdateRoles}
                error={field.state.meta.isTouched ? field.state.meta.errors[0] : undefined}
                onChange={field.handleChange}
                roles={policy.roles}
                value={field.state.value}
              />
            )}
          </form.Field>

          {mutation.error ? (
            <ErrorCallout error={mutation.error} title="Role update failed" />
          ) : null}

          <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
            {([canSubmit, isSubmitting]) => (
              <button
                type="submit"
                disabled={!canUpdateRoles || !canSubmit || isSubmitting || mutation.isPending}
                className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent disabled:opacity-50"
              >
                {isSubmitting || mutation.isPending ? "Saving..." : "Save Roles"}
              </button>
            )}
          </form.Subscribe>
        </form>
      </td>
    </tr>
  );
}

function PolicyEditor({
  canWritePolicy,
  operations,
  policy,
}: {
  canWritePolicy: boolean;
  operations: Operations;
  policy: PolicyDocument;
}) {
  const mutation = usePutPolicyMutation();
  const flattenedOperations = operations.services.flatMap((service) =>
    service.operations.map((operation) => ({ service: service.service, operation })),
  );
  const form = useForm({
    defaultValues: {
      roles: policy.roles,
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        roles: value.roles,
        version: policy.version,
      });
    },
  });

  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Policy</h2>
        <p className="text-sm text-muted-foreground">
          Service operations are the allow-list for permission documents.
        </p>
      </div>

      {!canWritePolicy ? (
        <Callout title="Policy write permission required">
          Your current role can view the policy but cannot save changes.
        </Callout>
      ) : null}

      <form
        key={policy.version}
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          void form.handleSubmit();
        }}
        className="space-y-4"
      >
        <form.Field name="roles">
          {(field) => (
            <div className="overflow-hidden rounded-lg border border-border">
              <table className="w-full text-sm">
                <thead className="bg-muted/50">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Permission</th>
                    {field.state.value.map((role) => (
                      <th key={role.role_key} className="px-4 py-2 text-left font-medium">
                        {role.display_name}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {flattenedOperations.map(({ operation, service }) => (
                    <tr key={operation.permission}>
                      <td className="px-4 py-3 align-top">
                        <div className="font-medium">{operation.resource}</div>
                        <div className="break-all text-xs text-muted-foreground">
                          {service} / {operation.action} / {operation.permission}
                        </div>
                      </td>
                      {field.state.value.map((role, roleIndex) => {
                        const isAdminRole = role.role_key === identityOrgAdminRole;
                        const selectedPermissions = permissionSet(role.permissions);
                        return (
                          <td key={role.role_key} className="px-4 py-3 align-top">
                            <input
                              aria-label={`${role.display_name}: ${operation.permission}`}
                              type="checkbox"
                              checked={isAdminRole || selectedPermissions.has(operation.permission)}
                              disabled={!canWritePolicy || isAdminRole}
                              onChange={(event) => {
                                const nextRoles = field.state.value.map((nextRole, index) => {
                                  if (index !== roleIndex) return nextRole;
                                  const nextPermissions = event.target.checked
                                    ? Array.from(
                                        new Set([...nextRole.permissions, operation.permission]),
                                      ).sort()
                                    : nextRole.permissions.filter(
                                        (permission) => permission !== operation.permission,
                                      );
                                  return { ...nextRole, permissions: nextPermissions };
                                });
                                field.handleChange(nextRoles);
                              }}
                            />
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </form.Field>

        {mutation.error ? <ErrorCallout error={mutation.error} title="Policy save failed" /> : null}

        <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
          {([canSubmit, isSubmitting]) => (
            <button
              type="submit"
              disabled={!canWritePolicy || !canSubmit || isSubmitting || mutation.isPending}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {isSubmitting || mutation.isPending ? "Saving..." : "Save Policy"}
            </button>
          )}
        </form.Subscribe>
      </form>
    </section>
  );
}
