import { revalidateLogic, useForm } from "@tanstack/react-form";
import { createFileRoute, Link, useHydrated } from "@tanstack/react-router";
import { Eye, EyeOff, KeyRound } from "lucide-react";
import { useReducer } from "react";
import { Button } from "@verself/ui/components/ui/button";
import { Label } from "@verself/ui/components/ui/label";
import {
  FieldError,
  SubmitError,
  authFormSubmitBusy,
  authFormSubmitDisabled,
  fieldInvalid,
} from "~/features/auth/form-primitives";
import {
  formString,
  newPasswordSchema,
  resetPasswordFormSchema,
} from "~/features/auth/form-schemas";
import { PASSWORD_CHECK_UNAVAILABLE_WARNING_CODE } from "~/features/auth/password-policy";
import { Squircle } from "~/features/console/flight/squircle";
import { completePasswordReset } from "~/server-fns/auth";
import type { AuthWarning } from "~/server-fns/auth.server";

type ResetPasswordSearch = {
  readonly user_id?: string;
  readonly verification_code?: string;
};

function searchString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function resetPasswordSearch(search: Record<string, unknown>): ResetPasswordSearch {
  const userID = searchString(search.user_id) ?? searchString(search.userId);
  const verificationCode =
    searchString(search.verification_code) ?? searchString(search.verificationCode);
  return {
    ...(userID ? { user_id: userID } : {}),
    ...(verificationCode ? { verification_code: verificationCode } : {}),
  };
}

export const Route = createFileRoute("/reset-password")({
  validateSearch: resetPasswordSearch,
  component: ResetPasswordPage,
});

function ResetPasswordPage() {
  const hydrated = useHydrated();
  const search = Route.useSearch();
  const [passwordVisible, togglePasswordVisible] = useReducer((value: boolean) => !value, false);
  const form = useForm({
    defaultValues: {
      password: "",
      confirmPassword: "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: resetPasswordFormSchema,
    },
    onSubmit: async ({ value }) => {
      const password = formString(value.password);
      if (!search.user_id || !search.verification_code) {
        throw new Error("This reset link is invalid or expired.");
      }
      const result = await completePasswordReset({
        data: {
          userId: search.user_id,
          verificationCode: search.verification_code,
          password,
        },
      });
      window.location.assign(loginPathAfterPasswordReset(result.warnings));
    },
  });

  return (
    <main className="min-h-svh bg-background px-4 py-6 text-foreground sm:px-6">
      <div className="mx-auto grid min-h-[calc(100svh-3rem)] w-full max-w-5xl items-end gap-8 md:grid-cols-[1fr_390px] md:items-center">
        <section className="hidden pb-6 md:block">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="max-w-xl text-4xl font-semibold tracking-tight">Set password</h1>
          <p className="mt-4 max-w-lg text-sm leading-6 text-muted-foreground">
            Choose the password for this Verself account.
          </p>
        </section>
        <Squircle
          cornerRadius={36}
          className="w-full bg-card p-5 shadow-[0_24px_80px_rgba(0,0,0,0.18)] md:p-6"
        >
          <div className="mb-5 flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
              VS
            </div>
            <div>
              <h2 className="text-lg font-semibold tracking-tight">Set password</h2>
              <p className="text-sm text-muted-foreground">
                {search.user_id && search.verification_code
                  ? "Enter a new password."
                  : "Reset link unavailable."}
              </p>
            </div>
          </div>
          {search.user_id && search.verification_code ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                event.stopPropagation();
                void form.handleSubmit();
              }}
              className="grid gap-4"
            >
              <form.Field
                name="password"
                validators={{ onBlur: newPasswordSchema, onChange: newPasswordSchema }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="grid gap-1.5 text-sm font-medium">
                      <Label htmlFor={field.name}>New password</Label>
                      <PasswordInput
                        id={field.name}
                        ariaDescribedBy={errorId}
                        invalid={fieldInvalid(field.state.meta)}
                        name={field.name}
                        visible={passwordVisible}
                        value={formString(field.state.value)}
                        autoComplete="new-password"
                        onBlur={field.handleBlur}
                        onChange={field.handleChange}
                        onToggle={togglePasswordVisible}
                      />
                      <FieldError id={errorId} meta={field.state.meta} />
                    </div>
                  );
                }}
              </form.Field>
              <form.Field
                name="confirmPassword"
                validators={{
                  onBlur: ({ value, fieldApi }) => {
                    if (!value) return "Confirm the password.";
                    return value === fieldApi.form.getFieldValue("password")
                      ? undefined
                      : "Passwords do not match.";
                  },
                  onChange: ({ value, fieldApi }) => {
                    if (!value) return "Confirm the password.";
                    return value === fieldApi.form.getFieldValue("password")
                      ? undefined
                      : "Passwords do not match.";
                  },
                }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="grid gap-1.5 text-sm font-medium">
                      <Label htmlFor={field.name}>Confirm password</Label>
                      <PasswordInput
                        id={field.name}
                        ariaDescribedBy={errorId}
                        invalid={fieldInvalid(field.state.meta)}
                        name={field.name}
                        visible={passwordVisible}
                        value={formString(field.state.value)}
                        autoComplete="new-password"
                        onBlur={field.handleBlur}
                        onChange={field.handleChange}
                        onToggle={togglePasswordVisible}
                      />
                      <FieldError id={errorId} meta={field.state.meta} />
                    </div>
                  );
                }}
              </form.Field>
              <form.Subscribe
                selector={(state) => [
                  state.canSubmit,
                  state.isSubmitting,
                  state.isValidating,
                  state.errorMap.onSubmit,
                ]}
              >
                {([canSubmit, isSubmitting, isValidating, submitError]) => (
                  <div className="grid gap-3">
                    <Button
                      type="submit"
                      aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
                      disabled={authFormSubmitDisabled({
                        hydrated,
                        canSubmit,
                        isSubmitting,
                        isValidating,
                      })}
                    >
                      <KeyRound aria-hidden="true" />
                      <span>{isSubmitting ? "Saving..." : "Save password"}</span>
                    </Button>
                    <SubmitError error={submitError} />
                  </div>
                )}
              </form.Subscribe>
            </form>
          ) : (
            <Link
              to="/forgot-password"
              className="text-sm text-muted-foreground underline underline-offset-4"
            >
              Request a new reset email
            </Link>
          )}
        </Squircle>
      </div>
    </main>
  );
}

function loginPathAfterPasswordReset(warnings: Array<AuthWarning> | undefined): string {
  const login = new URL("/login", window.location.origin);
  login.searchParams.set("prompt", "login");
  if (warnings?.some((warning) => warning.code === PASSWORD_CHECK_UNAVAILABLE_WARNING_CODE)) {
    login.searchParams.set("notice", "password_check_unavailable");
  }
  return `${login.pathname}${login.search}`;
}

function PasswordInput(props: {
  readonly id: string;
  readonly ariaDescribedBy: string;
  readonly invalid: boolean;
  readonly name: string;
  readonly visible: boolean;
  readonly value: string;
  readonly autoComplete: string;
  readonly onBlur: () => void;
  readonly onChange: (value: string) => void;
  readonly onToggle: () => void;
}) {
  return (
    <div className="grid grid-cols-[1fr_auto] items-center rounded-md border border-input bg-input/20 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/30">
      <input
        id={props.id}
        aria-describedby={props.ariaDescribedBy}
        aria-invalid={props.invalid || undefined}
        autoComplete={props.autoComplete}
        className="h-9 min-w-0 bg-transparent px-3 py-1 text-sm outline-none"
        name={props.name}
        onBlur={props.onBlur}
        onChange={(event) => props.onChange(event.target.value)}
        type={props.visible ? "text" : "password"}
        value={props.value}
      />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={props.visible ? "Hide password" : "Show password"}
        onClick={props.onToggle}
        className="mr-1"
      >
        {props.visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
      </Button>
    </div>
  );
}
