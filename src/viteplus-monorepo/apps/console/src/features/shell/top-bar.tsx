import { ClientOnly } from "@tanstack/react-router";
import { SignedIn } from "@forge-metal/auth-web/components";
import { SidebarTrigger } from "@forge-metal/ui/components/ui/sidebar";
import {
  NotificationBell,
  NotificationBellFallback,
} from "~/features/notifications/notification-bell";

export function ShellTopBar({ onOpenPalette }: { readonly onOpenPalette: () => void }) {
  return (
    <header className="sticky top-0 z-20 flex h-12 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 md:px-6">
      <SidebarTrigger className="-ml-1" data-testid="shell-sidebar-trigger" />
      <div className="flex flex-1 items-center justify-end gap-2">
        <OmniBar onOpen={onOpenPalette} />
        <NotificationBellSlot />
      </div>
    </header>
  );
}

function NotificationBellSlot() {
  return (
    <SignedIn>
      <ClientOnly fallback={<NotificationBellFallback />}>
        <NotificationBell />
      </ClientOnly>
    </SignedIn>
  );
}

function OmniBar({ onOpen }: { readonly onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="shell-omnibar"
      className="flex h-8 w-72 items-center gap-2 rounded-md border border-border/60 bg-background px-2.5 text-left text-xs text-muted-foreground transition-colors hover:border-border hover:bg-accent"
    >
      <span aria-hidden="true" className="text-muted-foreground/70">
        ⌕
      </span>
      <span className="flex-1 truncate">Search or jump to…</span>
      <kbd className="hidden rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground tabular-nums md:inline-block">
        ⌘K
      </kbd>
    </button>
  );
}
