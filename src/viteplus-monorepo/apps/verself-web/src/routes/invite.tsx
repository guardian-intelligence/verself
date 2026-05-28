import { useForm } from "@tanstack/react-form";
import { createFileRoute, useHydrated, useNavigate } from "@tanstack/react-router";
import { CheckCircle2 } from "lucide-react";
import { iamErrorFromUnknown, isIamError } from "@verself/sdk/iam-errors";
import { Button } from "@verself/ui/components/ui/button";
import {
  authFormSubmit,
  authFormSubmitBusy,
  submitErrorText,
} from "~/features/auth/form-primitives";
import { iamErrorMessage } from "~/features/auth/iam-error-copy";
import { acceptMemberInvite } from "~/server-fns/auth";

function searchString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function safeOrgSlug(value: unknown): string | undefined {
  const slug = searchString(value);
  return slug && /^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/.test(slug) ? slug : undefined;
}

export const Route = createFileRoute("/invite")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: searchString(search.token),
    org: safeOrgSlug(search.org),
  }),
  component: InvitePage,
});

function InvitePage() {
  const { token, org } = Route.useSearch();
  return (
    <main className="grid min-h-svh place-items-center px-6 py-16">
      <section className="flex w-full max-w-sm flex-col">
        <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Accept invite</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Join{org ? ` ${org}` : " this organization"} with this verified invite.
        </p>
        {token ? (
          <InviteAcceptanceForm token={token} org={org} />
        ) : (
          <p className="mt-6 text-sm font-medium text-destructive">
            Invite could not be completed.
          </p>
        )}
      </section>
    </main>
  );
}

function InviteAcceptanceForm({
  token,
  org,
}: {
  readonly token: string;
  readonly org: string | undefined;
}) {
  const hydrated = useHydrated();
  const navigate = useNavigate();
  const form = useForm({
    defaultValues: {},
    onSubmit: async () => {
      const result = await acceptMemberInvite({ data: { token } }).catch(iamErrorFromUnknown);
      if (isIamError(result)) {
        throw new Error(iamErrorMessage(result));
      }
      await navigate({
        to: "/login",
        search: org ? { redirect: `/${org}` } : {},
      });
    },
  });

  return (
    <form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        authFormSubmit(form);
      }}
      className="mt-6 grid gap-4"
    >
      <form.Subscribe selector={(state) => [state.isSubmitting, state.errorMap.onSubmit]}>
        {([isSubmitting, submitError]) => (
          <div className="grid gap-3">
            <Button
              type="submit"
              aria-busy={authFormSubmitBusy({
                hydrated,
                isSubmitting,
                isValidating: false,
              })}
            >
              <CheckCircle2 aria-hidden="true" />
              <span>{isSubmitting ? "Accepting..." : "Accept invite"}</span>
            </Button>
            {submitError ? (
              <p className="text-sm font-medium text-destructive">{submitErrorText(submitError)}</p>
            ) : null}
          </div>
        )}
      </form.Subscribe>
    </form>
  );
}
