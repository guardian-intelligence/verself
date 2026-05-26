import { useForm } from "@tanstack/react-form";
import { createFileRoute, redirect } from "@tanstack/react-router";
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
import { createOrganization } from "~/server-fns/api";
import { selectActiveOrganization } from "~/server-fns/auth";
import { orgPath } from "~/features/shell/org-routing";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";

function formString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

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
  const form = useForm({
    defaultValues: {
      displayName: "",
      slug: "",
    },
    onSubmit: async ({ value }) => {
      const displayName = formString(value.displayName).trim();
      const slug = formString(value.slug).trim().toLowerCase();
      if (!displayName) {
        throw new Error("Organization name is required.");
      }
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
                void form.handleSubmit();
              }}
              className="grid gap-3"
            >
              <form.Field name="displayName">
                {(field) => (
                  <div className="space-y-1.5">
                    <Label htmlFor={field.name}>Name</Label>
                    <Input
                      id={field.name}
                      value={formString(field.state.value)}
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
                      value={formString(field.state.value)}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                    />
                  </div>
                )}
              </form.Field>
              <form.Subscribe selector={(state) => [state.isSubmitting, state.errorMap.onSubmit]}>
                {([isSubmitting, submitError]) => (
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                    <Button type="submit" aria-busy={isSubmitting} className="sm:w-fit">
                      <Building2 aria-hidden="true" />
                      <span>{isSubmitting ? "Creating..." : "Create organization"}</span>
                    </Button>
                    {submitError ? (
                      <p className="text-sm font-medium text-destructive">{String(submitError)}</p>
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
