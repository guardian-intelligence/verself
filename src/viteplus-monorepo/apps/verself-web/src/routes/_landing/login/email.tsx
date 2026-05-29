import { revalidateLogic, useForm } from "@tanstack/react-form";
import { Link, createFileRoute, redirect, useHydrated, useNavigate } from "@tanstack/react-router";
import { KeyRound } from "lucide-react";
import { useIamFormSubmit } from "@verself/auth-web/form";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  FieldError,
  SubmitError,
  authFormSubmit,
  authFormSubmitBusy,
  authFormSubmitInvalid,
  fieldInvalid,
} from "~/features/auth/form-primitives";
import { loginIamFormError } from "~/features/auth/iam-error-copy";
import {
  currentPasswordSchema,
  emailSchema,
  formString,
  loginFormSchema,
  normalizeEmail,
  type LoginFormValues,
} from "~/features/auth/form-schemas";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import { getClientAuthSnapshot, passwordLogin } from "~/server-fns/auth";
import * as v from "valibot";

type LoginSearch = {
  readonly authRequest?: string;
  readonly prompt?: "login" | "select_account";
  readonly redirect?: string;
  readonly purpose?: string;
  readonly login_hint?: string;
  readonly required_subject?: string;
  readonly required_email?: string;
  readonly required_org_id?: string;
  readonly email?: string;
  readonly link?: string;
};

const optionalSearchString = v.pipe(
  v.unknown(),
  v.transform((value) => (typeof value === "string" && value.trim() ? value.trim() : undefined)),
);

const loginPromptSearch = v.pipe(
  v.unknown(),
  v.transform((value) => (value === "login" || value === "select_account" ? value : undefined)),
);

const loginSearchSchema = v.pipe(
  v.object({
    authRequest: v.optional(optionalSearchString),
    auth_request: v.optional(optionalSearchString),
    prompt: v.optional(loginPromptSearch),
    redirect: v.optional(optionalSearchString),
    purpose: v.optional(optionalSearchString),
    login_hint: v.optional(optionalSearchString),
    required_subject: v.optional(optionalSearchString),
    required_email: v.optional(optionalSearchString),
    required_org_id: v.optional(optionalSearchString),
    email: v.optional(optionalSearchString),
    link: v.optional(optionalSearchString),
  }),
  v.transform(
    (parsed): LoginSearch =>
      withoutUndefined({
        authRequest: parsed.authRequest ?? parsed.auth_request,
        prompt: parsed.prompt,
        redirect: parsed.redirect,
        purpose: parsed.purpose,
        login_hint: parsed.login_hint,
        required_subject: parsed.required_subject,
        required_email: parsed.required_email,
        required_org_id: parsed.required_org_id,
        email: parsed.email,
        link: parsed.link,
      }),
  ),
);

export const Route = createFileRoute("/_landing/login/email")({
  validateSearch: loginSearchSchema,
  beforeLoad: async ({ context, search }) => {
    const snapshot = await getClientAuthSnapshot();
    if (
      snapshot.auth.isAuthenticated &&
      !search.authRequest &&
      !search.prompt &&
      !hasLoginConstraint(search)
    ) {
      const fallback = await resolveDefaultSignedInPath(context.queryClient, snapshot.auth);
      throw redirect({
        href: fallback,
        replace: true,
      });
    }
  },
  component: LoginEmailSheetForm,
});

function LoginEmailSheetForm() {
  const hydrated = useHydrated();
  const navigate = useNavigate();
  const search = Route.useSearch();

  const loginSubmit = useIamFormSubmit({
    submit: (value: LoginFormValues) =>
      passwordLogin({
        data: {
          email: normalizeEmail(value.email),
          password: formString(value.password),
          authRequestId: search.authRequest,
          redirectTo: search.redirect,
          purpose: search.purpose,
          loginHint: search.login_hint,
          requiredSubject: search.required_subject,
          requiredEmail: search.required_email,
          requiredOrgId: search.required_org_id,
          prompt: search.prompt,
        },
      }),
    mapError: loginIamFormError,
  });

  const form = useForm({
    defaultValues: {
      email: search.email ?? search.required_email ?? search.login_hint ?? "",
      password: "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: loginFormSchema,
      onSubmitAsync: loginSubmit.validate,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async () => {
      const result = loginSubmit.requireSuccess();
      await navigateToSameOrigin(navigate, result.callbackUrl);
    },
  });

  const linkingGithub = search.link === "github";

  return (
    <section aria-label="Email sign in form" className="grid gap-4 pb-2">
      <h2 className="text-lg font-semibold tracking-tight">
        {linkingGithub ? "Confirm it's you" : "Sign in"}
      </h2>
      <p className="text-sm text-muted-foreground">
        {linkingGithub
          ? "An account with this email already exists. Enter your password to connect GitHub."
          : "Use your account email and password."}
      </p>
      <form
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          authFormSubmit(form);
        }}
        className="grid gap-4"
      >
        <form.Field name="email" validators={{ onBlur: emailSchema, onChange: emailSchema }}>
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
        <form.Field
          name="password"
          validators={{ onBlur: currentPasswordSchema, onChange: currentPasswordSchema }}
        >
          {(field) => {
            const errorId = `${field.name}-error`;
            return (
              <div className="grid gap-1.5 text-sm font-medium">
                <Label htmlFor={field.name}>Password</Label>
                <Input
                  id={field.name}
                  aria-describedby={errorId}
                  aria-invalid={fieldInvalid(field.state.meta) || undefined}
                  autoComplete="current-password"
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="password"
                  value={formString(field.state.value)}
                />
                <FieldError id={errorId} meta={field.state.meta} />
              </div>
            );
          }}
        </form.Field>
        <form.Subscribe
          selector={(state) =>
            [state.isSubmitting, state.isValidating, state.errorMap.onSubmit] as const
          }
        >
          {([isSubmitting, isValidating, submitError]) => (
            <div className="grid gap-3">
              <Button
                type="submit"
                aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
              >
                <KeyRound aria-hidden="true" />
                <span>{isSubmitting ? "Signing in..." : "Sign in"}</span>
              </Button>
              <SubmitError error={submitError} />
            </div>
          )}
        </form.Subscribe>
      </form>
      <Link
        to="/forgot-password"
        className="text-sm text-muted-foreground underline underline-offset-4"
      >
        Forgot password?
      </Link>
    </section>
  );
}

function hasLoginConstraint(search: LoginSearch): boolean {
  return Boolean(search.required_subject || search.required_email || search.required_org_id);
}

function withoutUndefined<T extends Record<string, unknown>>(
  value: T,
): {
  readonly [K in keyof T]?: Exclude<T[K], undefined>;
} {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined)) as {
    readonly [K in keyof T]?: Exclude<T[K], undefined>;
  };
}

async function navigateToSameOrigin(
  navigate: ReturnType<typeof useNavigate>,
  href: string,
): Promise<void> {
  const url = new URL(href, window.location.origin);
  if (url.origin !== window.location.origin) {
    throw new Error("External auth callback URL rejected.");
  }
  const options = {
    to: url.pathname,
    search: searchParamsObject(url.searchParams),
    ...(url.hash ? { hash: url.hash.slice(1) } : {}),
  } as unknown as Parameters<ReturnType<typeof useNavigate>>[0];
  await navigate(options);
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
