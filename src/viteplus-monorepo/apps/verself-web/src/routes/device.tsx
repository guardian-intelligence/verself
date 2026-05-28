import { revalidateLogic, useForm } from "@tanstack/react-form";
import { useMutation } from "@tanstack/react-query";
import { useLiveQuery } from "@tanstack/react-db";
import { createFileRoute, useHydrated } from "@tanstack/react-router";
import { CheckCircle2, KeyRound, XCircle } from "lucide-react";
import { useMemo } from "react";
import {
  authErrorMessage,
  authFailureFromUnknown,
  isAuthErrorException,
  unwrapAuthResult,
} from "@verself/sdk/auth";
import * as v from "valibot";
import { createElectricShapeCollection } from "@verself/web-env";
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
  submitErrorText,
} from "~/features/auth/form-primitives";
import {
  deviceCodeSchema,
  deviceCodeFormSchema,
  formString,
  normalizeDeviceCode,
} from "~/features/auth/form-schemas";
import { Squircle } from "~/features/console/flight/squircle";
import {
  approveDeviceLogin,
  denyDeviceLogin,
  getClientAuthSnapshot,
  lookupDeviceLogin,
} from "~/server-fns/auth";
import type { DeviceLoginResult } from "~/server-fns/auth.server";

type DeviceSearch = {
  readonly user_code?: string;
};

const deviceLoginProjectionSchema = v.object({
  device_login_handle: v.string(),
  user_code_suffix: v.string(),
  client_id: v.string(),
  app_name: v.string(),
  project_name: v.string(),
  scopes: v.union([
    v.array(v.string()),
    v.pipe(
      v.string(),
      v.transform((value) => JSON.parse(value) as unknown),
      v.array(v.string()),
    ),
  ]),
  state: v.string(),
  approved_at: v.union([
    v.date(),
    v.pipe(
      v.string(),
      v.transform((value) => new Date(value)),
    ),
  ]),
  denied_at: v.union([
    v.date(),
    v.pipe(
      v.string(),
      v.transform((value) => new Date(value)),
    ),
  ]),
  expires_at: v.union([
    v.date(),
    v.pipe(
      v.string(),
      v.transform((value) => new Date(value)),
    ),
  ]),
  updated_at: v.union([
    v.date(),
    v.pipe(
      v.string(),
      v.transform((value) => new Date(value)),
    ),
  ]),
});

type DeviceLoginProjection = v.InferOutput<typeof deviceLoginProjectionSchema>;

function deviceSearch(search: Record<string, unknown>): DeviceSearch {
  return typeof search.user_code === "string" && search.user_code.trim()
    ? { user_code: search.user_code.trim() }
    : {};
}

export const Route = createFileRoute("/device")({
  validateSearch: deviceSearch,
  loaderDeps: ({ search }) => search,
  loader: async ({ deps }) => {
    const [snapshot, lookup] = await Promise.all([
      getClientAuthSnapshot(),
      deps.user_code
        ? lookupDeviceLogin({ data: { userCode: deps.user_code } })
            .then(unwrapAuthResult)
            .then((result) => ({ result, error: "" }))
            .catch((error: unknown) => ({ result: undefined, error: errorMessage(error) }))
        : Promise.resolve({ result: undefined, error: "" }),
    ]);
    return {
      isSignedIn: snapshot.auth.isAuthenticated,
      lookup,
    };
  },
  component: DeviceLoginPage,
});

function DeviceLoginPage() {
  const hydrated = useHydrated();
  const { user_code } = Route.useSearch();
  const { isSignedIn, lookup } = Route.useLoaderData();
  const form = useForm({
    defaultValues: {
      userCode: normalizeDeviceCode(user_code ?? ""),
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: deviceCodeFormSchema,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      const code = normalizeDeviceCode(value.userCode);
      window.location.assign(`/device?user_code=${encodeURIComponent(code)}`);
    },
  });

  return (
    <main className="min-h-svh bg-background px-4 py-6 text-foreground sm:px-6">
      <div className="mx-auto grid min-h-[calc(100svh-3rem)] w-full max-w-5xl items-end gap-8 md:grid-cols-[1fr_390px] md:items-center">
        <section className="hidden pb-6 md:block">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="max-w-xl text-4xl font-semibold tracking-tight">Device login</h1>
          <p className="mt-4 max-w-lg text-sm leading-6 text-muted-foreground">
            Approve command-line and SDK sign-ins without leaving verself.sh.
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
              <h2 className="text-lg font-semibold tracking-tight">Approve device</h2>
              <p className="text-sm text-muted-foreground">
                {lookup.result ? deviceLoginSubtitle(lookup.result) : "Enter the device code."}
              </p>
            </div>
          </div>
          {!lookup.result ? (
            <form
              noValidate
              onSubmit={(event) => {
                event.preventDefault();
                event.stopPropagation();
                authFormSubmit(form);
              }}
              className="grid gap-4"
            >
              <form.Field
                name="userCode"
                validators={{ onBlur: deviceCodeSchema, onChange: deviceCodeSchema }}
              >
                {(field) => {
                  const errorId = `${field.name}-error`;
                  return (
                    <div className="grid gap-1.5 text-sm font-medium">
                      <Label htmlFor={field.name}>Device code</Label>
                      <Input
                        id={field.name}
                        aria-describedby={errorId}
                        aria-invalid={fieldInvalid(field.state.meta) || undefined}
                        autoComplete="one-time-code"
                        name={field.name}
                        onBlur={field.handleBlur}
                        onChange={(event) =>
                          field.handleChange(normalizeDeviceCode(event.target.value))
                        }
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
                      <span>Continue</span>
                    </Button>
                    <p className="min-h-5 text-sm font-medium text-destructive">
                      {lookup.error || submitErrorText(submitError)}
                    </p>
                  </div>
                )}
              </form.Subscribe>
            </form>
          ) : (
            <DeviceLoginDecision
              device={lookup.result}
              isSignedIn={isSignedIn}
              userCode={user_code ?? ""}
            />
          )}
        </Squircle>
      </div>
    </main>
  );
}

function DeviceLoginDecision({
  device,
  isSignedIn,
  userCode,
}: {
  readonly device: DeviceLoginResult;
  readonly isSignedIn: boolean;
  readonly userCode: string;
}) {
  const approval = useMutation({
    mutationFn: async () =>
      unwrapAuthResult(
        await approveDeviceLogin({ data: { deviceLoginHandle: device.deviceLoginHandle } }).catch(
          authFailureFromUnknown,
        ),
      ),
  });
  const denial = useMutation({
    mutationFn: async () =>
      unwrapAuthResult(
        await denyDeviceLogin({ data: { deviceLoginHandle: device.deviceLoginHandle } }).catch(
          authFailureFromUnknown,
        ),
      ),
  });
  return (
    <div className="grid gap-4">
      <LiveDeviceLoginStatus initial={device} />
      {isSignedIn ? (
        <div className="grid grid-cols-2 gap-2">
          <Button
            type="button"
            aria-busy={approval.isPending}
            onClick={() => {
              if (approval.isPending || denial.isPending) {
                toast.info("Still updating this device request.");
                return;
              }
              if (device.state !== "pending") {
                toast.error("This device request is no longer pending.");
                return;
              }
              approval.mutate();
            }}
          >
            <CheckCircle2 aria-hidden="true" />
            <span>Approve</span>
          </Button>
          <Button
            type="button"
            variant="outline"
            aria-busy={denial.isPending}
            onClick={() => {
              if (approval.isPending || denial.isPending) {
                toast.info("Still updating this device request.");
                return;
              }
              if (device.state !== "pending") {
                toast.error("This device request is no longer pending.");
                return;
              }
              denial.mutate();
            }}
          >
            <XCircle aria-hidden="true" />
            <span>Deny</span>
          </Button>
        </div>
      ) : (
        <Button type="button" onClick={() => window.location.assign(signInURL(userCode))}>
          <KeyRound aria-hidden="true" />
          <span>Sign in</span>
        </Button>
      )}
      <p className="min-h-5 text-sm font-medium text-destructive">
        {errorMessage(approval.error) || errorMessage(denial.error)}
      </p>
    </div>
  );
}

function LiveDeviceLoginStatus({ initial }: { readonly initial: DeviceLoginResult }) {
  const collection = useMemo(
    () =>
      createElectricShapeCollection({
        id: `auth:device-login:${initial.deviceLoginHandle}`,
        schema: deviceLoginProjectionSchema,
        getKey: (item: DeviceLoginProjection) => item.device_login_handle,
        shapePath: `/api/sync/auth/device-login?device_login_handle=${encodeURIComponent(initial.deviceLoginHandle)}`,
      }),
    [initial.deviceLoginHandle],
  );
  const query = useLiveQuery(() => collection, [collection]);
  const live = query.data?.[0];
  const state = live?.state ?? initial.state;
  return (
    <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
      <div className="flex items-center justify-between gap-3">
        <span className="text-muted-foreground">Status</span>
        <span className="font-medium capitalize">{state}</span>
      </div>
      <div className="mt-2 flex items-center justify-between gap-3">
        <span className="text-muted-foreground">Code</span>
        <span className="font-mono text-xs">ends {initial.userCodeSuffix}</span>
      </div>
    </div>
  );
}

function signInURL(userCode: string): string {
  const login = new URL("/login", window.location.origin);
  login.searchParams.set("redirect", `/device?user_code=${encodeURIComponent(userCode)}`);
  login.searchParams.set("purpose", "device");
  return `${login.pathname}${login.search}`;
}

function deviceLoginSubtitle(device: DeviceLoginResult): string {
  const app = device.appName || device.clientId || "this client";
  return `Continue ${app}.`;
}

function errorMessage(error: unknown): string {
  if (!error) return "";
  if (typeof error === "string") return error;
  if (isAuthErrorException(error)) return authErrorMessage(error.authError);
  if (error instanceof Error) return error.message;
  return "Unexpected error.";
}
