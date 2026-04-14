import { useEffect, useState } from "react";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { SignedIn, SignedOut, SignInButton } from "@forge-metal/auth-web/components";
import { useClerk, useUser } from "@forge-metal/auth-web/react";
import { cn } from "@forge-metal/ui/lib/utils";
import { Avatar, AvatarFallback } from "@forge-metal/ui/components/ui/avatar";
import { Button } from "@forge-metal/ui/components/ui/button";
import { PRODUCT_NAV, isPathActive, type ProductNavEntry } from "./nav-config";
import { CommandPalette, useCommandPaletteHotkey } from "./command-palette";

// Rent-a-Sandbox app shell. Hand-rolled on purpose — see command-palette.tsx
// for the explanation of why Radix overlays (and therefore shadcn Sidebar /
// Sheet / Dialog / DropdownMenu / Command) cannot appear in this app's SSR
// module graph. Every interactive surface in the shell is built on plain
// Tailwind + local state.

// Tablet (md) shows an icon-collapsed rail; desktop (lg+) expands it to a
// full-width labelled rail. Mobile renders an off-canvas drawer instead
// (see MobileDrawer below).
const SIDEBAR_WIDTH_TABLET = "md:w-14 lg:w-[240px]";

export function AppShell() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const path = useRouterState({ select: (s) => s.location.pathname });

  useCommandPaletteHotkey(() => setPaletteOpen(true));

  // Close the mobile drawer whenever the route actually changes so
  // navigation feels instant instead of stuck with an open overlay.
  useEffect(() => {
    setMobileOpen(false);
  }, [path]);

  return (
    <div className="relative flex min-h-svh bg-background text-foreground">
      <DesktopSidebar path={path} />
      <MobileDrawer open={mobileOpen} onClose={() => setMobileOpen(false)}>
        <SidebarContents path={path} variant="mobile" />
      </MobileDrawer>

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar
          onOpenMenu={() => setMobileOpen(true)}
          onOpenPalette={() => setPaletteOpen(true)}
        />
        <main
          id="main"
          className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 md:px-8 md:py-8"
        >
          <Outlet />
        </main>
      </div>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

function DesktopSidebar({ path }: { path: string }) {
  return (
    <aside
      aria-label="Primary"
      className={cn(
        "hidden shrink-0 flex-col border-r border-foreground bg-background md:flex",
        SIDEBAR_WIDTH_TABLET,
      )}
    >
      <SidebarContents path={path} variant="desktop" />
    </aside>
  );
}

function MobileDrawer({
  open,
  onClose,
  children,
}: {
  open: boolean;
  onClose: () => void;
  children: React.ReactNode;
}) {
  // Locks body scroll while the drawer is open. Cheaper than pulling in
  // react-remove-scroll (which is in the banned theKashey chain anyway).
  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-40 md:hidden" role="dialog" aria-modal="true" aria-label="Menu">
      <button
        type="button"
        aria-label="Close menu"
        onClick={onClose}
        className="absolute inset-0 bg-black/40"
      />
      <div className="relative flex h-full w-[280px] max-w-[85vw] flex-col border-r border-foreground bg-background">
        {children}
      </div>
    </div>
  );
}

function SidebarContents({
  path,
  variant,
}: {
  path: string;
  variant: "desktop" | "mobile";
}) {
  // On tablet (md) the desktop rail shrinks to icons; the wordmark collapses
  // to a single glyph. Mobile always renders the full label because it's an
  // explicit drawer.
  const showLabels = variant === "mobile" ? true : undefined;
  return (
    <>
      <div className="flex h-14 items-center gap-2 border-b border-foreground px-4">
        <span aria-hidden="true" className="font-mono text-sm font-semibold">
          ◼
        </span>
        <span
          className={cn(
            "font-mono text-xs font-semibold uppercase tracking-[0.2em]",
            showLabels ? "block" : "hidden lg:block",
          )}
        >
          Rent&#45;a&#45;Sandbox
        </span>
      </div>

      <nav className="flex-1 overflow-y-auto py-3" aria-label="Products">
        <SidebarSectionLabel showLabels={showLabels}>Products</SidebarSectionLabel>
        <ul className="flex flex-col">
          {PRODUCT_NAV.map((product) => (
            <SidebarItem
              key={product.id}
              entry={product}
              active={isPathActive(path, product)}
              showLabels={showLabels}
            />
          ))}
        </ul>
      </nav>

      <div className="border-t border-foreground">
        <SidebarAccountRow showLabels={showLabels} />
      </div>
    </>
  );
}

function SidebarSectionLabel({
  children,
  showLabels,
}: {
  children: React.ReactNode;
  showLabels: boolean | undefined;
}) {
  return (
    <div
      className={cn(
        "px-4 pb-2 font-mono text-[10px] uppercase tracking-[0.2em] text-muted-foreground",
        showLabels ? "block" : "hidden lg:block",
      )}
    >
      {children}
    </div>
  );
}

function SidebarItem({
  entry,
  active,
  showLabels,
}: {
  entry: ProductNavEntry;
  active: boolean;
  showLabels: boolean | undefined;
}) {
  return (
    <li>
      <Link
        to={entry.to}
        title={entry.label}
        data-testid={`nav-${entry.id}`}
        className={cn(
          "flex h-10 items-center gap-3 border-l-2 px-4 font-mono text-xs uppercase tracking-[0.15em]",
          active
            ? "border-foreground bg-foreground/5 font-semibold text-foreground"
            : "border-transparent text-muted-foreground hover:border-foreground/40 hover:text-foreground",
        )}
      >
        <span aria-hidden="true" className="font-mono text-sm">
          ▶
        </span>
        <span className={cn(showLabels ? "block" : "hidden lg:block")}>{entry.label}</span>
      </Link>
    </li>
  );
}

function SidebarAccountRow({ showLabels }: { showLabels: boolean | undefined }) {
  return (
    <>
      <SignedIn>
        <AccountMenu showLabels={showLabels} />
      </SignedIn>
      <SignedOut>
        <div className="px-4 py-3">
          <SignInButton variant="outline" className="w-full" />
        </div>
      </SignedOut>
    </>
  );
}

function AccountMenu({ showLabels }: { showLabels: boolean | undefined }) {
  const { user } = useUser();
  const { redirectToSignOut } = useClerk();
  const [open, setOpen] = useState(false);

  // Click-outside to close. Window listener is lighter than Radix's outside
  // press helper and avoids every overlay package in the tslib-incompatible
  // chain.
  useEffect(() => {
    if (!open) return;
    const handler = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      const root = document.querySelector("[data-shell-account-root]");
      if (root && !root.contains(target)) {
        setOpen(false);
      }
    };
    window.addEventListener("mousedown", handler);
    return () => window.removeEventListener("mousedown", handler);
  }, [open]);

  if (!user) return null;

  const display = user.name ?? user.preferredUsername ?? user.email ?? "Signed in";
  const email = user.email ?? "";
  const initials = initialsFor(display);

  return (
    <div data-shell-account-root className="relative">
      <button
        type="button"
        data-testid="shell-account-trigger"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((prev) => !prev)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-foreground/5"
      >
        <Avatar className="size-8 shrink-0 rounded-none border border-foreground">
          <AvatarFallback className="rounded-none bg-background font-mono text-[10px] uppercase tracking-wider">
            {initials}
          </AvatarFallback>
        </Avatar>
        <div
          className={cn(
            "min-w-0 flex-1 font-mono text-xs",
            showLabels ? "block" : "hidden lg:block",
          )}
        >
          <div className="truncate font-semibold uppercase tracking-wider">{display}</div>
          {email ? <div className="truncate text-muted-foreground">{email}</div> : null}
        </div>
      </button>

      {open ? (
        <div
          role="menu"
          data-testid="shell-account-menu"
          className="absolute bottom-[calc(100%+0.25rem)] left-2 right-2 border border-foreground bg-background shadow-[3px_3px_0_0_rgba(0,0,0,1)]"
        >
          <Link
            to="/settings/billing"
            role="menuitem"
            onClick={() => setOpen(false)}
            className="block border-b border-foreground/20 px-4 py-2 font-mono text-xs uppercase tracking-wider hover:bg-foreground/5"
          >
            Settings
          </Link>
          <Link
            to="/settings/organization"
            role="menuitem"
            onClick={() => setOpen(false)}
            className="block border-b border-foreground/20 px-4 py-2 font-mono text-xs uppercase tracking-wider hover:bg-foreground/5"
          >
            Manage organization
          </Link>
          <button
            type="button"
            role="menuitem"
            data-testid="shell-account-sign-out"
            onClick={() => {
              setOpen(false);
              void redirectToSignOut();
            }}
            className="block w-full px-4 py-2 text-left font-mono text-xs uppercase tracking-wider hover:bg-foreground/5"
          >
            Sign out
          </button>
        </div>
      ) : null}
    </div>
  );
}

function TopBar({
  onOpenMenu,
  onOpenPalette,
}: {
  onOpenMenu: () => void;
  onOpenPalette: () => void;
}) {
  return (
    <header className="flex h-14 items-center gap-3 border-b border-foreground bg-background px-4 md:px-6">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="md:hidden rounded-none"
        onClick={onOpenMenu}
        aria-label="Open menu"
        data-testid="shell-mobile-menu-trigger"
      >
        <span aria-hidden="true" className="font-mono text-lg leading-none">
          ≡
        </span>
      </Button>

      <OmniBar onOpen={onOpenPalette} />
    </header>
  );
}

function OmniBar({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="shell-omnibar"
      className="group flex h-9 w-full max-w-xl items-center gap-3 border border-foreground bg-background px-3 text-left font-mono text-xs uppercase tracking-wider text-muted-foreground hover:bg-foreground/5"
    >
      <span aria-hidden="true">⌕</span>
      <span className="flex-1 truncate normal-case tracking-normal">Search or jump to…</span>
      <kbd className="hidden border border-foreground px-1.5 py-0.5 text-[10px] font-semibold md:inline-block">
        ⌘K
      </kbd>
    </button>
  );
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
