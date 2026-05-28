import { revalidateLogic, useForm } from "@tanstack/react-form";
import { createFileRoute, Link, useHydrated, useNavigate } from "@tanstack/react-router";
import { CheckCircle2, Mail, UserPlus } from "lucide-react";
import { useIamFormSubmit } from "@verself/auth-web/form";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  FieldError,
  authFormSubmit,
  authFormSubmitBusy,
  authFormSubmitInvalid,
  fieldErrorText,
  fieldInvalid,
  submitErrorText,
} from "~/features/auth/form-primitives";
import { signupStartIamFormError } from "~/features/auth/iam-error-copy";
import { organizationSlugAvailabilityError } from "~/features/auth/slug-availability";
import {
  emailSchema,
  formString,
  normalizeEmail,
  normalizeHumanText,
  organizationNameSchema,
  organizationSlugSchema,
  signupStartFormSchema,
  type SignupStartFormValues,
  slugify,
} from "~/features/auth/form-schemas";
import { Squircle } from "~/features/console/flight/squircle";
import { startSignup } from "~/server-fns/auth";

type SignupSearch = {
  readonly sent?: string;
};

function signupSearch(search: Record<string, unknown>): SignupSearch {
  return typeof search.sent === "string" && search.sent.trim() ? { sent: search.sent.trim() } : {};
}

export const Route = createFileRoute("/signup")({
  validateSearch: signupSearch,
  component: SignupPage,
});

function SignupPage() {
  const search = Route.useSearch();
  return (
    <main className="min-h-svh bg-background px-4 py-6 text-foreground sm:px-6">
      <div className="mx-auto grid min-h-[calc(100svh-3rem)] w-full max-w-5xl items-end gap-8 md:grid-cols-[1fr_390px] md:items-center">
        <section className="hidden pb-6 md:block">
          <div className="mb-5 flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
            VS
          </div>
          <h1 className="max-w-xl text-4xl font-semibold tracking-tight">Verself</h1>
          <p className="mt-4 max-w-lg text-sm leading-6 text-muted-foreground">
            Create the workspace account that owns your first organization.
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
                  <p className="text-sm text-muted-foreground">{search.sent}</p>
                </div>
              </div>
              <Link
                to="/login"
                className="text-sm text-muted-foreground underline underline-offset-4"
              >
                Sign in instead
              </Link>
            </div>
          ) : (
            <SignupForm />
          )}
        </Squircle>
      </div>
    </main>
  );
}

function SignupForm() {
  const hydrated = useHydrated();
  const navigate = useNavigate();
  const signupSubmit = useIamFormSubmit({
    submit: (value: SignupStartFormValues) => {
      const organizationSlug = slugify(formString(value.organizationSlug));
      return startSignup({
        data: {
          email: normalizeEmail(value.email),
          organizationDisplayName: normalizeHumanText(value.organizationDisplayName),
          ...(organizationSlug ? { organizationSlug } : {}),
          givenName: formString(value.givenName).trim(),
          familyName: formString(value.familyName).trim(),
        },
      });
    },
    mapError: signupStartIamFormError,
  });
  const form = useForm({
    defaultValues: {
      email: "",
      organizationDisplayName: "",
      organizationSlug: "",
      givenName: "",
      familyName: "",
    },
    validationLogic: revalidateLogic({
      mode: "blur",
      modeAfterSubmission: "change",
    }),
    validators: {
      onDynamic: signupStartFormSchema,
      onSubmitAsync: signupSubmit.validate,
    },
    canSubmitWhenInvalid: true,
    onSubmitInvalid: authFormSubmitInvalid,
    onSubmit: async ({ value }) => {
      signupSubmit.requireSuccess();
      await navigate({
        to: "/signup",
        search: { sent: normalizeEmail(value.email) },
      });
    },
  });

  return (
    <>
      <div className="mb-5 flex items-center gap-3">
        <div className="flex size-10 items-center justify-center rounded-md border border-border bg-muted text-sm font-semibold">
          VS
        </div>
        <div>
          <h2 className="text-lg font-semibold tracking-tight">Create account</h2>
          <p className="text-sm text-muted-foreground">Start with an email and organization.</p>
        </div>
      </div>
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
                  autoComplete="email"
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
        <div className="grid grid-cols-2 gap-2">
          <form.Field name="givenName">
            {(field) => {
              const errorId = `${field.name}-error`;
              return (
                <div className="grid gap-1.5 text-sm font-medium">
                  <Label htmlFor={field.name}>First name</Label>
                  <Input
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    autoComplete="given-name"
                    name={field.name}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={formString(field.state.value)}
                  />
                  <FieldError id={errorId} meta={field.state.meta} />
                </div>
              );
            }}
          </form.Field>
          <form.Field name="familyName">
            {(field) => {
              const errorId = `${field.name}-error`;
              return (
                <div className="grid gap-1.5 text-sm font-medium">
                  <Label htmlFor={field.name}>Last name</Label>
                  <Input
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    autoComplete="family-name"
                    name={field.name}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={formString(field.state.value)}
                  />
                  <FieldError id={errorId} meta={field.state.meta} />
                </div>
              );
            }}
          </form.Field>
        </div>
        <form.Field
          name="organizationDisplayName"
          validators={{ onBlur: organizationNameSchema, onChange: organizationNameSchema }}
        >
          {(field) => {
            const errorId = `${field.name}-error`;
            return (
              <div className="grid gap-1.5 text-sm font-medium">
                <Label htmlFor={field.name}>Organization name</Label>
                <Input
                  id={field.name}
                  aria-describedby={errorId}
                  aria-invalid={fieldInvalid(field.state.meta) || undefined}
                  autoComplete="organization"
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => {
                    const value = event.target.value;
                    field.handleChange(value);
                    form.setFieldValue("organizationSlug", slugify(value));
                  }}
                  value={formString(field.state.value)}
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
            onChangeAsync: ({ value }) => organizationSlugAvailabilityError(formString(value)),
            onChangeAsyncDebounceMs: 350,
          }}
        >
          {(field) => {
            const errorId = `${field.name}-error`;
            const slug = slugify(formString(field.state.value));
            const error = fieldErrorText(field.state.meta);
            return (
              <div className="grid gap-1.5 text-sm font-medium">
                <Label htmlFor={field.name}>Organization URL</Label>
                <div className="relative min-w-0">
                  <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">
                    verself.sh/
                  </span>
                  <Input
                    id={field.name}
                    aria-describedby={errorId}
                    aria-invalid={fieldInvalid(field.state.meta) || undefined}
                    autoComplete="off"
                    className="pl-[5.75rem]"
                    name={field.name}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(slugify(event.target.value))}
                    value={formString(field.state.value)}
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
                {isSubmitting ? (
                  <CheckCircle2 aria-hidden="true" />
                ) : (
                  <UserPlus aria-hidden="true" />
                )}
                <span>{isSubmitting ? "Creating..." : "Create account"}</span>
              </Button>
              <p className="min-h-5 text-sm font-medium text-destructive">
                {submitErrorText(submitError)}
              </p>
            </div>
          )}
        </form.Subscribe>
      </form>
    </>
  );
}
