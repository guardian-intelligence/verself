import { revalidateLogic, useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, redirect, useHydrated } from "@tanstack/react-router";
import { Eye, EyeOff, KeyRound, LogIn, Trash2, UserRound } from "lucide-react";
import { useReducer } from "react";
import { authErrorMessage, authFailureFromUnknown } from "@verself/sdk/auth";
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
import {
  currentPasswordSchema,
  emailSchema,
  formString,
  loginFormSchema,
  normalizeEmail,
} from "~/features/auth/form-schemas";
import { Squircle } from "~/features/console/flight/squircle";
import { resolveDefaultSignedInPath } from "~/features/shell/org-route-loaders";
import {
  getClientAuthSnapshot,
  listBrowserAccounts,
  passwordLogin,
  removeBrowserAccount,
  selectActiveAccount,
} from "~/server-fns/auth";
import type { BrowserAccountSummary } from "~/server-fns/auth.server";

type LoginSearch = {
  readonly authRequest?: string;
  readonly prompt?: "login" | "select_account";
  readonly redirect?: string;
  readonly purpose?: string;
  readonly login_hint?: string;
  readonly required_subject?: string;
  readonly required_email?: string;
  readonly required_org_id?: string;
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
      }),
  ),
);

function withoutUndefined<T extends Record<string, unknown>>(
  value: T,
): {
  readonly [K in keyof T]?: Exclude<T[K], undefined>;
} {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined)) as {
    readonly [K in keyof T]?: Exclude<T[K], undefined>;
  };
}

export const Route = createFileRoute("/login")({
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
  const search = Route.useSearch();
  const { accounts } = Route.useLoaderData();
  const [passwordVisible, togglePasswordVisible] = useReducer((value: boolean) => !value, false);
  const constrainedEmail = search.required_email ?? search.login_hint ?? "";
  const accountOptions = search.authRequest
    ? []
    : accounts.filter((account) => accountMatchesLoginSearch(account, search));
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
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      const result = await passwordLogin({
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
      }).catch(authFailureFromUnknown);
      if (result._tag === "Err") {
        toast.error(authErrorMessage(result.error));
        return;
      }
      window.location.assign(result.value.callbackUrl);
    },
  });

  return (
    <main className="min-h-svh bg-background px-4 py-6 text-foreground sm:px-6">
      <div className="mx-auto grid min-h-[calc(100svh-3rem)] w-full max-w-5xl items-end gap-8 md:grid-cols-[1fr_390px] md:items-center">
        <section className="hidden pb-6 md:block">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="max-w-xl text-4xl font-semibold tracking-tight">Verself</h1>
          <p className="mt-4 max-w-lg text-sm leading-6 text-muted-foreground">
            Sign in with your workspace account to manage builds, devices, and organization access.
          </p>
          <div className="mt-8 grid max-w-md gap-3 text-sm">
            <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
              Password-manager friendly
            </div>
            <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
              Device login approvals stay on verself.sh
            </div>
          </div>
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
              <h2 className="text-lg font-semibold tracking-tight">Sign in</h2>
              {search.required_email ? (
                <p className="text-sm text-muted-foreground">
                  Continue with {search.required_email}.
                </p>
              ) : (
                <p className="text-sm text-muted-foreground">Use your Verself account.</p>
              )}
            </div>
          </div>
          {accountOptions.length > 0 ? (
            <AccountChooser accounts={accountOptions} search={search} />
          ) : null}
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
                        {passwordVisible ? (
                          <EyeOff aria-hidden="true" />
                        ) : (
                          <Eye aria-hidden="true" />
                        )}
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
            <form.Subscribe selector={(state) => [state.isSubmitting, state.isValidating]}>
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
        </Squircle>
      </div>
    </main>
  );
}

function AccountChooser({
  accounts,
  search,
}: {
  readonly accounts: ReadonlyArray<BrowserAccountSummary>;
  readonly search: LoginSearch;
}) {
  const choose = useMutation({
    mutationFn: (accountHandle: string) => selectActiveAccount({ data: { accountHandle } }),
    onSuccess: () => {
      window.location.assign(search.redirect ?? "/");
    },
  });
  const remove = useMutation({
    mutationFn: (accountHandle: string) => removeBrowserAccount({ data: { accountHandle } }),
    onSuccess: () => {
      window.location.reload();
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
