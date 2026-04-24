import type { ClientUser } from "@forge-metal/auth-web/isomorphic";
import type { ProfileSnapshot } from "~/server-fns/api";

export type AccountChrome = {
  readonly displayName: string;
  readonly email: string;
  readonly initials: string;
  readonly source: "auth" | "profile" | "signed-out";
};

export const signedOutAccountChrome: AccountChrome = {
  displayName: "Signed out",
  email: "",
  initials: "?",
  source: "signed-out",
};

export function accountChromeFromProfile(
  profile: ProfileSnapshot,
  fallbackUser: ClientUser | null,
): AccountChrome {
  const email = nonEmpty(profile.identity.email) ?? nonEmpty(fallbackUser?.email) ?? "";
  const displayName =
    nonEmpty(profile.identity.display_name) ??
    nonEmpty(fallbackUser?.name) ??
    nonEmpty(fallbackUser?.preferredUsername) ??
    nonEmpty(email) ??
    "Signed in";

  return {
    displayName,
    email,
    initials: initialsFor(displayName),
    source: "profile",
  };
}

export function accountChromeFromAuthUser(user: ClientUser | null): AccountChrome {
  if (!user) {
    return signedOutAccountChrome;
  }

  const email = nonEmpty(user.email) ?? "";
  const displayName =
    nonEmpty(user.name) ?? nonEmpty(user.preferredUsername) ?? nonEmpty(email) ?? "Signed in";

  return {
    displayName,
    email,
    initials: initialsFor(displayName),
    source: "auth",
  };
}

function nonEmpty(value: string | null | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function initialsFor(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  const parts = trimmed.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return `${parts[0]![0] ?? ""}${parts[1]![0] ?? ""}`.toUpperCase();
  }
  return trimmed.slice(0, 2).toUpperCase();
}
