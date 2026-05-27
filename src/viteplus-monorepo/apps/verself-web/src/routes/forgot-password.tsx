import { revalidateLogic, useForm } from "@tanstack/react-form";
import { createFileRoute, Link, useHydrated } from "@tanstack/react-router";
import { KeyRound, Mail } from "lucide-react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  FieldError,
  authFormSubmit,
  authFormSubmitBusy,
  authFormSubmitInvalid,
  fieldInvalid,
  submitErrorText,
} from "~/features/auth/form-primitives";
import {
  emailSchema,
  forgotPasswordFormSchema,
  formString,
  normalizeEmail,
} from "~/features/auth/form-schemas";
import { Squircle } from "~/features/console/flight/squircle";
import { startPasswordReset } from "~/server-fns/auth";

type ForgotPasswordSearch = {
  readonly sent?: string;
};

function forgotPasswordSearch(search: Record<string, unknown>): ForgotPasswordSearch {
  return typeof search.sent === "string" && search.sent.trim() ? { sent: search.sent.trim() } : {};
}

export const Route = createFileRoute("/forgot-password")({
  validateSearch: forgotPasswordSearch,
  component: ForgotPasswordPage,
});

function ForgotPasswordPage() {
  const hydrated = useHydrated();
  const search = Route.useSearch();
  const form = useForm({
    defaultValues: {
      email: search.sent ?? "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: forgotPasswordFormSchema,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      const email = normalizeEmail(value.email);
      await startPasswordReset({ data: { email } });
      window.location.assign(`/forgot-password?sent=${encodeURIComponent(email)}`);
    },
  });

  return (
    <main className="min-h-svh bg-background px-4 py-6 text-foreground sm:px-6">
      <div className="mx-auto grid min-h-[calc(100svh-3rem)] w-full max-w-5xl items-end gap-8 md:grid-cols-[1fr_390px] md:items-center">
        <section className="hidden pb-6 md:block">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="max-w-xl text-4xl font-semibold tracking-tight">Reset password</h1>
          <p className="mt-4 max-w-lg text-sm leading-6 text-muted-foreground">
            Use email recovery for a Verself account.
          </p>
        </section>
        <Squircle
          cornerRadius={36}
          className="w-full bg-card p-5 shadow-[0_24px_80px_rgba(0,0,0,0.18)] md:p-6"
        >
          {search.sent ? (
            <div className="grid gap-5">
              <div className="flex items-center gap-3">
                <div className="flex size-10 items-center justify-center rounded-md border border-border bg-muted">
                  <Mail className="size-4" aria-hidden="true" />
                </div>
                <div>
                  <h2 className="text-lg font-semibold tracking-tight">Check your email</h2>
                  <p className="text-sm text-muted-foreground">
                    If that email has an account, we sent reset instructions.
                  </p>
                </div>
              </div>
              <Link
                to="/login"
                className="text-sm text-muted-foreground underline underline-offset-4"
              >
                Back to sign in
              </Link>
            </div>
          ) : (
            <>
              <div className="mb-5 flex items-center gap-3">
                <div className="flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
                  VS
                </div>
                <div>
                  <h2 className="text-lg font-semibold tracking-tight">Reset password</h2>
                  <p className="text-sm text-muted-foreground">Enter your account email.</p>
                </div>
              </div>
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  authFormSubmit(form);
                }}
                className="grid gap-4"
              >
                <form.Field
                  name="email"
                  validators={{ onBlur: emailSchema, onChange: emailSchema }}
                >
                  {(field) => {
                    const errorId = `${field.name}-error`;
                    return (
                      <div className="grid gap-1.5 text-sm font-medium">
                        <Label htmlFor={field.name}>Email</Label>
                        <Input
                          id={field.name}
                          aria-describedby={errorId}
                          aria-invalid={fieldInvalid(field.state.meta) || undefined}
                          autoComplete="username"
                          inputMode="email"
                          name={field.name}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                          type="email"
                          value={formString(field.state.value)}
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
                    <div className="grid gap-3">
                      <Button
                        type="submit"
                        aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
                      >
                        <KeyRound aria-hidden="true" />
                        <span>{isSubmitting ? "Sending..." : "Send reset email"}</span>
                      </Button>
                      <p className="min-h-5 text-sm font-medium text-destructive">
                        {submitErrorText(submitError)}
                      </p>
                    </div>
                  )}
                </form.Subscribe>
              </form>
            </>
          )}
        </Squircle>
      </div>
    </main>
  );
}
