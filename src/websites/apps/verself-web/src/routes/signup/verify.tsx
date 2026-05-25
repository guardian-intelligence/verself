import { useForm } from "@tanstack/react-form";
import { Link, createFileRoute } from "@tanstack/react-router";
import { Building2, CheckCircle2, MailPlus } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { cn } from "@verself/ui/lib/utils";
import { acceptMemberInvite, verifySignup } from "~/server-fns/auth";

type SignupMode = "create" | "join";

function searchString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function safeOrgSlug(value: unknown): string | undefined {
  const slug = searchString(value);
  return slug && /^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/.test(slug) ? slug : undefined;
}

function formString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function signupMode(value: unknown): SignupMode {
  return value === "join" ? "join" : "create";
}

export const Route = createFileRoute("/signup/verify")({
  validateSearch: (search: Record<string, unknown>) => ({
    signup_intent_id:
      searchString(search.signupIntentId) ??
      searchString(search.signup_intent_id) ??
      searchString(search.signup_intent),
    verification_token:
      searchString(search.verificationToken) ?? searchString(search.verification_token),
    organization_display_name:
      searchString(search.organizationDisplayName) ??
      searchString(search.organization_display_name),
    mode: search.mode === "join" ? "join" : undefined,
  }),
  component: SignupVerificationPage,
});

function SignupVerificationPage() {
  const search = Route.useSearch();
  const signupIntentId = search.signup_intent_id;
  const verificationToken = search.verification_token;
  const organizationDisplayName = search.organization_display_name;
  const mode = signupMode(search.mode);
  const form = useForm({
    defaultValues: {
      organizationDisplayName: organizationDisplayName ?? "",
      inviteLink: "",
    },
    onSubmit: async ({ value }) => {
      if (mode === "join") {
        const invite = inviteCredentialsFromInput(formString(value.inviteLink));
        await acceptMemberInvite({ data: { token: invite.token } });
        assignLogin(invite.org);
        return;
      }
      if (!signupIntentId || !verificationToken) {
        throw new Error("Signup link could not be completed.");
      }
      const displayName = formString(value.organizationDisplayName).trim();
      if (!displayName) {
        throw new Error("Organization name is required.");
      }
      const result = await verifySignup({
        data: {
          signupIntentId,
          verificationToken,
          organizationDisplayName: displayName,
        },
      });
      assignLogin(result.organization.slug);
    },
  });

  return (
    <main className="grid min-h-svh place-items-center px-6 py-16">
      <section className="flex w-full max-w-md flex-col">
        <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Done</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Choose where this account should land.
        </p>
        <div className="mt-6 grid grid-cols-2 rounded-md border border-border bg-muted p-1">
          <ModeLink
            active={mode === "create"}
            mode="create"
            signupIntentId={signupIntentId}
            verificationToken={verificationToken}
            organizationDisplayName={organizationDisplayName}
          >
            <Building2 aria-hidden="true" />
            <span>Create org</span>
          </ModeLink>
          <ModeLink
            active={mode === "join"}
            mode="join"
            signupIntentId={signupIntentId}
            verificationToken={verificationToken}
            organizationDisplayName={organizationDisplayName}
          >
            <MailPlus aria-hidden="true" />
            <span>Join org</span>
          </ModeLink>
        </div>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
          className="mt-6 grid gap-4"
        >
          {mode === "create" ? (
            <form.Field name="organizationDisplayName">
              {(field) => (
                <div className="space-y-1.5">
                  <Label htmlFor={field.name}>Organization name</Label>
                  <Input
                    id={field.name}
                    type="text"
                    autoComplete="organization"
                    value={formString(field.state.value)}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    disabled={!signupIntentId || !verificationToken}
                  />
                </div>
              )}
            </form.Field>
          ) : (
            <form.Field name="inviteLink">
              {(field) => (
                <div className="space-y-1.5">
                  <Label htmlFor={field.name}>Invite link</Label>
                  <Input
                    id={field.name}
                    type="text"
                    inputMode="url"
                    autoComplete="url"
                    value={formString(field.state.value)}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                  />
                </div>
              )}
            </form.Field>
          )}
          <form.Subscribe selector={(state) => [state.isSubmitting, state.errorMap.onSubmit]}>
            {([isSubmitting, submitError]) => {
              const missingSignupLink =
                mode === "create" && (!signupIntentId || !verificationToken);
              return (
                <div className="grid gap-3">
                  <Button
                    type="submit"
                    aria-busy={isSubmitting}
                    disabled={missingSignupLink || isSubmitting}
                  >
                    <CheckCircle2 aria-hidden="true" />
                    <span>{isSubmitting ? "Finishing..." : "Done"}</span>
                  </Button>
                  {missingSignupLink ? (
                    <p className="text-sm font-medium text-destructive">
                      Signup link could not be completed.
                    </p>
                  ) : submitError ? (
                    <p className="text-sm font-medium text-destructive">{String(submitError)}</p>
                  ) : null}
                </div>
              );
            }}
          </form.Subscribe>
        </form>
      </section>
    </main>
  );
}

function ModeLink(props: {
  active: boolean;
  mode: SignupMode;
  signupIntentId: string | undefined;
  verificationToken: string | undefined;
  organizationDisplayName: string | undefined;
  children: ReactNode;
}) {
  return (
    <Link
      to="/signup/verify"
      search={{
        mode: props.mode,
        signup_intent_id: props.signupIntentId,
        verification_token: props.verificationToken,
        organization_display_name: props.organizationDisplayName,
      }}
      className={cn(
        "flex h-9 items-center justify-center gap-2 rounded-sm px-3 text-sm font-medium text-muted-foreground transition-colors",
        props.active && "bg-background text-foreground shadow-xs",
      )}
      aria-current={props.active ? "page" : undefined}
    >
      {props.children}
    </Link>
  );
}

function inviteCredentialsFromInput(raw: string): { token: string; org?: string } {
  const value = raw.trim();
  if (!value) {
    throw new Error("Invite link is required.");
  }
  try {
    const url = new URL(value, window.location.origin);
    const token =
      url.searchParams.get("token") ??
      url.searchParams.get("acceptance_token") ??
      url.searchParams.get("acceptanceToken");
    if (token?.trim()) {
      const org = safeOrgSlug(url.searchParams.get("org"));
      return org ? { token: token.trim(), org } : { token: token.trim() };
    }
  } catch {
    return { token: value };
  }
  return { token: value };
}

function assignLogin(orgSlug: string | undefined): void {
  const login = new URL("/login", window.location.origin);
  login.searchParams.set("prompt", "login");
  if (orgSlug) {
    login.searchParams.set("redirect", `/${orgSlug}`);
  }
  window.location.assign(`${login.pathname}${login.search}`);
}
