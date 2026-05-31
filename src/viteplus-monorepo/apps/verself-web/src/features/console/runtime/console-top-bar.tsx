import { Link } from "@tanstack/react-router";
import { WingsArgent } from "@verself/brand";
import { Plug } from "lucide-react";

export function ConsoleTopBar({
  orgSlug,
  title,
}: {
  readonly orgSlug: string;
  readonly title: string;
}) {
  return (
    <div className="flex h-11 items-center justify-between">
      <Link
        aria-label="Account"
        className="inline-flex h-10 items-center gap-2 rounded-full bg-white/[0.075] px-3 text-xs font-semibold text-white/82 shadow-[0_14px_34px_-28px_rgb(0_0_0/0.95)] backdrop-blur transition hover:bg-white/[0.11] hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
        params={{ orgSlug }}
        to="/$orgSlug/account/security"
      >
        <WingsArgent className="size-3.5" title="Guardian" viewBoxMode="cropped" />
        <span>Account</span>
      </Link>
      <div
        aria-label="Current page"
        className="pointer-events-none absolute left-1/2 max-w-36 -translate-x-1/2 truncate text-center text-sm font-semibold leading-none text-white/82"
      >
        {title}
      </div>
      <button
        aria-label="New"
        className="inline-flex h-10 items-center gap-2 rounded-full bg-white/[0.075] px-3 text-xs font-semibold text-white/82 shadow-[0_14px_34px_-28px_rgb(0_0_0/0.95)] backdrop-blur transition hover:bg-white/[0.11] hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
        type="button"
      >
        <span>New</span>
        <Plug className="size-3.5" aria-hidden="true" />
      </button>
    </div>
  );
}
