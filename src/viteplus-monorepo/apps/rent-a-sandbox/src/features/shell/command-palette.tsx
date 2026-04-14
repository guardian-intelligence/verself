import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useClerk } from "@forge-metal/auth-web/react";
import { cn } from "@forge-metal/ui/lib/utils";
import { PRODUCT_NAV, SETTINGS_NAV } from "./nav-config";

// Hand-rolled command palette. We deliberately avoid cmdk + Radix Dialog:
// cmdk transitively depends on @radix-ui/react-dialog, which pulls in the
// theKashey focus-scope / react-remove-scroll / aria-hidden chain. Those
// packages ship CJS builds whose tslib UMD shape crashes nitro's SSR
// bundler at module-evaluation time (see auth-web commit 5aa2452). Every
// overlay in this app is hand-rolled until that chain is replaced upstream.

type CommandAction =
  | { readonly kind: "navigate"; readonly to: string }
  | { readonly kind: "external"; readonly href: string }
  | { readonly kind: "sign_out" };

type CommandEntry = {
  readonly id: string;
  readonly section: "Navigation" | "Settings" | "Account";
  readonly label: string;
  readonly description?: string;
  readonly keywords: string;
  readonly action: CommandAction;
};

function buildEntries(): readonly CommandEntry[] {
  const navEntries: CommandEntry[] = PRODUCT_NAV.map((product) => ({
    id: `nav:${product.id}`,
    section: "Navigation",
    label: `Go to ${product.label}`,
    keywords: `${product.label} ${product.to}`,
    action: { kind: "navigate", to: product.to },
  }));
  const settingsEntries: CommandEntry[] = SETTINGS_NAV.map((entry) => ({
    id: `settings:${entry.id}`,
    section: "Settings",
    label: `Settings · ${entry.label}`,
    keywords: `settings ${entry.label} ${entry.to}`,
    action: { kind: "navigate", to: entry.to },
  }));
  const accountEntries: CommandEntry[] = [
    {
      id: "account:sign_out",
      section: "Account",
      label: "Sign out",
      keywords: "sign out logout exit",
      action: { kind: "sign_out" },
    },
  ];
  return [...navEntries, ...settingsEntries, ...accountEntries];
}

function matchesQuery(entry: CommandEntry, query: string): boolean {
  if (!query) return true;
  const haystack = `${entry.label} ${entry.keywords}`.toLowerCase();
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  // Character-sequence match (every query char appears in order). Cheap
  // fuzziness without a dependency, good enough for a <50-entry palette.
  let searchStart = 0;
  for (const ch of needle) {
    const hit = haystack.indexOf(ch, searchStart);
    if (hit === -1) return false;
    searchStart = hit + 1;
  }
  return true;
}

type CommandPaletteControlProps = {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
};

export function CommandPalette({ open, onOpenChange }: CommandPaletteControlProps) {
  const navigate = useNavigate();
  const { redirectToSignOut } = useClerk();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);

  const allEntries = useMemo(() => buildEntries(), []);
  const filtered = useMemo(
    () => allEntries.filter((entry) => matchesQuery(entry, query)),
    [allEntries, query],
  );
  const grouped = useMemo(() => groupBySection(filtered), [filtered]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setCursor(0);
      // Defer focus one tick so the input is already mounted.
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  useEffect(() => {
    if (cursor >= filtered.length) {
      setCursor(filtered.length === 0 ? 0 : filtered.length - 1);
    }
  }, [filtered.length, cursor]);

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onOpenChange(false);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onOpenChange]);

  if (!open) return null;

  const runAction = async (action: CommandAction) => {
    onOpenChange(false);
    switch (action.kind) {
      case "navigate":
        await navigate({ to: action.to });
        return;
      case "external":
        window.location.href = action.href;
        return;
      case "sign_out":
        await redirectToSignOut();
        return;
    }
  };

  const handleListKeyDown = async (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setCursor((prev) => Math.min(prev + 1, filtered.length - 1));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setCursor((prev) => Math.max(prev - 1, 0));
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      const selected = filtered[cursor];
      if (selected) {
        await runAction(selected.action);
      }
      return;
    }
  };

  return (
    <div
      // Backdrop. Click closes. The dialog itself stops propagation so
      // clicks inside do not bubble up and dismiss.
      aria-hidden="true"
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 px-4 pt-[15vh]"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onOpenChange(false);
      }}
      data-testid="command-palette-backdrop"
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="w-full max-w-xl border border-foreground bg-background shadow-[4px_4px_0_0_rgba(0,0,0,1)]"
        onKeyDown={handleListKeyDown}
      >
        <div className="flex items-center gap-3 border-b border-foreground px-4 py-3">
          <span aria-hidden="true" className="font-mono text-xs uppercase tracking-wider">
            &gt;
          </span>
          <input
            ref={inputRef}
            type="text"
            placeholder="Search or jump to…"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setCursor(0);
            }}
            className="flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-muted-foreground"
            data-testid="command-palette-input"
            aria-label="Command palette input"
          />
          <kbd className="rounded border border-foreground px-1.5 py-0.5 font-mono text-[10px] uppercase">
            Esc
          </kbd>
        </div>
        <div className="max-h-[50vh] overflow-y-auto py-2" data-testid="command-palette-list">
          {filtered.length === 0 ? (
            <div className="px-4 py-6 text-center font-mono text-xs uppercase tracking-wider text-muted-foreground">
              No matches
            </div>
          ) : (
            grouped.map((group) => (
              <div key={group.section} className="mb-2 last:mb-0">
                <div className="px-4 py-1 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
                  {group.section}
                </div>
                {group.entries.map((entry) => {
                  const entryIndex = filtered.indexOf(entry);
                  const isActive = entryIndex === cursor;
                  return (
                    <button
                      key={entry.id}
                      type="button"
                      data-testid={`command-palette-item-${entry.id}`}
                      data-active={isActive ? "true" : undefined}
                      onMouseEnter={() => setCursor(entryIndex)}
                      onClick={() => {
                        void runAction(entry.action);
                      }}
                      className={cn(
                        "flex w-full items-center justify-between border-l-2 border-transparent px-4 py-2 text-left font-mono text-sm",
                        isActive ? "border-foreground bg-foreground/5" : "hover:bg-foreground/5",
                      )}
                    >
                      <span>{entry.label}</span>
                      {entry.description ? (
                        <span className="text-xs uppercase tracking-wider text-muted-foreground">
                          {entry.description}
                        </span>
                      ) : null}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>
        <div className="flex items-center justify-between border-t border-foreground px-4 py-2 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
          <span>Navigate: ↑ ↓</span>
          <span>Select: Enter</span>
          <span>Close: Esc</span>
        </div>
      </div>
    </div>
  );
}

type GroupedSection = { readonly section: CommandEntry["section"]; readonly entries: CommandEntry[] };

function groupBySection(entries: readonly CommandEntry[]): readonly GroupedSection[] {
  const order: CommandEntry["section"][] = ["Navigation", "Settings", "Account"];
  const buckets = new Map<CommandEntry["section"], CommandEntry[]>();
  for (const entry of entries) {
    const bucket = buckets.get(entry.section) ?? [];
    bucket.push(entry);
    buckets.set(entry.section, bucket);
  }
  return order
    .map((section) => ({ section, entries: buckets.get(section) ?? [] }))
    .filter((group) => group.entries.length > 0);
}

// useCommandPaletteHotkey registers the global Cmd/Ctrl+K binding. Lives in
// the consumer's tree so the palette state stays local to whatever mounts
// the shell, rather than globally mounted.
export function useCommandPaletteHotkey(onOpen: () => void) {
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpen();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onOpen]);
}

// Re-export so the shell can subscribe to route changes (used to close the
// palette on programmatic navigation when the route actually moves).
export function useRoutePath(): string {
  return useRouterState({ select: (s) => s.location.pathname });
}
