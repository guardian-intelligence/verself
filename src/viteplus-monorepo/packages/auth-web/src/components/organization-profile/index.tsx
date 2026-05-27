import { revalidateLogic, useForm, type AnyFieldMeta, type AnyFormApi } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useHydrated } from "@tanstack/react-router";
import { UserPlus } from "lucide-react";
import * as v from "valibot";
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
import { useSignedInAuth } from "../../react.ts";
import { useIAMApi } from "../iam-api.ts";
import { useInviteMemberMutation, useUpdateOrganizationMutation } from "../mutations.ts";
import { organizationMembersQuery, organizationQuery } from "../queries.ts";
import type { Member, Organization } from "../types.ts";
import { PermissionAlert } from "./error-alert.tsx";

const PERMISSION_ORGANIZATION_UPDATE = "iam:organization:update";
const PERMISSION_MEMBER_INVITE = "iam:member:invite";
const ACTIVE_MEMBER_STATE = "active";
const DEFAULT_INVITE_ROLE = "roles/member";
const INVITE_ROLES = [
  ["roles/member", "Member"],
  ["roles/admin", "Admin"],
  ["roles/executionViewer", "Execution viewer"],
  ["roles/billingViewer", "Billing viewer"],
  ["roles/sourceViewer", "Source viewer"],
  ["roles/secretsUser", "Secrets user"],
] as const;

const organizationDisplayNameSchema = v.pipe(
  v.string("Display name is required."),
  v.trim(),
  v.nonEmpty("Display name is required."),
  v.maxLength(120, "Display name must be 120 characters or fewer."),
);

const organizationSlugSchema = v.pipe(
  v.string("Slug is required."),
  v.trim(),
  v.nonEmpty("Slug is required."),
  v.regex(/^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/, "Use lowercase letters, numbers, and hyphens."),
);

const memberInviteEmailSchema = v.pipe(
  v.string("Email is required."),
  v.trim(),
  v.nonEmpty("Email is required."),
  v.email("Enter a valid email address."),
);

const memberInviteRoleSchema = v.pipe(
  v.string("Role is required."),
  v.nonEmpty("Role is required."),
);

const organizationProfileFormSchema = v.object({
  displayName: organizationDisplayNameSchema,
  slug: organizationSlugSchema,
});

const memberInviteFormSchema = v.object({
  email: memberInviteEmailSchema,
  role: memberInviteRoleSchema,
});

function formString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/-{2,}/g, "-")
    .slice(0, 80)
    .replace(/-+$/g, "");
}

function fieldErrorText(meta: AnyFieldMeta): string {
  const error = meta.errors.find((item): item is NonNullable<typeof item> => Boolean(item));
  if (!error) return "";
  if (!meta.isBlurred && !meta.errorMap.onSubmit) return "";
  if (Array.isArray(error)) {
    const first = error.find(Boolean);
    return first ? fieldValidationMessage(first) : "";
  }
  return fieldValidationMessage(error);
}

function fieldValidationMessage(error: unknown): string {
  if (Array.isArray(error)) {
    const first = error.find(Boolean);
    return first ? fieldValidationMessage(first) : "";
  }
  if (error instanceof Error) return error.message;
  if (
    error &&
    typeof error === "object" &&
    "message" in error &&
    typeof error.message === "string"
  ) {
    return error.message;
  }
  return String(error);
}

function fieldInvalid(meta: AnyFieldMeta): boolean {
  return Boolean(fieldErrorText(meta));
}

function authFormSubmitBusy(state: {
  readonly hydrated: boolean;
  readonly isSubmitting: boolean | undefined;
  readonly isValidating: boolean | undefined;
  readonly isPending?: boolean;
}): boolean {
  return (
    !state.hydrated ||
    Boolean(state.isSubmitting) ||
    Boolean(state.isValidating) ||
    Boolean(state.isPending)
  );
}

function authFormSubmit(form: AnyFormApi): void {
  if (form.state.isSubmitting || form.state.isValidating) {
    toast.info("Still working on the last request.");
    return;
  }
  void form.handleSubmit().catch((error: unknown) => {
    toast.error(fieldValidationMessage(error));
  });
}

function authFormSubmitInvalid({ formApi }: { readonly formApi: AnyFormApi }): void {
  toast.error(firstFormErrorText(formApi) || "Check the highlighted fields.");
}

function firstFormErrorText(form: AnyFormApi): string {
  for (const meta of Object.values(form.state.fieldMeta)) {
    const message = fieldValidationMessage(meta?.errors);
    if (message) return message;
  }
  return fieldValidationMessage(form.state.errors);
}

function FieldError({ id, meta }: { readonly id: string; readonly meta: AnyFieldMeta }) {
  const error = fieldErrorText(meta);
  return (
    <p
      id={id}
      role={error ? "alert" : undefined}
      className="min-h-5 text-xs font-medium leading-5 text-destructive"
    >
      {error}
    </p>
  );
}

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
  const canInviteMember = hasPermission(organization.permissions, PERMISSION_MEMBER_INVITE);

  const activeMembers = members.filter((member) => member.state === ACTIVE_MEMBER_STATE);

  return (
    <PageSections>
      <OrganizationSettingsSection
        canUpdateOrganization={canUpdateOrganization}
        key={organization.version}
        organization={organization}
      />
      <MembersSection canInviteMember={canInviteMember} members={activeMembers} />
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
  const hydrated = useHydrated();
  const mutation = useUpdateOrganizationMutation();
  const form = useForm({
    defaultValues: {
      displayName: formString(organization.display_name),
      slug: formString(organization.slug),
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: organizationProfileFormSchema,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      if (!canUpdateOrganization) {
        toast.error("You don't have permission to update the organization.");
        return;
      }
      if (mutation.isPending) {
        toast.info("Still syncing the last organization change.");
        return;
      }
      const displayName = formString(value.displayName).trim();
      const slug = slugify(formString(value.slug));
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
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          authFormSubmit(form);
        }}
        className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end"
      >
        <form.Field
          name="displayName"
          validators={{
            onBlur: organizationDisplayNameSchema,
            onChange: organizationDisplayNameSchema,
          }}
        >
          {(field) => {
            const errorId = `${field.name}-error`;
            return (
              <div className="space-y-1.5">
                <Label htmlFor={field.name}>Display name</Label>
                <Input
                  id={field.name}
                  aria-describedby={errorId}
                  aria-invalid={fieldInvalid(field.state.meta) || undefined}
                  value={formString(field.state.value)}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
                <FieldError id={errorId} meta={field.state.meta} />
              </div>
            );
          }}
        </form.Field>

        <form.Field
          name="slug"
          validators={{ onBlur: organizationSlugSchema, onChange: organizationSlugSchema }}
        >
          {(field) => {
            const errorId = `${field.name}-error`;
            return (
              <div className="space-y-1.5">
                <Label htmlFor={field.name}>Slug</Label>
                <Input
                  id={field.name}
                  aria-describedby={errorId}
                  aria-invalid={fieldInvalid(field.state.meta) || undefined}
                  value={formString(field.state.value)}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(slugify(event.target.value))}
                />
                <FieldError id={errorId} meta={field.state.meta} />
              </div>
            );
          }}
        </form.Field>

        <form.Subscribe selector={(state) => [state.isSubmitting, state.isValidating]}>
          {([isSubmitting, isValidating]) => (
            <Button
              type="submit"
              aria-busy={authFormSubmitBusy({
                hydrated,
                isSubmitting,
                isValidating,
                isPending: mutation.isPending,
              })}
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

function MembersSection({
  canInviteMember,
  members,
}: {
  canInviteMember: boolean;
  members: ReadonlyArray<Member>;
}) {
  const hydrated = useHydrated();
  const mutation = useInviteMemberMutation();
  const form = useForm({
    defaultValues: {
      email: "",
      role: DEFAULT_INVITE_ROLE,
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: memberInviteFormSchema,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      if (!canInviteMember) {
        toast.error("You don't have permission to invite members.");
        return;
      }
      if (mutation.isPending) {
        toast.info("Still sending the last invitation.");
        return;
      }
      const email = formString(value.email).trim().toLowerCase();
      const role = formString(value.role) || DEFAULT_INVITE_ROLE;
      try {
        await mutation.mutateAsync({ email, roles: [role] });
        form.reset();
        toast.success("Invitation sent");
      } catch (error) {
        toast.error("Invitation failed", {
          description: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });
  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Members</SectionTitle>
          <SectionDescription>People provisioned in the identity provider.</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>
      {canInviteMember ? (
        <form
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            authFormSubmit(form);
          }}
          className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(11rem,14rem)_auto] sm:items-end"
        >
          <form.Field
            name="email"
            validators={{ onBlur: memberInviteEmailSchema, onChange: memberInviteEmailSchema }}
          >
            {(field) => {
              const errorId = `${field.name}-error`;
              return (
                <div className="space-y-1.5">
                  <Label htmlFor={field.name}>Email</Label>
                  <Input
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    type="email"
                    value={formString(field.state.value)}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                  />
                  <FieldError id={errorId} meta={field.state.meta} />
                </div>
              );
            }}
          </form.Field>
          <form.Field
            name="role"
            validators={{ onBlur: memberInviteRoleSchema, onChange: memberInviteRoleSchema }}
          >
            {(field) => {
              const errorId = `${field.name}-error`;
              return (
                <div className="space-y-1.5">
                  <Label htmlFor={field.name}>Role</Label>
                  <Select
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    value={formString(field.state.value) || DEFAULT_INVITE_ROLE}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                  >
                    {INVITE_ROLES.map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </Select>
                  <FieldError id={errorId} meta={field.state.meta} />
                </div>
              );
            }}
          </form.Field>
          <form.Subscribe selector={(state) => [state.isSubmitting, state.isValidating]}>
            {([isSubmitting, isValidating]) => (
              <Button
                type="submit"
                aria-busy={authFormSubmitBusy({
                  hydrated,
                  isSubmitting,
                  isValidating,
                  isPending: mutation.isPending,
                })}
                className="sm:shrink-0"
              >
                <UserPlus aria-hidden="true" />
                <span>{isSubmitting || mutation.isPending ? "Sending..." : "Invite"}</span>
              </Button>
            )}
          </form.Subscribe>
        </form>
      ) : null}
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
