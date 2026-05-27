import { revalidateLogic, useForm } from "@tanstack/react-form";
import { Link, createFileRoute, useHydrated } from "@tanstack/react-router";
import { Building2, CheckCircle2, Eye, EyeOff, MailPlus } from "lucide-react";
import { useReducer, type ReactNode } from "react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { cn } from "@verself/ui/lib/utils";
import {
  FieldError,
  SubmitError,
  authFormSubmitBusy,
  authFormSubmitDisabled,
  fieldErrorText,
  fieldInvalid,
} from "~/features/auth/form-primitives";
import { passwordLoginErrorMessage } from "~/features/auth/auth-errors";
import {
  formString,
  inviteLinkSchema,
  newPasswordSchema,
  normalizeHumanText,
  organizationNameSchema,
  organizationSlugSchema,
  signupVerificationCreateFormSchema,
  signupVerificationJoinFormSchema,
  slugify,
} from "~/features/auth/form-schemas";
import {
  PASSWORD_CHECK_UNAVAILABLE_WARNING_CODE,
  PASSWORD_GUIDANCE_TEXT,
} from "~/features/auth/password-policy";
import { organizationSlugAvailabilityError } from "~/features/auth/slug-availability";
import { acceptMemberInvite, passwordLogin, verifySignup } from "~/server-fns/auth";
import type { AuthWarning } from "~/server-fns/auth.server";

type SignupMode = "create" | "join";

function searchString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function safeOrgSlug(value: unknown): string | undefined {
  const slug = searchString(value);
  return slug && /^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/.test(slug) ? slug : undefined;
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
  const hydrated = useHydrated();
  const search = Route.useSearch();
  const signupIntentId = search.signup_intent_id;
  const verificationToken = search.verification_token;
  const organizationDisplayName = search.organization_display_name;
  const organizationSlug = search.organization_slug;
  const mode = signupMode(search.mode);
  const [passwordVisible, togglePasswordVisible] = useReducer((value: boolean) => !value, false);
  const form = useForm({
    defaultValues: {
      organizationDisplayName: organizationDisplayName ?? "",
      organizationSlug: organizationSlug ?? slugify(organizationDisplayName ?? ""),
      initialPassword: "",
      confirmPassword: "",
      inviteLink: "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic:
        mode === "join" ? signupVerificationJoinFormSchema : signupVerificationCreateFormSchema,
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
      const displayName = normalizeHumanText(value.organizationDisplayName);
      const slug = slugify(formString(value.organizationSlug));
      const initialPassword = formString(value.initialPassword);
      const result = await verifySignup({
        data: {
          signupIntentId,
          verificationToken,
          initialPassword,
          organizationDisplayName: displayName,
          organizationSlug: slug,
        },
      });
      if (!result.loginIntent) {
        throw new Error("Signup completed, but sign-in could not be started.");
      }
      const login = await passwordLogin({
        data: {
          email: result.loginIntent.requiredEmail,
          password: initialPassword,
          redirectTo: redirectPathWithAuthWarnings(
            result.loginIntent.redirectTo ?? `/${result.organization.slug}`,
            result.warnings,
          ),
          purpose: result.loginIntent.purpose,
          loginHint: result.loginIntent.requiredEmail,
          requiredSubject: result.loginIntent.requiredSubject,
          requiredEmail: result.loginIntent.requiredEmail,
          requiredOrgId: result.loginIntent.requiredOrgId,
          prompt: "login",
        },
      });
      if (!login.ok) {
        throw new Error(passwordLoginErrorMessage(login.code));
      }
      window.location.assign(login.callbackUrl);
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
              <form.Field
                name="organizationDisplayName"
                validators={{ onBlur: organizationNameSchema, onChange: organizationNameSchema }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Organization name</Label>
                      <Input
                        id={field.name}
                        aria-describedby={errorId}
                        aria-invalid={fieldInvalid(field.state.meta) || undefined}
                        type="text"
                        autoComplete="organization"
                        value={formString(field.state.value)}
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value)}
                        disabled={!signupIntentId || !verificationToken}
                      />
                      <FieldError id={errorId} meta={field.state.meta} />
                    </div>
                  );
                }}
              </form.Field>
              <form.Field
                name="organizationSlug"
                validators={{
                  onBlur: organizationSlugSchema,
                  onBlurAsync: ({ value }) => organizationSlugAvailabilityError(formString(value)),
                  onChange: organizationSlugSchema,
                  onChangeAsync: ({ value }) =>
                    organizationSlugAvailabilityError(formString(value)),
                  onChangeAsyncDebounceMs: 350,
                }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  const slug = slugify(formString(field.state.value));
                  const error = fieldErrorText(field.state.meta);
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Organization URL</Label>
                      <div className="relative min-w-0">
                        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
                          verself.sh/
                        </span>
                        <Input
                          id={field.name}
                          aria-describedby={errorId}
                          aria-invalid={fieldInvalid(field.state.meta) || undefined}
                          type="text"
                          autoComplete="off"
                          value={formString(field.state.value)}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(slugify(event.target.value))}
                          className="pl-[5.75rem]"
                          disabled={!signupIntentId || !verificationToken}
                        />
                      </div>
                      <FieldError id={errorId} meta={field.state.meta} />
                      <p className="min-h-5 text-xs font-medium leading-5" aria-live="polite">
                        {!error &&
                        slug &&
                        (field.state.meta.isBlurred || field.state.meta.isValidating) ? (
                          <span className={field.state.meta.isValidating ? "" : "text-emerald-700"}>
                            {field.state.meta.isValidating ? "Checking..." : "Available."}
                          </span>
                        ) : null}
                      </p>
                    </div>
                  );
                }}
              </form.Field>
              <form.Field
                name="initialPassword"
                validators={{ onBlur: newPasswordSchema, onChange: newPasswordSchema }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  const hintId = `${field.name}-hint`;
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Password</Label>
                      <PasswordInput
                        id={field.name}
                        ariaDescribedBy={`${errorId} ${hintId}`}
                        invalid={fieldInvalid(field.state.meta)}
                        name={field.name}
                        autoComplete="new-password"
                        visible={passwordVisible}
                        value={formString(field.state.value)}
                        disabled={!signupIntentId || !verificationToken}
                        onBlur={field.handleBlur}
                        onChange={field.handleChange}
                        onToggle={togglePasswordVisible}
                      />
                      <FieldError id={errorId} meta={field.state.meta} />
                      <p id={hintId} className="text-xs leading-5 text-muted-foreground">
                        {PASSWORD_GUIDANCE_TEXT}
                      </p>
                    </div>
                  );
                }}
              </form.Field>
              <form.Field
                name="confirmPassword"
                validators={{
                  onBlur: ({ value, fieldApi }) => {
                    if (!value) return "Confirm the password.";
                    return value === fieldApi.form.getFieldValue("initialPassword")
                      ? undefined
                      : "Passwords do not match.";
                  },
                  onChange: ({ value, fieldApi }) => {
                    if (!value) return "Confirm the password.";
                    return value === fieldApi.form.getFieldValue("initialPassword")
                      ? undefined
                      : "Passwords do not match.";
                  },
                }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="space-y-1.5">
                      <Label htmlFor={field.name}>Confirm password</Label>
                      <PasswordInput
                        id={field.name}
                        ariaDescribedBy={errorId}
                        invalid={fieldInvalid(field.state.meta)}
                        name={field.name}
                        autoComplete="new-password"
                        visible={passwordVisible}
                        value={formString(field.state.value)}
                        disabled={!signupIntentId || !verificationToken}
                        onBlur={field.handleBlur}
                        onChange={field.handleChange}
                        onToggle={togglePasswordVisible}
                      />
                      <FieldError id={errorId} meta={field.state.meta} />
                    </div>
                  );
                }}
              </form.Field>
            </>
          ) : (
            <form.Field
              name="inviteLink"
              validators={{ onBlur: inviteLinkSchema, onChange: inviteLinkSchema }}
            >
              {(field) => {
                const errorId = `${field.name}-error`;
                return (
                  <div className="space-y-1.5">
                    <Label htmlFor={field.name}>Invite link</Label>
                    <Input
                      id={field.name}
                      aria-describedby={errorId}
                      aria-invalid={fieldInvalid(field.state.meta) || undefined}
                      type="text"
                      inputMode="url"
                      autoComplete="url"
                      value={formString(field.state.value)}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                    />
                    <FieldError id={errorId} meta={field.state.meta} />
                  </div>
                );
              }}
            </form.Field>
          )}
          <form.Subscribe
            selector={(state) => [
              state.canSubmit,
              state.isSubmitting,
              state.isValidating,
              state.errorMap.onSubmit,
            ]}
          >
            {([canSubmit, isSubmitting, isValidating, submitError]) => {
              const missingSignupLink =
                mode === "create" && (!signupIntentId || !verificationToken);
              return (
                <div className="grid gap-3">
                  <Button
                    type="submit"
                    aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
                    disabled={authFormSubmitDisabled({
                      hydrated,
                      canSubmit,
                      isSubmitting,
                      isValidating,
                      allowed: !missingSignupLink,
                    })}
                  >
                    <CheckCircle2 aria-hidden="true" />
                    <span>{isSubmitting ? "Finishing..." : "Done"}</span>
                  </Button>
                  {missingSignupLink ? (
                    <p className="text-sm font-medium text-destructive">
                      Signup link could not be completed.
                    </p>
                  ) : (
                    <SubmitError error={submitError} />
                  )}
                </div>
              );
            }}
          </form.Subscribe>
        </form>
      </section>
    </main>
  );
}

function redirectPathWithAuthWarnings(
  redirectTo: string,
  warnings: Array<AuthWarning> | undefined,
): string {
  if (!warnings?.some((warning) => warning.code === PASSWORD_CHECK_UNAVAILABLE_WARNING_CODE)) {
    return redirectTo;
  }
  const url = new URL(redirectTo, window.location.origin);
  url.searchParams.set("notice", "password_check_unavailable");
  return `${url.pathname}${url.search}${url.hash}`;
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

function PasswordInput(props: {
  readonly id: string;
  readonly ariaDescribedBy: string;
  readonly invalid: boolean;
  readonly name: string;
  readonly visible: boolean;
  readonly value: string;
  readonly autoComplete: string;
  readonly disabled: boolean;
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
        className="h-9 min-w-0 bg-transparent px-3 py-1 text-sm outline-none disabled:cursor-not-allowed disabled:opacity-50"
        disabled={props.disabled}
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
        disabled={props.disabled}
        onClick={props.onToggle}
        className="mr-1"
      >
        {props.visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
      </Button>
    </div>
  );
}

function inviteCredentialsFromInput(raw: string): { token: string; org?: string } {
  const value = raw.trim();
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
