import { useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import { Link, createFileRoute } from "@tanstack/react-router";
import { Building2, CheckCircle2, MailPlus } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { cn } from "@verself/ui/lib/utils";
import { acceptMemberInvite, checkOrganizationSlug, verifySignup } from "~/server-fns/auth";

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
    organization_slug:
      safeOrgSlug(search.organizationSlug) ?? safeOrgSlug(search.organization_slug),
    mode: search.mode === "join" ? "join" : undefined,
  }),
  component: SignupVerificationPage,
});

function SignupVerificationPage() {
  const search = Route.useSearch();
  const signupIntentId = search.signup_intent_id;
  const verificationToken = search.verification_token;
  const organizationDisplayName = search.organization_display_name;
  const organizationSlug = search.organization_slug;
  const mode = signupMode(search.mode);
  const slugCheck = useMutation({
    mutationFn: (slug: string) => checkOrganizationSlug({ data: { slug } }),
  });
  const form = useForm({
    defaultValues: {
      organizationDisplayName: organizationDisplayName ?? "",
      organizationSlug: organizationSlug ?? slugify(organizationDisplayName ?? ""),
      inviteLink: "",
    },
    onSubmit: async ({ value }) => {
      if (mode === "join") {
        const invite = inviteCredentialsFromInput(formString(value.inviteLink));
        const result = await acceptMemberInvite({ data: { token: invite.token } });
        assignLogin(result.loginIntent?.loginUrl ?? result.loginUrl, invite.org);
        return;
      }
      if (!signupIntentId || !verificationToken) {
        throw new Error("Signup link could not be completed.");
      }
      const displayName = formString(value.organizationDisplayName).trim();
      if (!displayName) {
        throw new Error("Organization name is required.");
      }
      const slug = slugify(formString(value.organizationSlug));
      if (!slug) {
        throw new Error("Organization URL is required.");
      }
      const availability = await checkOrganizationSlug({ data: { slug } });
      if (!availability.available) {
        throw new Error("That organization URL is already taken.");
      }
      const result = await verifySignup({
        data: {
          signupIntentId,
          verificationToken,
          organizationDisplayName: displayName,
          organizationSlug: slug,
        },
      });
      assignLogin(result.loginIntent?.loginUrl ?? result.loginUrl, result.organization.slug);
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
          >
            <Building2 aria-hidden="true" />
            <span>Create org</span>
          </ModeLink>
          <ModeLink
            active={mode === "join"}
            mode="join"
            signupIntentId={signupIntentId}
            verificationToken={verificationToken}
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
            <>
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
              <form.Field name="organizationSlug">
                {(field) => {
                  const slug = slugify(formString(field.state.value));
                  const checked = slugCheck.data?.slug === slug ? slugCheck.data : undefined;
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Organization URL</Label>
                      <div className="flex gap-2">
                        <div className="relative min-w-0 flex-1">
                          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
                            verself.sh/
                          </span>
                          <Input
                            id={field.name}
                            type="text"
                            autoComplete="off"
                            value={formString(field.state.value)}
                            onBlur={field.handleBlur}
                            onChange={(event) => field.handleChange(slugify(event.target.value))}
                            className="pl-[5.75rem]"
                            disabled={!signupIntentId || !verificationToken}
                          />
                        </div>
                        <Button
                          type="button"
                          variant="secondary"
                          disabled={!slug || slugCheck.isPending}
                          aria-busy={slugCheck.isPending}
                          onClick={() => slugCheck.mutate(slug)}
                        >
                          {slugCheck.isPending ? "Checking..." : "Check"}
                        </Button>
                      </div>
                      <p className="text-xs leading-5 text-muted-foreground">
                        You can change this later.
                      </p>
                      {checked ? (
                        <p
                          className={cn(
                            "text-sm font-medium",
                            checked.available ? "text-emerald-700" : "text-destructive",
                          )}
                        >
                          {checked.available
                            ? "This organization URL is available."
                            : "That organization URL is already taken."}
                        </p>
                      ) : slugCheck.error ? (
                        <p className="text-sm font-medium text-destructive">
                          Could not check that organization URL.
                        </p>
                      ) : null}
                    </div>
                  );
                }}
              </form.Field>
            </>
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
  children: ReactNode;
}) {
  return (
    <Link
      to="/signup/verify"
      search={() => modeLinkSearch(props)}
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

function modeLinkSearch(props: {
  mode: SignupMode;
  signupIntentId: string | undefined;
  verificationToken: string | undefined;
}) {
  return {
    signup_intent_id: props.signupIntentId,
    verification_token: props.verificationToken,
    organization_display_name: undefined,
    organization_slug: undefined,
    mode: props.mode === "join" ? ("join" as const) : undefined,
  };
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

function assignLogin(loginURLOrOrgSlug: string | undefined, fallbackOrgSlug?: string): void {
  if (loginURLOrOrgSlug?.startsWith("http") || loginURLOrOrgSlug?.startsWith("/login")) {
    const login = new URL(loginURLOrOrgSlug, window.location.origin);
    window.location.assign(`${login.pathname}${login.search}`);
    return;
  }
  const orgSlug = fallbackOrgSlug ?? loginURLOrOrgSlug;
  const login = new URL("/login", window.location.origin);
  login.searchParams.set("prompt", "login");
  if (orgSlug) {
    login.searchParams.set("redirect", `/${orgSlug}`);
  }
  window.location.assign(`${login.pathname}${login.search}`);
}
