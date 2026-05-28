import type { DeepKeys, GlobalFormValidationError, ValidationError } from "@tanstack/react-form";
import { useRef } from "react";
import {
  iamErrorFromUnknown,
  isIamError,
  unknownIamError,
  type IamError,
  type IamErrorTag,
} from "@verself/sdk/iam-errors";

export type IamFormValidationError<TFormData> = GlobalFormValidationError<TFormData>;

export type IamFormErrorFieldMap<TFormData> = Partial<
  Record<IamErrorTag, ReadonlyArray<DeepKeys<TFormData>>>
>;

type SuccessSlot<TSuccess> =
  | {
      readonly state: "empty";
    }
  | {
      readonly state: "ready";
      readonly value: TSuccess;
    };

export function iamFormValidationError<TFormData>(input: {
  readonly form: ValidationError;
  readonly fields?: Partial<Record<DeepKeys<TFormData>, ValidationError>>;
}): IamFormValidationError<TFormData> {
  return {
    form: input.form,
    fields: input.fields ?? {},
  };
}

export function iamTaggedFormValidationError<TFormData>(
  error: IamError,
  options: {
    readonly message: (error: IamError) => ValidationError;
    readonly fields: IamFormErrorFieldMap<TFormData>;
  },
): IamFormValidationError<TFormData> {
  const message = options.message(error);
  return iamFormValidationError({
    form: message,
    fields: fieldErrors(options.fields[error._tag] ?? [], message),
  });
}

export function useIamFormSubmit<TFormData, TOutcome>(options: {
  readonly submit: (value: TFormData) => Promise<TOutcome>;
  readonly mapError: (error: IamError) => IamFormValidationError<TFormData>;
}): {
  readonly validate: (input: {
    readonly value: TFormData;
  }) => Promise<IamFormValidationError<TFormData> | undefined>;
  readonly requireSuccess: () => Exclude<TOutcome, IamError>;
} {
  const success = useRef<SuccessSlot<Exclude<TOutcome, IamError>>>({ state: "empty" });

  return {
    validate: async ({ value }) => {
      success.current = { state: "empty" };
      const result = await options.submit(value).catch(iamErrorFromUnknown);
      if (isIamError(result)) {
        return options.mapError(result);
      }
      if (result === undefined || result === null) {
        return options.mapError(
          unknownIamError({
            cause: {
              reason: "IAM form submit returned no result",
            },
          }),
        );
      }
      success.current = { state: "ready", value: result as Exclude<TOutcome, IamError> };
      return undefined;
    },
    requireSuccess: () => {
      const current = success.current;
      success.current = { state: "empty" };
      if (current.state !== "ready") {
        throw new Error("IAM form submit completed without a success result.");
      }
      return current.value;
    },
  };
}

function fieldErrors<TFormData>(
  fields: ReadonlyArray<DeepKeys<TFormData>>,
  message: ValidationError,
): Partial<Record<DeepKeys<TFormData>, ValidationError>> {
  return Object.fromEntries(fields.map((field) => [field, message])) as Partial<
    Record<DeepKeys<TFormData>, ValidationError>
  >;
}
