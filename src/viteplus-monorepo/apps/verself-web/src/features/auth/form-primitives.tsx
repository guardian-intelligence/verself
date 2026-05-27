import type { AnyFieldMeta } from "@tanstack/react-form";
import type { ReactNode } from "react";
import { cn } from "@verself/ui/lib/utils";

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

export function authFormSubmitDisabled(state: {
  readonly hydrated: boolean;
  readonly canSubmit: boolean | undefined;
  readonly isSubmitting: boolean | undefined;
  readonly isValidating: boolean | undefined;
  readonly isPending?: boolean;
  readonly allowed?: boolean;
}): boolean {
  return (
    !state.hydrated ||
    state.allowed === false ||
    !state.canSubmit ||
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
