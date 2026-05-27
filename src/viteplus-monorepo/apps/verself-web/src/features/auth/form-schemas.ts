import * as v from "valibot";
import { MIN_PASSWORD_LENGTH, PASSWORD_LENGTH_MESSAGE } from "./password-policy";

export { MIN_PASSWORD_LENGTH };

export const organizationSlugPattern = /^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/;

export const emailSchema = v.pipe(
  v.string("Email is required."),
  v.trim(),
  v.nonEmpty("Email is required."),
  v.email("Enter a valid email address."),
);

export const currentPasswordSchema = v.pipe(
  v.string("Password is required."),
  v.nonEmpty("Password is required."),
);

export const newPasswordSchema = v.pipe(
  v.string("Password is required."),
  v.nonEmpty("Password is required."),
  v.minLength(MIN_PASSWORD_LENGTH, PASSWORD_LENGTH_MESSAGE),
);

export const organizationNameSchema = v.pipe(
  v.string("Organization name is required."),
  v.trim(),
  v.nonEmpty("Organization name is required."),
  v.maxLength(120, "Organization name must be 120 characters or fewer."),
);

export const organizationSlugSchema = v.pipe(
  v.string("Organization URL is required."),
  v.trim(),
  v.nonEmpty("Organization URL is required."),
  v.regex(organizationSlugPattern, "Use lowercase letters, numbers, and single hyphens."),
);

export const optionalOrganizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.regex(
    /^$|^[a-z0-9]([a-z0-9-]{0,78}[a-z0-9])?$/,
    "Use lowercase letters, numbers, and single hyphens.",
  ),
);

export const deviceCodeSchema = v.pipe(
  v.string("Device code is required."),
  v.trim(),
  v.nonEmpty("Device code is required."),
  v.regex(/^[A-Z0-9][A-Z0-9-]{3,63}$/, "Enter the device code from your terminal."),
);

export const inviteLinkSchema = v.pipe(
  v.string("Invite link is required."),
  v.trim(),
  v.nonEmpty("Invite link is required."),
);

export const loginFormSchema = v.object({
  email: emailSchema,
  password: currentPasswordSchema,
});

export const signupStartFormSchema = v.object({
  email: emailSchema,
  organizationDisplayName: organizationNameSchema,
  organizationSlug: organizationSlugSchema,
  givenName: v.pipe(v.string(), v.trim(), v.maxLength(100, "First name is too long.")),
  familyName: v.pipe(v.string(), v.trim(), v.maxLength(100, "Last name is too long.")),
});

export const forgotPasswordFormSchema = v.object({
  email: emailSchema,
});

export const resetPasswordFormSchema = v.pipe(
  v.object({
    password: newPasswordSchema,
    confirmPassword: v.pipe(v.string("Confirm the password."), v.nonEmpty("Confirm the password.")),
  }),
  v.forward(
    v.partialCheck(
      [["password"], ["confirmPassword"]],
      (input) => input.password === input.confirmPassword,
      "Passwords do not match.",
    ),
    ["confirmPassword"],
  ),
);

export const signupVerificationCreateFormSchema = v.pipe(
  v.object({
    organizationDisplayName: organizationNameSchema,
    organizationSlug: organizationSlugSchema,
    initialPassword: newPasswordSchema,
    confirmPassword: v.pipe(v.string("Confirm the password."), v.nonEmpty("Confirm the password.")),
    inviteLink: v.string(),
  }),
  v.forward(
    v.partialCheck(
      [["initialPassword"], ["confirmPassword"]],
      (input) => input.initialPassword === input.confirmPassword,
      "Passwords do not match.",
    ),
    ["confirmPassword"],
  ),
);

export const signupVerificationJoinFormSchema = v.object({
  organizationDisplayName: v.string(),
  organizationSlug: v.string(),
  initialPassword: v.string(),
  confirmPassword: v.string(),
  inviteLink: inviteLinkSchema,
});

export const deviceCodeFormSchema = v.object({
  userCode: deviceCodeSchema,
});

export const onboardingOrganizationFormSchema = v.object({
  displayName: organizationNameSchema,
  slug: optionalOrganizationSlugSchema,
});

export type LoginFormValues = v.InferInput<typeof loginFormSchema>;
export type SignupStartFormValues = v.InferInput<typeof signupStartFormSchema>;
export type ForgotPasswordFormValues = v.InferInput<typeof forgotPasswordFormSchema>;
export type ResetPasswordFormValues = v.InferInput<typeof resetPasswordFormSchema>;
export type SignupVerificationFormValues = v.InferInput<typeof signupVerificationCreateFormSchema>;
export type DeviceCodeFormValues = v.InferInput<typeof deviceCodeFormSchema>;
export type OnboardingOrganizationFormValues = v.InferInput<
  typeof onboardingOrganizationFormSchema
>;

export function formString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function normalizeEmail(value: unknown): string {
  return formString(value).trim().toLowerCase();
}

export function normalizeHumanText(value: unknown): string {
  return formString(value).trim().replace(/\s+/g, " ");
}

export function normalizeDeviceCode(value: string): string {
  return value.trim().replace(/\s+/g, "-").toUpperCase();
}

export function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/-{2,}/g, "-")
    .slice(0, 80)
    .replace(/-+$/g, "");
}
