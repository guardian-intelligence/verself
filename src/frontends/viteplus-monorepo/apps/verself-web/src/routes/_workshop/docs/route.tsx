import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { Button } from "@verself/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@verself/ui/components/ui/sheet";
import { cn } from "@verself/ui/lib/utils";
import { useActiveAnchor } from "@verself/ui/hooks/use-active-anchor";
import { BookOpen, Copy, FileText, Menu, MessageSquare } from "lucide-react";

import { DOCS_NAV, type DocsNavChild, type DocsNavEntry, isPathActive } from "~/lib/docs-nav";

const DOCS_FEEDBACK_HREF =
  "mailto:shovon@guardianintelligence.org?subject=Verself%20docs%20feedback";

export const Route = createFileRoute("/_workshop/docs")({
  component: DocsLayout,
});

function DocsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const activeEntry = DOCS_NAV.find((entry) => isPathActive(path, entry)) ?? firstDocsEntry();

  return (
    <div className="mx-auto w-full max-w-[1440px]">
      <DocsMobileBar activeEntry={activeEntry} path={path} />
      <div className="grid grid-cols-1 lg:grid-cols-[280px_minmax(0,1fr)] xl:grid-cols-[280px_minmax(0,860px)_260px]">
        <DocsSidebar path={path} />
        <div className="min-w-0 border-border lg:border-l xl:border-r">
          <div
            data-docs-article
            className="mx-auto flex w-full max-w-3xl flex-col gap-10 px-4 py-8 md:px-6 lg:px-8 lg:py-10"
          >
            <Outlet />
            <DocsPagination activeEntry={activeEntry} />
          </div>
        </div>
        <DocsRightRail activeEntry={activeEntry} />
      </div>
    </div>
  );
}

function firstDocsEntry(): DocsNavEntry {
  const [entry] = DOCS_NAV;
  if (!entry) {
    throw new Error("Docs navigation is empty");
  }
  return entry;
}

function DocsMobileBar({ activeEntry, path }: { activeEntry: DocsNavEntry; path: string }) {
  return (
    <div
      data-testid="docs-mobile-bar"
      className="sticky top-[var(--header-scroll-offset)] z-20 flex items-center justify-between border-y border-border bg-[var(--treatment-ground)]/95 px-4 py-2 backdrop-blur lg:hidden"
    >
      <div className="flex min-w-0 items-center gap-2">
        <Sheet>
          <SheetTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Open docs navigation"
              />
            }
          >
            <Menu />
          </SheetTrigger>
          <SheetContent
            side="left"
            className="w-[320px] max-w-[85vw] bg-[var(--treatment-ground)] p-0 text-[var(--treatment-ink)]"
            showCloseButton
          >
            <SheetHeader className="border-b border-border p-4">
              <SheetTitle>Docs</SheetTitle>
            </SheetHeader>
            <nav aria-label="Docs sections" className="overflow-y-auto p-3">
              <DocsNavList path={path} variant="drawer" />
            </nav>
          </SheetContent>
        </Sheet>
        <BookOpen className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate text-sm font-medium">{activeEntry.label}</span>
      </div>
      <Button
        type="button"
        variant="outline"
        size="icon-sm"
        aria-label="Copy as Markdown"
        onClick={copyCurrentDocsPageAsMarkdown}
      >
        <FileText />
      </Button>
    </div>
  );
}

function DocsSidebar({ path }: { path: string }) {
  return (
    <aside className="hidden lg:sticky lg:top-[var(--header-scroll-offset)] lg:block lg:max-h-[calc(100svh-var(--header-scroll-offset))] lg:overflow-y-auto">
      <nav aria-label="Docs sections" className="px-6 py-6">
        <DocsNavList path={path} variant="sidebar" />
      </nav>
    </aside>
  );
}

function DocsNavList({ path, variant }: { path: string; variant: "sidebar" | "drawer" }) {
  return (
    <ul className="flex flex-col gap-1">
      {DOCS_NAV.map((entry) => (
        <li key={entry.id}>
          <DocsRailEntry entry={entry} path={path} variant={variant} />
          {entry.children && entry.children.length > 0 && isPathActive(path, entry) && (
            <DocsRailChildren children={entry.children} />
          )}
        </li>
      ))}
    </ul>
  );
}

function DocsRailEntry({
  entry,
  path,
  variant,
}: {
  entry: DocsNavEntry;
  path: string;
  variant: "sidebar" | "drawer";
}) {
  const active = isPathActive(path, entry);
  return (
    <Link
      to={entry.to}
      data-testid={`docs-nav-${entry.id}`}
      data-active={active ? "true" : "false"}
      className={cn(
        "flex min-h-8 items-center justify-between gap-3 rounded-md px-3 py-1.5 text-sm transition-colors",
        "data-[active=false]:text-muted-foreground data-[active=false]:hover:bg-accent/60 data-[active=false]:hover:text-foreground",
        "data-[active=true]:bg-accent data-[active=true]:font-medium data-[active=true]:text-foreground",
        variant === "drawer" && "text-[15px]",
      )}
    >
      <span className="truncate">{entry.label}</span>
      {entry.status === "coming-soon" && (
        <span className="shrink-0 rounded-sm border border-border px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase tracking-normal text-muted-foreground">
          Soon
        </span>
      )}
    </Link>
  );
}

function DocsRailChildren({ children }: { children: readonly DocsNavChild[] }) {
  const active = useActiveAnchor(children);

  return (
    <ul className="ml-3 mt-1 flex flex-col gap-0 border-l border-border">
      {children.map((child) => (
        <li key={child.id}>
          <DocsRailChildLink child={child} isActive={child.id === active} />
        </li>
      ))}
    </ul>
  );
}

function DocsRailChildLink({ child, isActive }: { child: DocsNavChild; isActive: boolean }) {
  return (
    <a
      href={`#${child.id}`}
      data-active={isActive ? "true" : "false"}
      data-testid={`docs-nav-child-${child.id}`}
      className={cn(
        "relative block whitespace-nowrap py-1.5 pl-3 pr-3 -ml-px text-sm transition-colors",
        "data-[active=false]:text-muted-foreground data-[active=false]:hover:text-[var(--treatment-ink)]",
        "data-[active=true]:font-medium data-[active=true]:text-[var(--treatment-ink)]",
        "data-[active=true]:before:absolute data-[active=true]:before:left-[-1px] data-[active=true]:before:top-1/2 data-[active=true]:before:h-4 data-[active=true]:before:w-px data-[active=true]:before:-translate-y-1/2 data-[active=true]:before:bg-[var(--treatment-ink)]",
      )}
    >
      {child.label}
    </a>
  );
}

function DocsRightRail({ activeEntry }: { activeEntry: DocsNavEntry }) {
  const activeChild = useActiveAnchor(activeEntry.children ?? []);

  return (
    <aside
      data-testid="docs-right-rail"
      className="hidden xl:sticky xl:top-[var(--header-scroll-offset)] xl:block xl:max-h-[calc(100svh-var(--header-scroll-offset))] xl:overflow-y-auto"
    >
      <div className="flex flex-col gap-6 px-6 py-10 text-sm">
        {activeEntry.children && activeEntry.children.length > 0 && (
          <nav aria-label="On this page" className="flex flex-col gap-3">
            <p className="text-xs font-medium text-muted-foreground">On this page</p>
            <ul className="flex flex-col gap-2">
              {activeEntry.children.map((child) => (
                <li key={child.id}>
                  <a
                    href={`#${child.id}`}
                    data-active={child.id === activeChild ? "true" : "false"}
                    className="block truncate text-muted-foreground transition-colors hover:text-[var(--treatment-ink)] data-[active=true]:font-medium data-[active=true]:text-[var(--treatment-ink)]"
                  >
                    {child.label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
        )}

        <div className="flex flex-col gap-2 border-t border-border pt-4">
          <button
            type="button"
            onClick={copyCurrentDocsPageAsMarkdown}
            className="flex items-center gap-2 rounded-md py-1 text-left text-muted-foreground transition-colors hover:text-[var(--treatment-ink)]"
          >
            <Copy className="size-4" />
            <span>Copy as Markdown</span>
          </button>
          <a
            href={DOCS_FEEDBACK_HREF}
            className="flex items-center gap-2 rounded-md py-1 text-muted-foreground transition-colors hover:text-[var(--treatment-ink)]"
          >
            <MessageSquare className="size-4" />
            <span>Give feedback</span>
          </a>
        </div>
      </div>
    </aside>
  );
}

function DocsPagination({ activeEntry }: { activeEntry: DocsNavEntry }) {
  const currentIndex = DOCS_NAV.findIndex((entry) => entry.id === activeEntry.id);
  const previous = currentIndex > 0 ? DOCS_NAV[currentIndex - 1] : undefined;
  const next =
    currentIndex >= 0 && currentIndex < DOCS_NAV.length - 1
      ? DOCS_NAV[currentIndex + 1]
      : undefined;

  if (!previous && !next) {
    return null;
  }

  return (
    <nav
      aria-label="Docs pagination"
      className="grid gap-3 border-t border-border pt-6 md:grid-cols-2"
    >
      {previous ? (
        <PaginationLink direction="Previous" entry={previous} />
      ) : (
        <div aria-hidden="true" />
      )}
      {next && <PaginationLink direction="Next" entry={next} align="end" />}
    </nav>
  );
}

function PaginationLink({
  direction,
  entry,
  align = "start",
}: {
  direction: "Previous" | "Next";
  entry: DocsNavEntry;
  align?: "start" | "end";
}) {
  return (
    <Link
      to={entry.to}
      className={cn(
        "group flex flex-col gap-1 rounded-md border border-border p-3 text-sm transition-colors hover:bg-accent/60",
        align === "end" && "items-end text-right",
      )}
    >
      <span className="text-xs font-medium text-muted-foreground">{direction}</span>
      <span className="font-medium text-[var(--treatment-ink)] group-hover:text-foreground">
        {entry.label}
      </span>
    </Link>
  );
}

function copyCurrentDocsPageAsMarkdown() {
  if (!navigator.clipboard) {
    throw new Error("Clipboard API is unavailable");
  }

  const article = document.querySelector("[data-docs-article]");
  if (!article) {
    throw new Error("Docs article root is missing");
  }

  const blocks = Array.from(article.querySelectorAll("h1, h2, h3, p, li, pre"))
    .map((element) => formatMarkdownBlock(element))
    .filter((text) => text.length > 0);
  const markdown = `${blocks.join("\n\n")}\n\nSource: ${window.location.href}\n`;

  void navigator.clipboard.writeText(markdown);
}

function formatMarkdownBlock(element: Element): string {
  const text = element.textContent?.replace(/\s+/g, " ").trim() ?? "";
  if (!text) {
    return "";
  }

  if (element instanceof HTMLHeadingElement) {
    const depth = Number(element.tagName.slice(1));
    return `${"#".repeat(depth)} ${text}`;
  }

  if (element.tagName === "LI") {
    return `- ${text}`;
  }

  if (element.tagName === "PRE") {
    return `\`\`\`\n${element.textContent?.trim() ?? ""}\n\`\`\``;
  }

  return text;
}
