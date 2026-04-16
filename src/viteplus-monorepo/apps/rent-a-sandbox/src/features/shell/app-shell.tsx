import { useEffect, useState } from "react";
import { ClientOnly, Link, Outlet, useHydrated, useRouterState } from "@tanstack/react-router";
import { LogOut } from "lucide-react";
import { SignedIn, SignedOut, SignInButton } from "@forge-metal/auth-web/components";
import { useClerk, useUser } from "@forge-metal/auth-web/react";
import { Avatar, AvatarFallback } from "@forge-metal/ui/components/ui/avatar";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Toaster } from "@forge-metal/ui/components/ui/sonner";
import { useBillingTierLabel } from "~/features/billing/use-billing-account";
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

export function AppShell() {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const path = useRouterState({ select: (s) => s.location.pathname });

  useCommandPaletteHotkey(() => setPaletteOpen((prev) => !prev));

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
      <Toaster />
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
  const tierLabel = useBillingTierLabel();

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
    <AccountTriggerContent display={display} initials={initials} tierLabel={tierLabel} />
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
  initials,
  tierLabel,
}: {
  display: string;
  initials: string;
  tierLabel: string | null;
}) {
  return (
    <>
      <Avatar className="size-8 shrink-0">
        <AvatarFallback className="text-xs">{initials}</AvatarFallback>
      </Avatar>
      <span className="min-w-0 flex-1 truncate text-left text-sm font-medium">{display}</span>
      {tierLabel ? (
        <Badge
          variant="secondary"
          data-testid="shell-account-tier"
          className="shrink-0 group-data-[collapsible=icon]:hidden"
        >
          {tierLabel}
        </Badge>
      ) : null}
    </>
  );
}

function TopBar({ onOpenPalette }: { onOpenPalette: () => void }) {
  // Top bar is chrome: it yields the primary visual weight to the page content.
  // Sidebar toggle hugs the left, omnibar is small and right-aligned, page
  // title on the route is what the user reads first.
  return (
    <header className="sticky top-0 z-20 flex h-12 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 md:px-6">
      <SidebarTrigger className="-ml-1" data-testid="shell-sidebar-trigger" />
      <div className="flex flex-1 items-center justify-end">
        <OmniBar onOpen={onOpenPalette} />
      </div>
    </header>
  );
}

function OmniBar({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="shell-omnibar"
      className="flex h-8 w-72 items-center gap-2 rounded-md border border-border/60 bg-background px-2.5 text-left text-xs text-muted-foreground transition-colors hover:border-border hover:bg-accent"
    >
      <span aria-hidden="true" className="text-muted-foreground/70">⌕</span>
      <span className="flex-1 truncate">Search or jump to…</span>
      <kbd className="hidden rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground tabular-nums md:inline-block">
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
