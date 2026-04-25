import { type FocusEvent, type ReactNode, useState } from "react";
import { useIsMutating, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { Button } from "@verself/ui/components/ui/button";
import { Input } from "@verself/ui/components/ui/input";
import { Label } from "@verself/ui/components/ui/label";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import { Select } from "@verself/ui/components/ui/select";
import { AutoSyncStatus } from "~/components/auto-sync-status";
import { ErrorCallout } from "~/components/error-callout";
import {
  profileMutationKeys,
  usePutProfilePreferencesMutation,
  useUpdateProfileIdentityMutation,
} from "~/features/profile/mutations";
import { loadProfilePage, profileQuery } from "~/features/profile/queries";
import { useAutoSyncForm } from "~/lib/auto-sync-form";
import type {
  ProfileSnapshot,
  PutProfilePreferencesRequest,
  UpdateProfileIdentityRequest,
} from "~/server-fns/api";

const LOCALE_OPTIONS = ["en-US", "en-GB", "de-DE", "fr-FR", "es-US"] as const;
const TIMEZONE_OPTIONS = [
  "UTC",
  "America/Los_Angeles",
  "America/Denver",
  "America/Chicago",
  "America/New_York",
  "Europe/London",
  "Europe/Berlin",
  "Asia/Tokyo",
] as const;
const DEFAULT_SURFACE_OPTIONS = ["executions", "schedules", "settings/profile"] as const;

export const Route = createFileRoute("/_shell/_authenticated/settings/profile")({
  loader: ({ context }) => loadProfilePage(context.queryClient, context.auth),
  component: ProfileSettings,
});

type IdentityForm = {
  display_name: string;
  family_name: string;
  given_name: string;
};

type PreferencesForm = {
  default_surface: string;
  locale: string;
  theme: "system" | "light" | "dark";
  time_display: "utc" | "local";
  timezone: string;
};

function ProfileSettings() {
  const auth = useSignedInAuth();
  const initial = Route.useLoaderData();
  const { data: profile } = useSuspenseQuery({
    ...profileQuery(auth),
    initialData: initial,
  });
  const activeSyncs = useIsMutating({ mutationKey: profileMutationKeys.all(auth) });

  return (
    <div className="flex flex-col gap-6">
      <PageSections>
        <IdentitySection profile={profile} />
        <PreferencesSection profile={profile} />
      </PageSections>
      <div className="flex justify-start">
        <AutoSyncStatus
          data-testid="profile-sync-status"
          state={activeSyncs > 0 ? "syncing" : "idle"}
          syncedAt={latestProfileSyncAt(profile)}
        />
      </div>
    </div>
  );
}

function IdentitySection({ profile }: { profile: ProfileSnapshot }) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();
  const mutation = useUpdateProfileIdentityMutation();
  const [refreshingLatest, setRefreshingLatest] = useState(false);
  const autosync = useAutoSyncForm<IdentityForm, UpdateProfileIdentityRequest, ProfileSnapshot>({
    formEqual: identityFormsEqual,
    formFromResult: identityFormFromProfile,
    initialForm: identityFormFromProfile(profile),
    initialVersion: profile.identity.version,
    isConflictError: isProfileVersionConflict,
    mutate: mutation.mutateAsync,
    requestFromForm: (form, version) => ({
      display_name: form.display_name,
      family_name: form.family_name,
      given_name: form.given_name,
      version,
    }),
    validate: validateIdentityForm,
    versionFromResult: (snapshot) => snapshot.identity.version,
  });
  const form = autosync.form;

  const syncLatestAndKeepDraft = async () => {
    setRefreshingLatest(true);
    try {
      const latest = await queryClient.fetchQuery(profileQuery(auth));
      autosync.rebase(latest);
      autosync.sync();
    } catch (error) {
      autosync.fail(error);
    } finally {
      setRefreshingLatest(false);
    }
  };

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Identity</SectionTitle>
          <SectionDescription>{profile.identity.email || "No primary email"}</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      <form
        aria-busy={autosync.status === "syncing" || undefined}
        className="grid gap-4 sm:grid-cols-2"
        data-testid="profile-identity-form"
        onBlur={(event) => {
          if (focusStayedInside(event)) return;
          autosync.sync();
        }}
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          autosync.sync();
        }}
      >
        <Field label="Given name" htmlFor="profile-given-name">
          <Input
            id="profile-given-name"
            autoComplete="given-name"
            value={form.given_name}
            onChange={(event) =>
              autosync.change((current) => ({ ...current, given_name: event.target.value }))
            }
          />
        </Field>
        <Field label="Family name" htmlFor="profile-family-name">
          <Input
            id="profile-family-name"
            autoComplete="family-name"
            value={form.family_name}
            onChange={(event) =>
              autosync.change((current) => ({ ...current, family_name: event.target.value }))
            }
          />
        </Field>
        <div className="sm:col-span-2">
          <Field label="Display name" htmlFor="profile-display-name">
            <Input
              id="profile-display-name"
              autoComplete="name"
              value={form.display_name}
              onChange={(event) =>
                autosync.change((current) => ({ ...current, display_name: event.target.value }))
              }
            />
          </Field>
        </div>
        <ProfileFormSyncError
          actionTestId="profile-identity-sync-latest"
          className="sm:col-span-2"
          error={autosync.error}
          onSyncLatest={() => {
            void syncLatestAndKeepDraft();
          }}
          syncing={refreshingLatest || autosync.status === "syncing"}
          testId="profile-identity-sync-error"
          title="Identity sync failed"
          variant={autosync.status === "conflict" ? "conflict" : "error"}
        />
      </form>
    </PageSection>
  );
}

function PreferencesSection({ profile }: { profile: ProfileSnapshot }) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();
  const mutation = usePutProfilePreferencesMutation();
  const [refreshingLatest, setRefreshingLatest] = useState(false);
  const autosync = useAutoSyncForm<PreferencesForm, PutProfilePreferencesRequest, ProfileSnapshot>({
    formEqual: preferenceFormsEqual,
    formFromResult: preferencesFormFromProfile,
    initialForm: preferencesFormFromProfile(profile),
    initialVersion: profile.preferences.version,
    isConflictError: isProfileVersionConflict,
    mutate: mutation.mutateAsync,
    requestFromForm: (form, version) => ({
      default_surface: form.default_surface,
      locale: form.locale,
      theme: form.theme,
      time_display: form.time_display,
      timezone: form.timezone,
      version,
    }),
    versionFromResult: (snapshot) => snapshot.preferences.version,
  });
  const form = autosync.form;

  const syncLatestAndKeepDraft = async () => {
    setRefreshingLatest(true);
    try {
      const latest = await queryClient.fetchQuery(profileQuery(auth));
      autosync.rebase(latest);
      autosync.sync();
    } catch (error) {
      autosync.fail(error);
    } finally {
      setRefreshingLatest(false);
    }
  };

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Preferences</SectionTitle>
          <SectionDescription>
            {form.locale} - {form.timezone}
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      <form
        aria-busy={autosync.status === "syncing" || undefined}
        className="grid gap-4 sm:grid-cols-2"
        data-testid="profile-preferences-form"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          autosync.sync();
        }}
      >
        <Field label="Locale" htmlFor="profile-locale">
          <Select
            id="profile-locale"
            value={form.locale}
            onChange={(event) => {
              const next = autosync.change((current) => ({
                ...current,
                locale: event.target.value,
              }));
              autosync.sync(next);
            }}
          >
            {includesOption(LOCALE_OPTIONS, form.locale) ? null : (
              <option value={form.locale}>{form.locale}</option>
            )}
            {LOCALE_OPTIONS.map((locale) => (
              <option key={locale} value={locale}>
                {locale}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Time zone" htmlFor="profile-timezone">
          <Select
            id="profile-timezone"
            value={form.timezone}
            onChange={(event) => {
              const next = autosync.change((current) => ({
                ...current,
                timezone: event.target.value,
              }));
              autosync.sync(next);
            }}
          >
            {includesOption(TIMEZONE_OPTIONS, form.timezone) ? null : (
              <option value={form.timezone}>{form.timezone}</option>
            )}
            {TIMEZONE_OPTIONS.map((timezone) => (
              <option key={timezone} value={timezone}>
                {timezone}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Time display" htmlFor="profile-time-display">
          <Select
            id="profile-time-display"
            value={form.time_display}
            onChange={(event) => {
              const next = autosync.change((current) => ({
                ...current,
                time_display: timeDisplayFromValue(event.target.value),
              }));
              autosync.sync(next);
            }}
          >
            <option value="utc">UTC</option>
            <option value="local">Local</option>
          </Select>
        </Field>
        <Field label="Theme" htmlFor="profile-theme">
          <Select
            id="profile-theme"
            value={form.theme}
            onChange={(event) => {
              const next = autosync.change((current) => ({
                ...current,
                theme: themeFromValue(event.target.value),
              }));
              autosync.sync(next);
            }}
          >
            <option value="system">System</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </Select>
        </Field>
        <div className="sm:col-span-2">
          <Field label="Default surface" htmlFor="profile-default-surface">
            <Select
              id="profile-default-surface"
              value={form.default_surface}
              onChange={(event) => {
                const next = autosync.change((current) => ({
                  ...current,
                  default_surface: event.target.value,
                }));
                autosync.sync(next);
              }}
            >
              {includesOption(DEFAULT_SURFACE_OPTIONS, form.default_surface) ? null : (
                <option value={form.default_surface}>{form.default_surface}</option>
              )}
              <option value="executions">Executions</option>
              <option value="schedules">Schedules</option>
              <option value="settings/profile">Profile settings</option>
            </Select>
          </Field>
        </div>
        <ProfileFormSyncError
          actionTestId="profile-preferences-sync-latest"
          className="sm:col-span-2"
          error={autosync.error}
          onSyncLatest={() => {
            void syncLatestAndKeepDraft();
          }}
          syncing={refreshingLatest || autosync.status === "syncing"}
          testId="profile-preferences-sync-error"
          title="Preferences sync failed"
          variant={autosync.status === "conflict" ? "conflict" : "error"}
        />
      </form>
    </PageSection>
  );
}

function ProfileFormSyncError({
  actionTestId,
  className,
  error,
  onSyncLatest,
  syncing,
  testId,
  title,
  variant,
}: {
  readonly actionTestId: string;
  readonly className?: string;
  readonly error: unknown;
  readonly onSyncLatest: () => void;
  readonly syncing: boolean;
  readonly testId: string;
  readonly title: string;
  readonly variant: "conflict" | "error";
}) {
  if (!error) {
    return null;
  }

  if (variant === "conflict") {
    return (
      <div className={className} data-testid={testId}>
        <ErrorCallout
          action={
            <Button
              data-testid={actionTestId}
              disabled={syncing}
              onClick={onSyncLatest}
              size="sm"
              type="button"
              variant="destructive"
            >
              Sync latest and keep my changes
            </Button>
          }
          error="Your draft is still in the form. Sync the latest version before saving these fields."
          title="Profile changed elsewhere"
        />
      </div>
    );
  }

  return (
    <div className={className} data-testid={testId}>
      <ErrorCallout error={error} title={title} />
    </div>
  );
}

function isProfileVersionConflict(error: unknown): boolean {
  if (error && typeof error === "object" && "status" in error && error.status === 409) {
    return true;
  }

  const message = error instanceof Error ? error.message : typeof error === "string" ? error : "";
  return (
    message.includes("Profile API 409") ||
    message.includes("profile resource version conflict") ||
    message.includes("profile-version-conflict")
  );
}

function Field({
  children,
  htmlFor,
  label,
}: {
  children: ReactNode;
  htmlFor: string;
  label: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  );
}

function identityFormFromProfile(profile: ProfileSnapshot): IdentityForm {
  return {
    display_name: profile.identity.display_name,
    family_name: profile.identity.family_name,
    given_name: profile.identity.given_name,
  };
}

function preferencesFormFromProfile(profile: ProfileSnapshot): PreferencesForm {
  return {
    default_surface: profile.preferences.default_surface || "executions",
    locale: profile.preferences.locale || "en-US",
    theme: profile.preferences.theme,
    time_display: profile.preferences.time_display,
    timezone: profile.preferences.timezone || "UTC",
  };
}

function identityFormsEqual(left: IdentityForm, right: IdentityForm): boolean {
  return (
    left.display_name === right.display_name &&
    left.family_name === right.family_name &&
    left.given_name === right.given_name
  );
}

function preferenceFormsEqual(left: PreferencesForm, right: PreferencesForm): boolean {
  return (
    left.default_surface === right.default_surface &&
    left.locale === right.locale &&
    left.theme === right.theme &&
    left.time_display === right.time_display &&
    left.timezone === right.timezone
  );
}

function validateIdentityForm(form: IdentityForm): string | null {
  if (!form.given_name.trim()) return "Given name is required.";
  if (!form.family_name.trim()) return "Family name is required.";
  return null;
}

function latestProfileSyncAt(profile: ProfileSnapshot): string | undefined {
  return latestTimestamp(profile.identity.synced_at, profile.preferences.updated_at);
}

function latestTimestamp(...values: ReadonlyArray<string | undefined>): string | undefined {
  let latestValue: string | undefined;
  let latestTime = Number.NEGATIVE_INFINITY;
  for (const value of values) {
    if (!value) continue;
    const time = new Date(value).getTime();
    if (Number.isNaN(time) || time <= latestTime) continue;
    latestTime = time;
    latestValue = value;
  }
  return latestValue;
}

function includesOption(options: ReadonlyArray<string>, value: string): boolean {
  return options.includes(value);
}

function timeDisplayFromValue(value: string): PreferencesForm["time_display"] {
  if (value === "utc" || value === "local") return value;
  throw new Error(`unknown profile time display option: ${value}`);
}

function themeFromValue(value: string): PreferencesForm["theme"] {
  if (value === "system" || value === "light" || value === "dark") return value;
  throw new Error(`unknown profile theme option: ${value}`);
}

function focusStayedInside(event: FocusEvent<HTMLElement>): boolean {
  const nextFocus = event.relatedTarget;
  return isNode(nextFocus) && event.currentTarget.contains(nextFocus);
}

function isNode(value: EventTarget | null): value is Node {
  return typeof Node !== "undefined" && value instanceof Node;
}
