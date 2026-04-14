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
// Tailwind + local state, but the visual language is standard shadcn: soft
// rounding, `bg-card`/`bg-background`/`text-muted-foreground` tokens, no
// hand-rolled receipt chrome.

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
        <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 md:px-8 md:py-8">
          <Outlet />
        </main>
      </div>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

// DesktopSidebar is position:sticky + height:svh so as the main column
// scrolls, the rail stays pinned and the account footer is always in
// view. This is the same trick shadcn's own Sidebar uses, just without
// Radix overlays.
function DesktopSidebar({ path }: { path: string }) {
  return (
    <aside
      aria-label="Primary"
      className="sticky top-0 hidden h-svh w-60 shrink-0 flex-col border-r bg-card md:flex"
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
      <div className="relative flex h-full w-72 max-w-[85vw] flex-col border-r bg-card shadow-lg">
        {children}
      </div>
    </div>
  );
}

function SidebarContents({
  path,
  variant: _variant,
}: {
  path: string;
  variant: "desktop" | "mobile";
}) {
  return (
    <>
      <div className="flex h-14 items-center gap-2 border-b px-4">
        <span aria-hidden="true" className="text-sm font-semibold">
          ◼
        </span>
        <span className="text-sm font-semibold">Rent-a-Sandbox</span>
      </div>

      <nav className="flex-1 overflow-y-auto px-2 py-4" aria-label="Products">
        <div className="px-2 pb-1 text-xs font-medium text-muted-foreground">Products</div>
        <ul className="flex flex-col gap-1">
          {PRODUCT_NAV.map((product) => (
            <SidebarItem key={product.id} entry={product} active={isPathActive(path, product)} />
          ))}
        </ul>
      </nav>

      <div className="border-t p-2">
        <SidebarAccountRow />
      </div>
    </>
  );
}

function SidebarItem({ entry, active }: { entry: ProductNavEntry; active: boolean }) {
  return (
    <li>
      <Link
        to={entry.to}
        data-testid={`nav-${entry.id}`}
        className={cn(
          "flex h-9 items-center rounded-md px-3 text-sm",
          active
            ? "bg-accent font-medium text-accent-foreground"
            : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
        )}
      >
        {entry.label}
      </Link>
    </li>
  );
}

function SidebarAccountRow() {
  return (
    <>
      <SignedIn>
        <AccountMenu />
      </SignedIn>
      <SignedOut>
        <SignInButton variant="outline" className="w-full" />
      </SignedOut>
    </>
  );
}

function AccountMenu() {
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
        className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-accent"
      >
        <Avatar className="size-8 shrink-0">
          <AvatarFallback>{initials}</AvatarFallback>
        </Avatar>
        <div className="min-w-0 flex-1 text-sm">
          <div className="truncate font-medium">{display}</div>
          {email ? (
            <div className="truncate text-xs text-muted-foreground">{email}</div>
          ) : null}
        </div>
      </button>

      {open ? (
        <div
          role="menu"
          data-testid="shell-account-menu"
          className="absolute bottom-[calc(100%+0.25rem)] left-0 right-0 overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md"
        >
          <Link
            to="/settings/billing"
            role="menuitem"
            onClick={() => setOpen(false)}
            className="block px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground"
          >
            Settings
          </Link>
          <Link
            to="/settings/organization"
            role="menuitem"
            onClick={() => setOpen(false)}
            className="block px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground"
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
            className="block w-full px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground"
          >
            Sign out
          </button>
        </div>
      ) : null}
    </div>
  );
}

// Top bar lays the omnibar out in a three-column arrangement: mobile
// hamburger flush-left (mobile only), omnibar centred in the remaining
// space, and a right-side spacer that balances the hamburger so the
// omnibar stays visually centred on desktop too.
function TopBar({
  onOpenMenu,
  onOpenPalette,
}: {
  onOpenMenu: () => void;
  onOpenPalette: () => void;
}) {
  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 md:px-6">
      <div className="flex w-10 shrink-0 justify-start md:hidden">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onOpenMenu}
          aria-label="Open menu"
          data-testid="shell-mobile-menu-trigger"
        >
          <span aria-hidden="true" className="text-lg leading-none">
            ≡
          </span>
        </Button>
      </div>
      <div className="flex flex-1 justify-center">
        <OmniBar onOpen={onOpenPalette} />
      </div>
      <div className="hidden w-10 shrink-0 md:block" aria-hidden="true" />
    </header>
  );
}

function OmniBar({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="shell-omnibar"
      className="flex h-9 w-full max-w-lg items-center gap-2 rounded-md border bg-background px-3 text-left text-sm text-muted-foreground shadow-sm hover:bg-accent"
    >
      <span aria-hidden="true">⌕</span>
      <span className="flex-1 truncate">Search or jump to…</span>
      <kbd className="hidden rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground md:inline-block">
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
