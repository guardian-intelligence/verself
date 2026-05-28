import { revalidateLogic, useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import {
  createFileRoute,
  redirect,
  useHydrated,
  useNavigate,
  useRouter,
} from "@tanstack/react-router";
import { Eye, EyeOff, KeyRound, LogIn, Trash2, UserRound } from "lucide-react";
import { useReducer } from "react";
import { useIamFormSubmit } from "@verself/auth-web/form";
import * as v from "valibot";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import { toast } from "@verself/ui/components/ui/sonner";
import {
  FieldError,
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
import {
  getClientAuthSnapshot,
  listBrowserAccounts,
  passwordLogin,
  removeBrowserAccount,
  selectActiveAccount,
  startGithubLogin,
} from "~/server-fns/auth";
import type { BrowserAccountSummary } from "~/server-fns/auth.server";
import { isIamError } from "@verself/sdk/iam-errors";

type LoginSearch = {
  readonly authRequest?: string;
  readonly prompt?: "login" | "select_account";
  readonly redirect?: string;
  readonly purpose?: string;
  readonly login_hint?: string;
  readonly required_subject?: string;
  readonly required_email?: string;
  readonly required_org_id?: string;
  readonly error?: string;
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
    error: v.optional(optionalSearchString),
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
        error: parsed.error,
      }),
  ),
);

function GithubMark() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="currentColor" width="16" height="16">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

function loginSearchErrorMessage(code: string): string {
  switch (code) {
    case "github_login_failed":
      return "GitHub sign-in could not be completed. Please try again.";
    case "github_login_expired":
      return "GitHub sign-in expired. Please start again.";
    default:
      return "Sign-in could not be completed. Please try again.";
  }
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

export const Route = createFileRoute("/_landing/login")({
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
  loader: async () => ({
    accounts: await listBrowserAccounts().catch(() => [] as Array<BrowserAccountSummary>),
  }),
  component: LoginPage,
});

function LoginPage() {
  const hydrated = useHydrated();
  const navigate = useNavigate();
  const search = Route.useSearch();
  const { accounts } = Route.useLoaderData();
  const [passwordVisible, togglePasswordVisible] = useReducer((value: boolean) => !value, false);
  const constrainedEmail = search.required_email ?? search.login_hint ?? "";
  const accountOptions = search.authRequest
    ? []
    : accounts.filter((account) => accountMatchesLoginSearch(account, search));
  const loginSubmit = useIamFormSubmit({
    submit: (value: LoginFormValues) =>
      passwordLogin({
        data: {
          email: normalizeEmail(value.email),
          password: formString(value.password),
          authRequestId: search.authRequest,
          redirectTo: search.redirect ?? "/",
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
  const githubLogin = useMutation({
    mutationFn: () =>
      startGithubLogin({
        data: { redirectTo: search.redirect ?? "/", purpose: search.purpose },
      }),
    onSuccess: (result) => {
      if (isIamError(result)) {
        toast.error("Could not start GitHub sign-in. Please try again.");
        return;
      }
      // Full-page navigation to GitHub's authorize URL (external origin).
      globalThis.location.assign(result.redirectURL);
    },
    onError: () => {
      toast.error("Could not start GitHub sign-in. Please try again.");
    },
  });
  const form = useForm({
    defaultValues: {
      email: constrainedEmail,
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

  return (
    <section className="grid gap-4" aria-label="Email sign in form">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Or use your workspace email
      </p>
      <div className="mb-1 flex items-center gap-3">
        <div className="flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Sign in</h2>
          {search.required_email ? (
            <p className="text-sm text-muted-foreground">Continue with {search.required_email}.</p>
          ) : (
            <p className="text-sm text-muted-foreground">Use your Verself account.</p>
          )}
        </div>      </div>
      {accountOptions.length > 0 ? <AccountChooser accounts={accountOptions} search={search} /> : null}
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
                  readOnly={Boolean(search.required_email)}
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
                <div className="grid grid-cols-[1fr_auto] items-center rounded-md border border-input bg-input/20 focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/30">
                  <input
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    autoComplete="current-password"
                    className="h-9 min-w-0 bg-transparent px-3 py-1 text-sm outline-none"
                    name={field.name}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    type={passwordVisible ? "text" : "password"}
                    value={formString(field.state.value)}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={passwordVisible ? "Hide password" : "Show password"}
                    onClick={togglePasswordVisible}
                    className="mr-1"
                  >
                    {passwordVisible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
                  </Button>
                </div>
                <FieldError id={errorId} meta={field.state.meta} />
              </div>
            );
          }}
        </form.Field>
        <div className="flex items-center justify-between text-sm">
          <a
            className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            href="/forgot-password"
          >
            Forgot password?
          </a>
        </div>
        <form.Subscribe selector={(state) => [state.isSubmitting, state.isValidating] as const}>
          {([isSubmitting, isValidating]) => (
            <div className="grid gap-3">
              <Button
                type="submit"
                aria-busy={authFormSubmitBusy({ hydrated, isSubmitting, isValidating })}
              >
                {isSubmitting ? <KeyRound aria-hidden="true" /> : <LogIn aria-hidden="true" />}
                <span>{isSubmitting ? "Signing in..." : "Sign in"}</span>
              </Button>
              <p className="min-h-5 text-sm text-muted-foreground">
                Passphrases and password managers are supported.
              </p>
            </div>
          )}
        </form.Subscribe>
      </form>
    </section>
  );
}

function AccountChooser({
  accounts,
  search,
}: {
  readonly accounts: ReadonlyArray<BrowserAccountSummary>;
  readonly search: LoginSearch;
}) {
  const navigate = useNavigate();
  const router = useRouter();
  const choose = useMutation({
    mutationFn: (accountHandle: string) => selectActiveAccount({ data: { accountHandle } }),
    onSuccess: () => {
      void navigateToSameOrigin(navigate, search.redirect ?? "/").catch((error: unknown) => {
        toast.error(errorMessage(error));
      });
    },
  });
  const remove = useMutation({
    mutationFn: (accountHandle: string) => removeBrowserAccount({ data: { accountHandle } }),
    onSuccess: () => {
      void router.invalidate().catch((error: unknown) => {
        toast.error(errorMessage(error));
      });
    },
  });

  return (
    <div className="mb-4 grid gap-2">
      {search.required_email ? (
        <p className="text-sm text-muted-foreground">
          Continue with {search.required_email} to finish signup.
        </p>
      ) : null}
      {accounts.map((account) => {
        const label = accountLabel(account);
        return (
          <div
            key={account.accountHandle}
            className="grid grid-cols-[1fr_auto] items-center gap-2 rounded-md border border-border bg-muted/30 p-2"
          >
            <button
              type="button"
              className="grid min-w-0 grid-cols-[auto_1fr] items-center gap-2 text-left"
              onClick={() => {
                if (choose.isPending || remove.isPending) {
                  toast.info("Still updating browser accounts.");
                  return;
                }
                choose.mutate(account.accountHandle);
              }}
            >
              <span className="flex size-8 items-center justify-center rounded-md border border-border bg-background">
                <UserRound className="size-4" aria-hidden="true" />
              </span>
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium">{label}</span>
                <span className="block truncate text-xs text-muted-foreground">
                  {account.email ?? account.subject}
                </span>
              </span>
            </button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Remove ${label} from this browser`}
              onClick={() => {
                if (choose.isPending || remove.isPending) {
                  toast.info("Still updating browser accounts.");
                  return;
                }
                remove.mutate(account.accountHandle);
              }}
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </div>
        );
      })}
      <p className="min-h-5 text-sm font-medium text-destructive">
        {errorMessage(choose.error) || errorMessage(remove.error)}
      </p>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>Use another account</span>
        <span className="h-px flex-1 bg-border" />
      </div>
    </div>
  );
}

function accountLabel(account: BrowserAccountSummary): string {
  return account.displayName ?? account.preferredUsername ?? account.email ?? "Verself account";
}

function hasLoginConstraint(search: LoginSearch): boolean {
  return Boolean(search.required_subject || search.required_email || search.required_org_id);
}

function accountMatchesLoginSearch(account: BrowserAccountSummary, search: LoginSearch): boolean {
  if (search.required_subject && account.subject !== search.required_subject) {
    return false;
  }
  if (search.required_email && !sameEmail(account.email, search.required_email)) {
    return false;
  }
  if (
    search.required_org_id &&
    account.homeOrgID !== search.required_org_id &&
    account.selectedOrgID !== search.required_org_id &&
    !account.availableOrganizations.some(
      (organization) => organization.orgID === search.required_org_id,
    )
  ) {
    return false;
  }
  return true;
}

function sameEmail(left: string | null, right: string): boolean {
  return left?.trim().toLowerCase() === right.trim().toLowerCase();
}

function errorMessage(error: unknown): string {
  if (!error) return "";
  if (error instanceof Error) return error.message;
  return "Account action failed.";
}

type Navigate = ReturnType<typeof useNavigate>;

async function navigateToSameOrigin(
  navigate: Navigate,
  href: string,
  replace = false,
): Promise<void> {
  const url = new URL(href, window.location.origin);
  if (url.origin !== window.location.origin) {
    throw new Error("External auth callback URL rejected.");
  }
  const options = {
    to: url.pathname,
    search: searchParamsObject(url.searchParams),
    ...(url.hash ? { hash: url.hash.slice(1) } : {}),
    replace,
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
