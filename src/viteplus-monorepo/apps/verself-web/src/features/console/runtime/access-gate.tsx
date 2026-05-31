import { Link, useLocation } from "@tanstack/react-router";
import { LogIn, Sparkles, UsersRound } from "lucide-react";
import type { ReactNode } from "react";
import type { ConsoleAppDefinition } from "./app-types";
import {
  CONSOLE_APP_SURFACE_RADIUS_CLASS,
  CONSOLE_PRIMARY_BACKGROUND_HEX,
  CONSOLE_PRIMARY_TEXT_HEX,
} from "./console-layout";
import type { ConsoleBanner } from "./console-search";

export function AccessGate({
  app,
  banner,
  children,
  isSignedIn,
}: {
  readonly app: ConsoleAppDefinition;
  readonly banner?: ConsoleBanner | undefined;
  readonly children: ReactNode;
  readonly isSignedIn: boolean;
}) {
  const location = useLocation();
  const gate = gateFor({ app, banner, isSignedIn });
  if (!gate) return <>{children}</>;
  const Icon = gate.icon;
  return (
    <div
      className={`relative flex h-full flex-col overflow-hidden px-7 py-7 ${CONSOLE_APP_SURFACE_RADIUS_CLASS}`}
      style={{
        backgroundColor: CONSOLE_PRIMARY_BACKGROUND_HEX,
        color: CONSOLE_PRIMARY_TEXT_HEX,
      }}
    >
      <div className="relative z-10 flex items-start justify-between">
        <div>
          <p className="font-mono text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white/45">
            {app.eyebrow}
          </p>
          <h1 className="mt-3 max-w-60 text-[1.55rem] font-semibold leading-[1.03] tracking-normal">
            {gate.title}
          </h1>
        </div>
        <div className="flex size-10 items-center justify-center rounded-full border border-white/10 bg-white/[0.06] text-[#f1c767]">
          <Icon className="size-4" aria-hidden="true" />
        </div>
      </div>
      <p className="relative z-10 mt-5 max-w-64 text-sm font-medium leading-5 text-white/52">
        {gate.body}
      </p>
      <div className="relative z-10 mt-auto">
        {gate.kind === "signin" ? (
          <Link
            to="/login"
            search={{ redirect: location.href }}
            className="inline-flex h-10 items-center justify-center rounded-full bg-white px-5 text-sm font-semibold text-black transition hover:bg-white/88 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
          >
            Sign in
          </Link>
        ) : (
          <a
            className="inline-flex h-10 items-center justify-center rounded-full bg-white px-5 text-sm font-semibold text-black transition hover:bg-white/88 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
            href={gate.href}
          >
            {gate.action}
          </a>
        )}
      </div>
    </div>
  );
}

function gateFor({
  app,
  banner,
  isSignedIn,
}: {
  readonly app: ConsoleAppDefinition;
  readonly banner?: ConsoleBanner | undefined;
  readonly isSignedIn: boolean;
}): null | {
  readonly action: string;
  readonly body: string;
  readonly href: string;
  readonly icon: typeof LogIn;
  readonly kind: "signin" | "upgrade" | "org";
  readonly title: string;
} {
  if (!isSignedIn && app.access.kind !== "public") {
    return {
      action: "Sign in",
      body: app.access.signedOutBody ?? "This panel needs an authenticated Verself session.",
      href: "/login",
      icon: LogIn,
      kind: "signin",
      title: app.access.signedOutTitle ?? "Sign in required",
    };
  }
  if (banner === "upgrade" || app.access.kind === "entitled") {
    return {
      action: "Review plan",
      body: app.access.upgradeBody ?? "This panel needs a higher billing tier.",
      href: "/docs",
      icon: Sparkles,
      kind: "upgrade",
      title: app.access.upgradeTitle ?? "Upgrade required",
    };
  }
  if (banner === "org") {
    return {
      action: "Choose org",
      body: "Choose an organization before this panel can show live infrastructure data.",
      href: "/onboarding",
      icon: UsersRound,
      kind: "org",
      title: "Organization required",
    };
  }
  if (banner === "signin") {
    return {
      action: "Sign in",
      body: "Sign in to replace this preview with your live Verself data.",
      href: "/login",
      icon: LogIn,
      kind: "signin",
      title: "Sign in for live data",
    };
  }
  return null;
}
