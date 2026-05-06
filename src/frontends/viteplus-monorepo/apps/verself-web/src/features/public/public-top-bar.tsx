import { Link } from "@tanstack/react-router";
import { ChevronDown, Search, Sparkles } from "lucide-react";

import { cn } from "@verself/ui/lib/utils";

type PublicNavItem = {
  readonly label: string;
  readonly to: string;
  readonly hasMenu?: boolean;
  readonly active?: boolean;
};

const NAV_ITEMS: readonly PublicNavItem[] = [
  { label: "Products", to: "/docs", hasMenu: true },
  { label: "Resources", to: "/docs", hasMenu: true, active: true },
  { label: "Solutions", to: "/docs", hasMenu: true },
  { label: "Enterprise", to: "/docs" },
  { label: "Pricing", to: "/docs/pricing" },
] as const;

export function PublicTopBar() {
  return (
    <header className="sticky top-0 z-40 border-b border-white/10 bg-black text-white">
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-[1440px] items-center gap-6 px-4 md:px-6">
        <Link to="/" aria-label="Verself home" className="flex shrink-0 items-center">
          <VerselfWordmark />
        </Link>

        <nav className="hidden items-center gap-1 md:flex" aria-label="Primary">
          {NAV_ITEMS.map((item) => (
            <Link
              key={item.label}
              to={item.to}
              className={cn(
                "inline-flex h-9 items-center gap-1.5 rounded-full px-3 text-sm text-white/60 transition-colors hover:text-white",
                item.active && "bg-white/[0.13] text-white",
              )}
            >
              <span>{item.label}</span>
              {item.hasMenu && <ChevronDown className="size-3.5" aria-hidden="true" />}
            </Link>
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <Link
            to="/docs"
            className="hidden h-9 items-center gap-2 rounded-md border border-white/10 bg-white/[0.08] px-3 text-sm text-white/60 transition-colors hover:border-white/20 hover:text-white lg:flex"
          >
            <Search className="size-4" aria-hidden="true" />
            <span>Search Documentation</span>
            <kbd className="ml-2 rounded border border-white/10 bg-black/60 px-1.5 py-0.5 font-mono text-[11px] text-white/40">
              ⌘ K
            </kbd>
          </Link>
          <Link
            to="/docs"
            className="hidden h-9 items-center gap-2 rounded-md border border-white/15 px-3 text-sm font-medium text-white/90 transition-colors hover:bg-white/10 md:flex"
          >
            <Sparkles className="size-4" aria-hidden="true" />
            <span>Ask AI</span>
          </Link>
          <Link
            to="/login"
            search={{ redirect: undefined }}
            className="flex h-9 items-center rounded-md border border-white/15 px-3 text-sm font-medium text-white/90 transition-colors hover:bg-white/10"
          >
            Console
          </Link>
        </div>
      </div>
    </header>
  );
}

function VerselfWordmark() {
  return (
    <span className="inline-flex items-center text-[20px] font-semibold tracking-tight text-white">
      <span aria-hidden="true" className="mr-2 inline-block translate-y-px text-white">
        ▽
      </span>
      <span>Verself</span>
    </span>
  );
}
