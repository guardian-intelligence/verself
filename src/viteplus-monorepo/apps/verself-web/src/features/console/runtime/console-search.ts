import type { FrameKind } from "../flight/iphone-frame";

export const CONSOLE_APP_IDS = [
  "ci-flight",
  "golden-environments",
  "cache-health",
  "runner-capacity",
  "account-security",
] as const;

export type ConsoleAppId = (typeof CONSOLE_APP_IDS)[number];
export type ConsoleDock = "notifications" | "account";
export type ConsoleBanner = "signin" | "upgrade" | "org";
export type ConsoleIntent = "login" | "select-account" | "checkout";

export type ConsoleSearch = {
  readonly app: ConsoleAppId;
  readonly panel?: string | undefined;
  readonly dock?: ConsoleDock | undefined;
  readonly banner?: ConsoleBanner | undefined;
  readonly focus?: string | undefined;
  readonly intent?: ConsoleIntent | undefined;
  readonly debug?: string | undefined;
  readonly flight?: string | undefined;
  readonly actor?: string | undefined;
  readonly src?: string | undefined;
  readonly dst?: string | undefined;
  readonly status?: string | undefined;
  readonly state?: string | undefined;
  readonly remaining?: string | undefined;
  readonly commits?: string | undefined;
  readonly frame?: FrameKind | undefined;
};

const DEFAULT_APP_ID: ConsoleAppId = "ci-flight";

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function oneOf<const TValues extends readonly string[]>(
  values: TValues,
  value: unknown,
): TValues[number] | undefined {
  return typeof value === "string" && values.includes(value) ? value : undefined;
}

function frameKind(value: unknown): FrameKind | undefined {
  return value === "iphone17" ? "iphone17" : undefined;
}

export function parseConsoleSearch(search: Record<string, unknown>): ConsoleSearch {
  return {
    app: oneOf(CONSOLE_APP_IDS, search.app) ?? DEFAULT_APP_ID,
    panel: stringValue(search.panel),
    dock: oneOf(["notifications", "account"] as const, search.dock),
    banner: oneOf(["signin", "upgrade", "org"] as const, search.banner),
    focus: stringValue(search.focus),
    intent: oneOf(["login", "select-account", "checkout"] as const, search.intent),
    debug: stringValue(search.debug),
    flight: stringValue(search.flight),
    actor: stringValue(search.actor),
    src: stringValue(search.src),
    dst: stringValue(search.dst),
    status: stringValue(search.status),
    state: stringValue(search.state),
    remaining: stringValue(search.remaining),
    commits: stringValue(search.commits),
    frame: frameKind(search.frame),
  };
}
