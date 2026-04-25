import type { ClientUser } from "@forge-metal/auth-web/isomorphic";
import type { ProfileSnapshot } from "~/server-fns/api";

export type AccountChrome = {
  readonly displayName: string;
  readonly email: string;
  readonly initials: string;
  readonly source: "pending" | "profile";
};

export const pendingAccountChrome: AccountChrome = {
  displayName: "",
  email: "",
  initials: "",
  source: "pending",
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
