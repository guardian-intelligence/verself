import { revalidateLogic, useForm } from "@tanstack/react-form";
import { createFileRoute, redirect, useHydrated } from "@tanstack/react-router";
import { Building2 } from "lucide-react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import {
  FieldError,
  authFormSubmit,
  authFormSubmitBusy,
  authFormSubmitInvalid,
  fieldInvalid,
  submitErrorText,
} from "~/features/auth/form-primitives";
import {
  formString,
  normalizeHumanText,
  onboardingOrganizationFormSchema,
  optionalOrganizationSlugSchema,
  organizationNameSchema,
  slugify,
} from "~/features/auth/form-schemas";
import { createOrganization } from "~/server-fns/api";
import { selectActiveOrganization } from "~/server-fns/auth";
import { orgPath } from "~/features/shell/org-routing";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";

export const Route = createFileRoute("/_shell/_authenticated/onboarding")({
  beforeLoad: async ({ context }) => {
    const target = await resolveDefaultSignedInPath(context.queryClient, context.auth);
    if (target !== "/onboarding") {
      throw redirect({ href: target, replace: true });
    }
  },
  component: OnboardingPage,
});

function OnboardingPage() {
  const hydrated = useHydrated();
  const form = useForm({
    defaultValues: {
      displayName: "",
      slug: "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: onboardingOrganizationFormSchema,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      const displayName = normalizeHumanText(value.displayName);
      const slug = slugify(formString(value.slug));
      const organization = await createOrganization({
        data: {
          display_name: displayName,
          ...(slug ? { slug } : {}),
        },
      });
      await selectActiveOrganization({ data: { orgID: organization.org_id } });
      window.location.assign(orgPath(organization.slug, "/"));
    },
  });

  return (
    <main className="min-h-svh px-6 py-16">
      <Page variant="narrow" className="mx-auto">
        <PageHeader>
          <PageHeaderContent>
            <PageTitle>Create an organization</PageTitle>
            <PageDescription>Signed-in setup for the Verself console.</PageDescription>
          </PageHeaderContent>
        </PageHeader>
        <PageSections>
          <PageSection>
            <SectionHeader>
              <SectionHeaderContent>
                <SectionTitle>Organization</SectionTitle>
                <SectionDescription>Names used across the console and API.</SectionDescription>
              </SectionHeaderContent>
            </SectionHeader>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                event.stopPropagation();
                authFormSubmit(form);
              }}
              className="grid gap-3"
            >
              <form.Field
                name="displayName"
                validators={{ onBlur: organizationNameSchema, onChange: organizationNameSchema }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Name</Label>
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
                validators={{
                  onBlur: optionalOrganizationSlugSchema,
                  onChange: optionalOrganizationSlugSchema,
                }}
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
              <form.Subscribe
                selector={(state) => [
                  state.isSubmitting,
                  state.isValidating,
                  state.errorMap.onSubmit,
                ]}
              >
                {([isSubmitting, isValidating, submitError]) => (
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                    <Button
                      type="submit"
                      aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
                      className="sm:w-fit"
                    >
                      <Building2 aria-hidden="true" />
                      <span>{isSubmitting ? "Creating..." : "Create organization"}</span>
                    </Button>
                    {submitError ? (
                      <p className="text-sm font-medium text-destructive">
                        {submitErrorText(submitError)}
                      </p>
                    ) : null}
                  </div>
                )}
              </form.Subscribe>
            </form>
          </PageSection>
        </PageSections>
      </Page>
    </main>
  );
}
