import { useForm } from "@tanstack/react-form";
import { createFileRoute } from "@tanstack/react-router";
import { KeyRound } from "lucide-react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { acceptMemberInvite } from "~/server-fns/auth";

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

export const Route = createFileRoute("/invite")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: searchString(search.token),
    org: safeOrgSlug(search.org),
  }),
  component: InvitePage,
});

function InvitePage() {
  const { token, org } = Route.useSearch();
  const form = useForm({
    defaultValues: {
      password: "",
      confirmPassword: "",
    },
    onSubmit: async ({ value }) => {
      if (!token) {
        throw new Error("Invite could not be completed.");
      }
      const password = formString(value.password);
      const confirmPassword = formString(value.confirmPassword);
      if (password.length < 15) {
        throw new Error("Password must be at least 15 characters.");
      }
      if (password !== confirmPassword) {
        throw new Error("Passwords do not match.");
      }
      await acceptMemberInvite({ data: { token, password } });
      const login = new URL("/login", window.location.origin);
      if (org) {
        login.searchParams.set("redirect", `/${org}`);
      }
      window.location.assign(`${login.pathname}${login.search}`);
    },
  });

  return (
    <main className="grid min-h-svh place-items-center px-6 py-16">
      <section className="flex w-full max-w-sm flex-col">
        <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Accept invite</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Create a password to join{org ? ` ${org}` : ""}.
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
          className="mt-6 grid gap-4"
        >
          <form.Field name="password">
            {(field) => (
              <div className="space-y-1.5">
                <Label htmlFor={field.name}>Password</Label>
                <Input
                  id={field.name}
                  type="password"
                  autoComplete="new-password"
                  value={formString(field.state.value)}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  disabled={!token}
                />
              </div>
            )}
          </form.Field>
          <form.Field name="confirmPassword">
            {(field) => (
              <div className="space-y-1.5">
                <Label htmlFor={field.name}>Confirm password</Label>
                <Input
                  id={field.name}
                  type="password"
                  autoComplete="new-password"
                  value={formString(field.state.value)}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  disabled={!token}
                />
              </div>
            )}
          </form.Field>
          <form.Subscribe selector={(state) => [state.isSubmitting, state.errorMap.onSubmit]}>
            {([isSubmitting, submitError]) => (
              <div className="grid gap-3">
                <Button type="submit" aria-busy={isSubmitting} disabled={!token || isSubmitting}>
                  <KeyRound aria-hidden="true" />
                  <span>{isSubmitting ? "Accepting..." : "Accept invite"}</span>
                </Button>
                {!token ? (
                  <p className="text-sm font-medium text-destructive">
                    Invite could not be completed.
                  </p>
                ) : submitError ? (
                  <p className="text-sm font-medium text-destructive">{String(submitError)}</p>
                ) : null}
              </div>
            )}
          </form.Subscribe>
        </form>
      </section>
    </main>
  );
}
