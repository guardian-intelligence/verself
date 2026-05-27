import type { AnyFieldMeta, AnyFormApi } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { toast } from "@verself/ui/components/ui/sonner";
import { cn } from "@verself/ui/lib/utils";
import {
  BREACHED_PASSWORD_MESSAGE,
  PASSWORD_BREACH_HELP_TEXT,
  PASSWORD_BREACH_HELP_URL,
} from "./password-policy";

export function fieldErrorText(meta: AnyFieldMeta): string {
  const error = meta.errors.find((item): item is NonNullable<typeof item> => Boolean(item));
  if (!error) return "";
  if (!meta.isBlurred && !meta.errorMap.onSubmit) return "";
  return validationMessage(error);
}

export function FieldError({
  id,
  meta,
  className,
}: {
  readonly id: string;
  readonly meta: AnyFieldMeta;
  readonly className?: string;
}) {
  const error = fieldErrorText(meta);
  return (
    <p
      id={id}
      role={error ? "alert" : undefined}
      className={cn("min-h-5 text-xs font-medium leading-5 text-destructive", className)}
    >
      {error}
    </p>
  );
}

export function FieldHint({
  id,
  children,
  className,
}: {
  readonly id: string;
  readonly children: ReactNode;
  readonly className?: string;
}) {
  return (
    <p id={id} className={cn("min-h-5 text-xs leading-5 text-muted-foreground", className)}>
      {children}
    </p>
  );
}

export function submitErrorText(error: unknown): string {
  if (!error) return "";
  return validationMessage(error);
}

export function authFormSubmit(form: AnyFormApi): void {
  if (form.state.isSubmitting || form.state.isValidating) {
    toast.info("Still working on the last request.");
    return;
  }
  void form.handleSubmit().catch((error: unknown) => {
    toast.error(validationMessage(error));
  });
}

export function authFormSubmitInvalid({ formApi }: { readonly formApi: AnyFormApi }): void {
  toast.error(firstFormErrorText(formApi) || "Check the highlighted fields.");
}

export function SubmitError({
  error,
  className,
}: {
  readonly error: unknown;
  readonly className?: string;
}) {
  const message = submitErrorText(error);
  return (
    <p
      role={message ? "alert" : undefined}
      className={cn("min-h-5 text-sm font-medium text-destructive", className)}
    >
      {message === BREACHED_PASSWORD_MESSAGE ? (
        <>
          <span>{message}</span>{" "}
          <a
            href={PASSWORD_BREACH_HELP_URL}
            target="_blank"
            rel="noreferrer"
            className="underline underline-offset-4"
          >
            {PASSWORD_BREACH_HELP_TEXT}
          </a>
        </>
      ) : (
        message
      )}
    </p>
  );
}

export function fieldInvalid(meta: AnyFieldMeta): boolean {
  return Boolean(fieldErrorText(meta));
}

export function authFormSubmitBusy(state: {
  readonly hydrated: boolean;
  readonly isSubmitting: boolean | undefined;
  readonly isValidating: boolean | undefined;
  readonly isPending?: boolean;
}): boolean {
  return (
    !state.hydrated ||
    Boolean(state.isSubmitting) ||
    Boolean(state.isValidating) ||
    Boolean(state.isPending)
  );
}

function validationMessage(error: unknown): string {
  if (Array.isArray(error)) {
    const first = error.find(Boolean);
    return first ? validationMessage(first) : "";
  }
  if (error instanceof Error) return error.message;
  if (
    error &&
    typeof error === "object" &&
    "message" in error &&
    typeof error.message === "string"
  ) {
    return error.message;
  }
  return String(error);
}

function firstFormErrorText(form: AnyFormApi): string {
  for (const meta of Object.values(form.state.fieldMeta)) {
    const message = validationMessage(meta?.errors);
    if (message) return message;
  }
  return validationMessage(form.state.errors);
}
