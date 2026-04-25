import { useState } from "react";
import { Outlet, useRouterState } from "@tanstack/react-router";
import { ElapsedTimeProvider } from "@verself/ui/hooks/use-elapsed-time";
import { Toaster } from "@verself/ui/components/ui/sonner";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarProvider,
} from "@verself/ui/components/ui/sidebar";
import { EVERGREEN_NAV, PRIMARY_NAV } from "./nav-config";
import { CommandPalette, useCommandPaletteHotkey } from "./command-palette";
import { SidebarAccountSlot } from "./sidebar-account-menu";
import { SidebarNavGroup } from "./sidebar-nav";
import { ShellConfigProvider, useShellConfig } from "./shell-config";
import { ShellTopBar } from "./top-bar";

type PaletteState = {
  readonly locationPath: string | null;
  readonly open: boolean;
};

export function AppShell({ platformOrigin }: { platformOrigin: string }) {
  const palette = useCommandPaletteState();

  return (
    <ElapsedTimeProvider pollIntervalMs={1_000} justNowThresholdSeconds={3}>
      <ShellConfigProvider platformOrigin={platformOrigin}>
        <SidebarProvider>
          <AppSidebar />
          <SidebarInset>
            <ShellTopBar onOpenPalette={palette.openPalette} />
            <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 md:px-8 md:py-8">
              <Outlet />
            </main>
          </SidebarInset>
          <CommandPaletteLayer open={palette.open} onOpenChange={palette.setOpen} />
          <Toaster />
        </SidebarProvider>
      </ShellConfigProvider>
    </ElapsedTimeProvider>
  );
}

function AppSidebar() {
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex h-10 items-center gap-2 px-2">
          <span aria-hidden="true" className="text-base font-semibold">
            ◼
          </span>
          <span className="truncate text-base font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
            Console
          </span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarNavGroup entries={PRIMARY_NAV} />
        <SidebarNavGroup entries={EVERGREEN_NAV} anchor="bottom" />
      </SidebarContent>

      <SidebarFooter>
        <SidebarAccountSlot />
      </SidebarFooter>
    </Sidebar>
  );
}

function CommandPaletteLayer({
  onOpenChange,
  open,
}: {
  readonly onOpenChange: (open: boolean) => void;
  readonly open: boolean;
}) {
  const { platformOrigin } = useShellConfig();
  return <CommandPalette open={open} onOpenChange={onOpenChange} platformOrigin={platformOrigin} />;
}

function useCommandPaletteState() {
  const locationPath = useRouterState({ select: (s) => s.location.pathname });
  const [state, setState] = useState<PaletteState>({ locationPath: null, open: false });
  const open = state.open && state.locationPath === locationPath;

  const setOpen = (nextOpen: boolean) => {
    setState({ locationPath: nextOpen ? locationPath : null, open: nextOpen });
  };

  const openPalette = () => {
    setOpen(true);
  };

  useCommandPaletteHotkey(() => {
    setState((current) =>
      current.open && current.locationPath === locationPath
        ? { locationPath: null, open: false }
        : { locationPath, open: true },
    );
  });

  return { open, openPalette, setOpen };
}
