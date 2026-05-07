import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ClientOnly, Link, useHydrated } from "@tanstack/react-router";
import { SignedIn } from "@verself/auth-web/components";
import { useSignedInAuth } from "@verself/auth-web/react";
import { cn } from "@verself/ui/lib/utils";
import { SidebarTrigger, useSidebar } from "@verself/ui/components/ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@verself/ui/components/ui/dropdown-menu";
import { ChevronsUpDown, Plus, Search, Triangle } from "lucide-react";
import {
  NotificationBell,
  NotificationBellFallback,
} from "~/features/notifications/notification-bell";
import { projectsQuery } from "~/features/projects/queries";
import { orgPath, useOrgRouteParams } from "./org-routing";

// Top bar carries the expand/collapse trigger when the rail is hidden or
// collapsed: on mobile (rail is an off-canvas drawer that the user can't
// otherwise reveal) and on desktop when the rail is in icon mode (per
// Image #5, the expander sits *outside* the narrow icon rail). When the
// rail is expanded on desktop the trigger lives inside the rail header
// and the top bar omits it.
export function ShellTopBar({ onOpenPalette }: { readonly onOpenPalette: () => void }) {
  return (
    <header className="sticky top-0 z-20 flex h-12 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60 md:px-6">
      <TopBarSidebarTrigger />
      <ProjectSwitcherSlot />
      <div className="flex flex-1 items-center justify-end gap-2">
        <OmniBar onOpen={onOpenPalette} />
        <NotificationBellSlot />
      </div>
    </header>
  );
}

function TopBarSidebarTrigger() {
  const { state, isMobile } = useSidebar();
  // Show on mobile (rail is an off-canvas Sheet — no other way to open it)
  // and on desktop when collapsed (icon rail has no room for the trigger).
  if (!isMobile && state === "expanded") return null;
  return (
    <SidebarTrigger data-testid="shell-sidebar-trigger-topbar" className="text-muted-foreground" />
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

function ProjectSwitcherSlot() {
  return (
    <SignedIn>
      <ClientOnly fallback={<ProjectSwitcherFallback />}>
        <ProjectSwitcher />
      </ClientOnly>
    </SignedIn>
  );
}

function ProjectSwitcher() {
  const auth = useSignedInAuth();
  const hydrated = useHydrated();
  const { orgSlug } = useOrgRouteParams();
  const [query, setQuery] = useState("");
  const { data } = useQuery({
    ...projectsQuery(auth),
    enabled: hydrated,
  });
  const projects = data?.projects ?? [];
  const visibleProjects = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return projects;
    return projects.filter((project) => {
      const label = project.display_name || project.slug;
      return (
        label.toLowerCase().includes(normalized) || project.slug.toLowerCase().includes(normalized)
      );
    });
  }, [projects, query]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="flex h-9 shrink-0 items-center gap-1.5 rounded-md bg-accent px-3 text-sm font-medium text-accent-foreground transition-colors hover:bg-accent/80 data-popup-open:bg-accent/80"
          >
            <span>All Projects</span>
            <ChevronsUpDown className="size-3.5 text-muted-foreground" />
          </button>
        }
      />
      <DropdownMenuContent
        side="bottom"
        align="start"
        sideOffset={5}
        className="w-[24rem] overflow-hidden rounded-lg border border-white/10 bg-[#050505] p-0 text-[#ededed] shadow-2xl"
      >
        <label className="m-2 flex h-9 items-center gap-2 rounded-md border border-white/10 bg-white/[0.03] px-3 text-white/55 focus-within:border-white/20 focus-within:text-white/70">
          <Search className="size-4 shrink-0" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Find Project..."
            className="min-w-0 flex-1 bg-transparent text-sm text-white outline-none placeholder:text-white/45"
          />
          <kbd className="rounded border border-white/15 px-1.5 py-0.5 font-sans text-[10px] font-medium text-white/55">
            Esc
          </kbd>
        </label>

        <div className="max-h-72 overflow-y-auto px-2 pb-2">
          {visibleProjects.map((project, index) => {
            const label = project.display_name || project.slug;
            return (
              <DropdownMenuItem
                key={project.project_id}
                render={
                  <Link
                    to={orgPath(orgSlug, "/builds")}
                    className={cn(
                      "flex h-10 items-center gap-3 rounded-md px-2 text-sm text-white/85 focus:bg-white/10 focus:text-white",
                      index === 0 && !query && "bg-white/10 text-white",
                    )}
                    data-active={index === 0 && !query ? "true" : "false"}
                  >
                    <span className="flex size-5 items-center justify-center rounded-full bg-black text-white ring-1 ring-white/10">
                      <Triangle className="size-2.5 fill-current" />
                    </span>
                    <span className="min-w-0 flex-1 truncate">{label}</span>
                  </Link>
                }
              />
            );
          })}
          {visibleProjects.length === 0 ? (
            <div className="px-3 py-8 text-center text-sm text-white/50">No projects found.</div>
          ) : null}
        </div>

        <DropdownMenuSeparator className="mx-0 my-0 bg-white/10" />
        <button
          type="button"
          aria-disabled="true"
          className="flex h-14 w-full items-center gap-3 px-4 text-left text-sm text-white/80"
        >
          <Plus className="size-4 text-white/55" />
          <span>Create Project</span>
        </button>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ProjectSwitcherFallback() {
  return <div className="h-9 w-28 rounded-md bg-accent" aria-hidden="true" />;
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
