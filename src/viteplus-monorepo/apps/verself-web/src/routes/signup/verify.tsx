import { revalidateLogic, useForm } from "@tanstack/react-form";
import { Link, createFileRoute, useHydrated, useNavigate, useRouter } from "@tanstack/react-router";
import { Building2, CheckCircle2, Eye, EyeOff, MailPlus } from "lucide-react";
import { useReducer, type ReactNode } from "react";
import { iamFormValidationError, useIamFormSubmit } from "@verself/auth-web/form";
import { isIamError, type IamError } from "@verself/sdk/iam-errors";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { cn } from "@verself/ui/lib/utils";
import {
  FieldError,
  SubmitError,
  authFormSubmit,
  authFormSubmitBusy,
  authFormSubmitInvalid,
  fieldErrorText,
  fieldInvalid,
} from "~/features/auth/form-primitives";
import { completeBrowserCallback } from "~/features/auth/complete-callback";
import { signupVerificationIamFormError } from "~/features/auth/iam-error-copy";
import {
  formString,
  inviteLinkSchema,
  newPasswordSchema,
  normalizeHumanText,
  organizationNameSchema,
  organizationSlugSchema,
  signupVerificationCreateFormSchema,
  signupVerificationJoinFormSchema,
  type SignupVerificationFormValues,
  slugify,
} from "~/features/auth/form-schemas";
import { PASSWORD_GUIDANCE_TEXT } from "~/features/auth/password-policy";
import { organizationSlugAvailabilityError } from "~/features/auth/slug-availability";
import { acceptMemberInvite, passwordLogin, verifySignup } from "~/server-fns/auth";

type SignupMode = "create" | "join";

type SignupVerificationSubmitSuccess =
  | {
      readonly kind: "login-target";
      readonly target: string;
    }
  | {
      readonly kind: "callback-url";
      readonly callbackUrl: string;
    };

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
  const navigate = useNavigate();
  const router = useRouter();
  const search = Route.useSearch();
  const signupIntentId = search.signup_intent_id;
  const verificationToken = search.verification_token;
  const organizationDisplayName = search.organization_display_name;
  const organizationSlug = search.organization_slug;
  const mode = signupMode(search.mode);
  const [passwordVisible, togglePasswordVisible] = useReducer((value: boolean) => !value, false);
  const signupSubmit = useIamFormSubmit({
    submit: async (
      value: SignupVerificationFormValues,
    ): Promise<SignupVerificationSubmitSuccess | IamError> => {
      if (mode === "join") {
        const invite = inviteCredentialsFromInput(formString(value.inviteLink));
        const result = await acceptMemberInvite({ data: { token: invite.token } });
        if (isIamError(result)) {
          return result;
        }
        return {
          kind: "login-target",
          target: result.loginIntent?.loginUrl ?? result.loginUrl ?? loginPath(invite.org),
        };
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
      if (isIamError(result)) {
        return result;
      }
      if (!result.loginIntent) {
        throw new Error("Signup completed, but sign-in could not be started.");
      }
      const login = await passwordLogin({
        data: {
          email: result.loginIntent.requiredEmail,
          password: initialPassword,
          redirectTo: result.loginIntent.redirectTo ?? `/${result.organization.slug}`,
          purpose: result.loginIntent.purpose,
          loginHint: result.loginIntent.requiredEmail,
          requiredSubject: result.loginIntent.requiredSubject,
          requiredEmail: result.loginIntent.requiredEmail,
          requiredOrgId: result.loginIntent.requiredOrgId,
          prompt: "login",
        },
      });
      if (isIamError(login)) {
        return login;
      }
      return {
        kind: "callback-url",
        callbackUrl: login.callbackUrl,
      };
    },
    mapError: signupVerificationIamFormError,
  });
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
      onSubmit: () =>
        mode === "create" && (!signupIntentId || !verificationToken)
          ? iamFormValidationError<SignupVerificationFormValues>({
              form: "Signup link could not be completed.",
            })
          : undefined,
      onSubmitAsync: signupSubmit.validate,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async () => {
      const result = signupSubmit.requireSuccess();
      if (result.kind === "login-target") {
        await navigateLogin(navigate, result.target);
        return;
      }
      await completeBrowserCallback(router, result.callbackUrl);
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
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            authFormSubmit(form);
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
            selector={(state) =>
              [state.isSubmitting, state.isValidating, state.errorMap.onSubmit] as const
            }
          >
            {([isSubmitting, isValidating, submitError]) => {
              const missingSignupLink =
                mode === "create" && (!signupIntentId || !verificationToken);
              return (
                <div className="grid gap-3">
                  <Button
                    type="submit"
                    aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
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

function loginPath(orgSlug: string | undefined): string {
  return orgSlug ? `/login?redirect=/${orgSlug}` : "/login";
}

type Navigate = ReturnType<typeof useNavigate>;

async function navigateLogin(
  navigate: Navigate,
  loginURLOrOrgSlug: string | undefined,
  fallbackOrgSlug?: string,
): Promise<void> {
  if (loginURLOrOrgSlug?.startsWith("http") || loginURLOrOrgSlug?.startsWith("/login")) {
    await navigateToSameOrigin(navigate, loginURLOrOrgSlug);
    return;
  }
  const orgSlug = fallbackOrgSlug ?? loginURLOrOrgSlug;
  await navigate({
    to: "/login",
    search: {
      prompt: "login",
      ...(orgSlug ? { redirect: `/${orgSlug}` } : {}),
    },
  });
}

async function navigateToSameOrigin(navigate: Navigate, href: string): Promise<void> {
  const url = new URL(href, window.location.origin);
  if (url.origin !== window.location.origin) {
    throw new Error("External auth callback URL rejected.");
  }
  if (url.pathname.startsWith("/api/v1/auth/")) {
    window.location.assign(url.href);
    return;
  }
  const options = {
    to: url.pathname,
    search: searchParamsObject(url.searchParams),
    ...(url.hash ? { hash: url.hash.slice(1) } : {}),
  };
  await navigate(options as unknown as Parameters<Navigate>[0]);
}

function searchParamsObject(params: URLSearchParams): Record<string, string | Array<string>> {
  const out: Record<string, string | Array<string>> = {};
  for (const [key, value] of params.entries()) {
    const existing = out[key];
    if (Array.isArray(existing)) {
      existing.push(value);
      continue;
    }
    out[key] = existing === undefined ? value : [existing, value];
  }
  return out;
}
