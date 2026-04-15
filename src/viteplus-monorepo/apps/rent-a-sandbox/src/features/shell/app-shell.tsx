import { useEffect, useState } from "react";
import { ClientOnly, Link, Outlet, useHydrated, useRouterState } from "@tanstack/react-router";
import { LogOut } from "lucide-react";
import { SignedIn, SignedOut, SignInButton } from "@forge-metal/auth-web/components";
import { useClerk, useUser } from "@forge-metal/auth-web/react";
import { Avatar, AvatarFallback } from "@forge-metal/ui/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@forge-metal/ui/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@forge-metal/ui/components/ui/sidebar";
import { EVERGREEN_NAV, isPathActive, PRIMARY_NAV, type NavEntry } from "./nav-config";
import { CommandPalette, useCommandPaletteHotkey } from "./command-palette";

// Rent-a-Sandbox app shell, built on the Base UI Sidebar block shipped from
// @forge-metal/ui. The hand-rolled shell that preceded this file existed
// because Radix overlays were banned by the SSR module graph; commit
// 4d7567b moved the package to Base UI, but Base UI's Menu and Tooltip
// primitives are wrapped in @base-ui/utils fastComponent, which calls
// React.useSyncExternalStore through use-sync-external-store/shim. Under
// nitro SSR + Rolldown, that shim resolves through CJS createRequire and
// gets a duplicate React module instance whose hook dispatcher is null —
// the SSR render crashes with "Cannot read properties of null (reading
// 'useSyncExternalStore')". Tracked upstream at vitejs/rolldown-vite#596
// and mui/base-ui#3194, both closed but neither shipped a fix yet. We
// gate every Base UI Menu/Tooltip surface on hydration via ClientOnly /
// useHydrated until upstream resolves it. See AGENTS.md "Base UI gotchas".

export function AppShell() {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const path = useRouterState({ select: (s) => s.location.pathname });

  useCommandPaletteHotkey(() => setPaletteOpen(true));

  // Close the palette on route change so navigation feels instant instead
  // of leaving the overlay hanging.
  useEffect(() => {
    setPaletteOpen(false);
  }, [path]);

  return (
    <SidebarProvider>
      <AppSidebar path={path} />
      <SidebarInset>
        <TopBar onOpenPalette={() => setPaletteOpen(true)} />
        <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 md:px-8 md:py-8">
          <Outlet />
        </main>
      </SidebarInset>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </SidebarProvider>
  );
}

function AppSidebar({ path }: { path: string }) {
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex h-10 items-center gap-2 px-2">
          <span aria-hidden="true" className="text-base font-semibold">
            ◼
          </span>
          <span className="truncate text-base font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
            Rent-a-Sandbox
          </span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <NavMenu entries={PRIMARY_NAV} path={path} />
          </SidebarGroupContent>
        </SidebarGroup>

        {/* mt-auto anchors the evergreen group (Settings) to the bottom of
            the rail above the account footer, regardless of how many
            product rows are above it. */}
        <SidebarGroup className="mt-auto">
          <SidebarGroupContent>
            <NavMenu entries={EVERGREEN_NAV} path={path} />
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarAccountRow />
      </SidebarFooter>
    </Sidebar>
  );
}

function NavMenu({ entries, path }: { entries: readonly NavEntry[]; path: string }) {
  // SidebarMenuButton's tooltip prop wraps the button in Base UI's Tooltip
  // primitive (TooltipRoot/TooltipTrigger), which is a fastComponent and
  // crashes SSR. Gate the prop on hydration so the server renders without
  // it and the client adds collapsed-rail tooltips after mount.
  const hydrated = useHydrated();
  return (
    <SidebarMenu>
      {entries.map((entry) => {
        const active = isPathActive(path, entry);
        const Icon = entry.icon;
        return (
          <SidebarMenuItem key={entry.id}>
            <SidebarMenuButton
              isActive={active}
              {...(hydrated ? { tooltip: entry.label } : {})}
              render={
                <Link to={entry.to} data-testid={`nav-${entry.id}`}>
                  <Icon />
                  <span>{entry.label}</span>
                </Link>
              }
            />
          </SidebarMenuItem>
        );
      })}
    </SidebarMenu>
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

  if (!user) return null;

  const display = user.name ?? user.preferredUsername ?? user.email ?? "Signed in";
  const email = user.email ?? "";
  const initials = initialsFor(display);

  // ClientOnly fallback renders the trigger button only — no DropdownMenu
  // wrapper, no MenuRoot/MenuTrigger fastComponent, so SSR is safe. After
  // hydration the real DropdownMenu mounts with the same trigger shape so
  // there is no layout shift; the popover wires up its event listeners on
  // the client.
  const triggerContent = (
    <AccountTriggerContent display={display} email={email} initials={initials} />
  );

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <ClientOnly
          fallback={
            <SidebarMenuButton
              size="lg"
              variant="outline"
              data-testid="shell-account-trigger"
              disabled
              aria-busy="true"
            >
              {triggerContent}
            </SidebarMenuButton>
          }
        >
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <SidebarMenuButton
                  size="lg"
                  variant="outline"
                  data-testid="shell-account-trigger"
                  className="data-popup-open:bg-sidebar-accent"
                >
                  {triggerContent}
                </SidebarMenuButton>
              }
            />
            <DropdownMenuContent
              data-testid="shell-account-menu"
              side="top"
              align="end"
              sideOffset={8}
              className="min-w-60"
            >
              <DropdownMenuGroup>
                <DropdownMenuLabel className="flex flex-col gap-0.5">
                  <span className="truncate text-sm font-medium text-foreground">{display}</span>
                  {email ? (
                    <span className="truncate text-xs text-muted-foreground">{email}</span>
                  ) : null}
                </DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                data-testid="shell-account-sign-out"
                onClick={() => {
                  void redirectToSignOut();
                }}
              >
                <LogOut />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ClientOnly>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

function AccountTriggerContent({
  display,
  email,
  initials,
}: {
  display: string;
  email: string;
  initials: string;
}) {
  return (
    <>
      <Avatar className="size-8 shrink-0">
        <AvatarFallback className="text-xs">{initials}</AvatarFallback>
      </Avatar>
      <div className="grid min-w-0 flex-1 text-left leading-tight">
        <span className="truncate text-sm font-medium">{display}</span>
        {email ? (
          <span className="truncate text-xs text-muted-foreground">{email}</span>
        ) : null}
      </div>
    </>
  );
}

function TopBar({ onOpenPalette }: { onOpenPalette: () => void }) {
  return (
    <header className="sticky top-0 z-20 flex h-14 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 md:px-6">
      <SidebarTrigger className="-ml-1" data-testid="shell-sidebar-trigger" />
      <div className="flex flex-1 justify-center">
        <OmniBar onOpen={onOpenPalette} />
      </div>
      <div className="hidden w-9 shrink-0 md:block" aria-hidden="true" />
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
