import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Save } from "lucide-react";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Input } from "@forge-metal/ui/components/ui/input";
import { Label } from "@forge-metal/ui/components/ui/label";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { Select } from "@forge-metal/ui/components/ui/select";
import { ErrorCallout } from "~/components/error-callout";
import {
  usePutProfilePreferencesMutation,
  useUpdateProfileIdentityMutation,
} from "~/features/profile/mutations";
import { loadProfilePage, profileQuery } from "~/features/profile/queries";
import type { ProfileSnapshot } from "~/server-fns/api";

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

  return (
    <PageSections>
      <IdentitySection profile={profile} />
      <PreferencesSection profile={profile} />
    </PageSections>
  );
}

function IdentitySection({ profile }: { profile: ProfileSnapshot }) {
  const mutation = useUpdateProfileIdentityMutation();
  const [form, setForm] = useState<IdentityForm>(() => identityFormFromProfile(profile));

  useEffect(() => {
    setForm(identityFormFromProfile(profile));
  }, [profile]);

  const hasChanges = useMemo(() => {
    const next = identityFormFromProfile(profile);
    return (
      form.display_name !== next.display_name ||
      form.family_name !== next.family_name ||
      form.given_name !== next.given_name
    );
  }, [form, profile]);

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Identity</SectionTitle>
          <SectionDescription>{profile.identity.email || "No primary email"}</SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault();
          mutation.mutate({
            display_name: form.display_name,
            family_name: form.family_name,
            given_name: form.given_name,
            version: profile.identity.version,
          });
        }}
      >
        <Field label="Given name" htmlFor="profile-given-name">
          <Input
            id="profile-given-name"
            autoComplete="given-name"
            value={form.given_name}
            onChange={(event) =>
              setForm((current) => ({ ...current, given_name: event.target.value }))
            }
          />
        </Field>
        <Field label="Family name" htmlFor="profile-family-name">
          <Input
            id="profile-family-name"
            autoComplete="family-name"
            value={form.family_name}
            onChange={(event) =>
              setForm((current) => ({ ...current, family_name: event.target.value }))
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
                setForm((current) => ({ ...current, display_name: event.target.value }))
              }
            />
          </Field>
        </div>
        <div className="flex flex-col gap-3 sm:col-span-2">
          {mutation.error ? (
            <ErrorCallout error={mutation.error} title="Identity update failed" />
          ) : null}
          <Button
            type="submit"
            className="w-full sm:w-fit"
            disabled={!hasChanges || mutation.isPending}
          >
            <Save className="size-4" />
            {mutation.isPending ? "Saving…" : "Save identity"}
          </Button>
        </div>
      </form>
    </PageSection>
  );
}

function PreferencesSection({ profile }: { profile: ProfileSnapshot }) {
  const mutation = usePutProfilePreferencesMutation();
  const [form, setForm] = useState<PreferencesForm>(() => preferencesFormFromProfile(profile));

  useEffect(() => {
    setForm(preferencesFormFromProfile(profile));
  }, [profile]);

  const hasChanges = useMemo(() => {
    const next = preferencesFormFromProfile(profile);
    return (
      form.default_surface !== next.default_surface ||
      form.locale !== next.locale ||
      form.theme !== next.theme ||
      form.time_display !== next.time_display ||
      form.timezone !== next.timezone
    );
  }, [form, profile]);

  return (
    <PageSection>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Preferences</SectionTitle>
          <SectionDescription>
            {form.locale} · {form.timezone}
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>

      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault();
          mutation.mutate({
            default_surface: form.default_surface,
            locale: form.locale,
            theme: form.theme,
            time_display: form.time_display,
            timezone: form.timezone,
            version: profile.preferences.version,
          });
        }}
      >
        <Field label="Locale" htmlFor="profile-locale">
          <Select
            id="profile-locale"
            value={form.locale}
            onChange={(event) => setForm((current) => ({ ...current, locale: event.target.value }))}
          >
            {LOCALE_OPTIONS.includes(form.locale as (typeof LOCALE_OPTIONS)[number]) ? null : (
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
            onChange={(event) =>
              setForm((current) => ({ ...current, timezone: event.target.value }))
            }
          >
            {TIMEZONE_OPTIONS.includes(
              form.timezone as (typeof TIMEZONE_OPTIONS)[number],
            ) ? null : (
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
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                time_display: event.target.value as PreferencesForm["time_display"],
              }))
            }
          >
            <option value="utc">UTC</option>
            <option value="local">Local</option>
          </Select>
        </Field>
        <Field label="Theme" htmlFor="profile-theme">
          <Select
            id="profile-theme"
            value={form.theme}
            onChange={(event) =>
              setForm((current) => ({
                ...current,
                theme: event.target.value as PreferencesForm["theme"],
              }))
            }
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
              onChange={(event) =>
                setForm((current) => ({ ...current, default_surface: event.target.value }))
              }
            >
              {["executions", "schedules", "settings/profile"].includes(
                form.default_surface,
              ) ? null : (
                <option value={form.default_surface}>{form.default_surface}</option>
              )}
              <option value="executions">Executions</option>
              <option value="schedules">Schedules</option>
              <option value="settings/profile">Profile settings</option>
            </Select>
          </Field>
        </div>
        <div className="flex flex-col gap-3 sm:col-span-2">
          {mutation.error ? (
            <ErrorCallout error={mutation.error} title="Preferences update failed" />
          ) : null}
          <Button
            type="submit"
            className="w-full sm:w-fit"
            disabled={!hasChanges || mutation.isPending}
          >
            <Save className="size-4" />
            {mutation.isPending ? "Saving…" : "Save preferences"}
          </Button>
        </div>
      </form>
    </PageSection>
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
