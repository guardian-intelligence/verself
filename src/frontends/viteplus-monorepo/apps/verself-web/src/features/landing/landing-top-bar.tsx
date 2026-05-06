import { Link } from "@tanstack/react-router";
import {
  type CSSProperties,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useReducer,
  useRef,
} from "react";

// Vercel-style top bar with a single shared menu panel that morphs between
// triggers (the "Stripe nav" pattern). One panel is visible at a time; moving
// between triggers smoothly slides + resizes the panel toward the new
// content. Hovering off a trigger and onto the panel does NOT close.

interface MenuColumnLink {
  readonly title: string;
  readonly description?: string;
  readonly href?: string;
}

interface MenuColumn {
  readonly heading?: string;
  readonly links: ReadonlyArray<MenuColumnLink>;
}

interface MenuDefinition {
  readonly key: string;
  readonly label: string;
  readonly columns: ReadonlyArray<MenuColumn>;
}

type Action =
  | { readonly type: "trigger-enter"; readonly key: string }
  | { readonly type: "trigger-leave"; readonly key: string }
  | { readonly type: "panel-enter" }
  | { readonly type: "panel-leave" }
  | { readonly type: "close" };

interface State {
  readonly openKey: string | null;
  readonly previousKey: string | null;
  readonly direction: "left" | "right" | "none";
}

const MENUS: ReadonlyArray<MenuDefinition> = [
  {
    key: "products",
    label: "Products",
    columns: [
      {
        heading: "Compute",
        links: [
          { title: "CI Runner", description: "Firecracker-backed GitHub Actions replacement." },
          { title: "Lambda Sandboxes", description: "Short-lived invocation workloads." },
          { title: "Dev VMs", description: "Persistent sandboxes for everyday work." },
        ],
      },
      {
        heading: "Platform",
        links: [
          { title: "Bootstrap CLI", description: "Stand up your own installation." },
          { title: "Observability", description: "Logs, traces, metrics in one stack." },
          { title: "Secrets", description: "Opaque credential storage." },
        ],
      },
    ],
  },
  {
    key: "solutions",
    label: "Solutions",
    columns: [
      {
        heading: "By team",
        links: [
          { title: "Solo founders" },
          { title: "Platform engineering" },
          { title: "Compliance-led" },
        ],
      },
      {
        heading: "By use case",
        links: [
          { title: "Faster CI" },
          { title: "Self-hosted compute" },
          { title: "Forge clones" },
        ],
      },
    ],
  },
  {
    key: "resources",
    label: "Resources",
    columns: [
      {
        heading: "Learn",
        links: [{ title: "Docs" }, { title: "Architecture" }, { title: "Changelog" }],
      },
      {
        heading: "Connect",
        links: [{ title: "Status" }, { title: "Community" }, { title: "Security" }],
      },
    ],
  },
  {
    key: "enterprise",
    label: "Enterprise",
    columns: [
      {
        heading: "Enterprise",
        links: [{ title: "Single tenant" }, { title: "Procurement" }, { title: "Support tiers" }],
      },
    ],
  },
];

const SIMPLE_LINKS: ReadonlyArray<{ readonly key: string; readonly label: string }> = [
  { key: "docs", label: "Docs" },
  { key: "pricing", label: "Pricing" },
];

const HOVER_OPEN_DELAY_MS = 80;
const HOVER_CLOSE_DELAY_MS = 140;

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "trigger-enter": {
      if (state.openKey === action.key) return state;
      const direction = computeDirection(state.openKey, action.key);
      return { openKey: action.key, previousKey: state.openKey, direction };
    }
    case "trigger-leave":
      return state;
    case "panel-enter":
      return state;
    case "panel-leave":
      return state;
    case "close":
      return { openKey: null, previousKey: state.openKey, direction: "none" };
    default:
      return state;
  }
}

function computeDirection(prev: string | null, next: string): "left" | "right" | "none" {
  if (prev == null) return "none";
  const prevIndex = MENUS.findIndex((m) => m.key === prev);
  const nextIndex = MENUS.findIndex((m) => m.key === next);
  if (prevIndex === -1 || nextIndex === -1) return "none";
  if (nextIndex === prevIndex) return "none";
  return nextIndex > prevIndex ? "right" : "left";
}

export function LandingTopBar() {
  const [state, dispatch] = useReducer(reducer, {
    openKey: null,
    previousKey: null,
    direction: "none",
  });
  const openTimer = useRef<number | null>(null);
  const closeTimer = useRef<number | null>(null);
  const containerRef = useRef<HTMLElement>(null);
  const panelId = useId();

  const clearTimers = useCallback(() => {
    if (openTimer.current != null) {
      window.clearTimeout(openTimer.current);
      openTimer.current = null;
    }
    if (closeTimer.current != null) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);

  const scheduleOpen = useCallback(
    (key: string) => {
      clearTimers();
      openTimer.current = window.setTimeout(() => {
        dispatch({ type: "trigger-enter", key });
        openTimer.current = null;
      }, HOVER_OPEN_DELAY_MS);
    },
    [clearTimers],
  );

  const scheduleClose = useCallback(() => {
    if (openTimer.current != null) {
      window.clearTimeout(openTimer.current);
      openTimer.current = null;
    }
    closeTimer.current = window.setTimeout(() => {
      dispatch({ type: "close" });
      closeTimer.current = null;
    }, HOVER_CLOSE_DELAY_MS);
  }, []);

  useEffect(() => {
    return () => {
      clearTimers();
    };
  }, [clearTimers]);

  useEffect(() => {
    if (state.openKey == null) return;
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") dispatch({ type: "close" });
    };
    const handlePointerDown = (event: PointerEvent) => {
      const root = containerRef.current;
      if (!root) return;
      if (!root.contains(event.target as Node)) dispatch({ type: "close" });
    };
    window.addEventListener("keydown", handleKey);
    window.addEventListener("pointerdown", handlePointerDown);
    return () => {
      window.removeEventListener("keydown", handleKey);
      window.removeEventListener("pointerdown", handlePointerDown);
    };
  }, [state.openKey]);

  return (
    <header
      ref={containerRef}
      onPointerLeave={scheduleClose}
      className="pointer-events-auto absolute inset-x-0 top-0 z-30 flex h-14 items-center px-4 text-sm md:px-8"
    >
      <Link to="/" className="mr-8 flex items-center gap-2 text-white" aria-label="Verself">
        <VerselfWordmark />
      </Link>

      <nav className="hidden flex-1 items-center justify-center gap-1 md:flex" aria-label="Primary">
        {MENUS.map((menu) => (
          <MenuTrigger
            key={menu.key}
            menu={menu}
            isOpen={state.openKey === menu.key}
            panelId={panelId}
            onPointerEnter={() => scheduleOpen(menu.key)}
            onFocus={() => dispatch({ type: "trigger-enter", key: menu.key })}
          />
        ))}
        {SIMPLE_LINKS.map((link) => (
          <a
            key={link.key}
            href="#"
            onPointerEnter={() => {
              clearTimers();
              dispatch({ type: "close" });
            }}
            className="rounded-md px-3 py-2 text-white/70 transition-colors hover:text-white"
          >
            {link.label}
          </a>
        ))}
      </nav>

      <div className="ml-auto flex items-center gap-2">
        <Link
          to="/login"
          search={{ redirect: undefined }}
          className="rounded-md px-3 py-2 text-white/80 transition-colors hover:text-white"
        >
          Sign in
        </Link>
        <Link
          to="/login"
          search={{ redirect: undefined }}
          className="rounded-md bg-white px-3 py-2 font-medium text-black transition-colors hover:bg-white/90"
        >
          Get started
        </Link>
      </div>

      <MenuPanel
        state={state}
        panelId={panelId}
        onPointerEnter={clearTimers}
        onPointerLeave={scheduleClose}
      />
    </header>
  );
}

function MenuTrigger({
  isOpen,
  menu,
  onFocus,
  onPointerEnter,
  panelId,
}: {
  readonly isOpen: boolean;
  readonly menu: MenuDefinition;
  readonly onFocus: () => void;
  readonly onPointerEnter: () => void;
  readonly panelId: string;
}) {
  return (
    <button
      type="button"
      onPointerEnter={onPointerEnter}
      onFocus={onFocus}
      aria-expanded={isOpen}
      aria-controls={panelId}
      data-open={isOpen ? "" : undefined}
      className="rounded-md px-3 py-2 text-white/70 transition-colors hover:text-white data-[open]:text-white"
    >
      {menu.label}
    </button>
  );
}

function MenuPanel({
  onPointerEnter,
  onPointerLeave,
  panelId,
  state,
}: {
  readonly onPointerEnter: () => void;
  readonly onPointerLeave: () => void;
  readonly panelId: string;
  readonly state: State;
}) {
  const open = state.openKey != null;
  const measureRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useReducer(
    (_: { width: number; height: number }, n: { width: number; height: number }) => n,
    { width: 0, height: 0 },
  );

  useLayoutEffect(() => {
    const node = measureRef.current;
    if (!node) return;
    const r = node.getBoundingClientRect();
    setSize({ width: r.width, height: r.height });
  }, [state.openKey]);

  const sizeStyle: CSSProperties = open
    ? { width: size.width || "auto", height: size.height || "auto" }
    : { width: 0, height: 0 };

  return (
    <div
      id={panelId}
      role="region"
      aria-hidden={!open}
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
      className={[
        "pointer-events-auto absolute left-1/2 top-12 -translate-x-1/2",
        "overflow-hidden rounded-xl border border-white/10 bg-black/85 text-white shadow-2xl backdrop-blur-md",
        "transition-[opacity,width,height,transform] duration-200 ease-out",
        open ? "opacity-100" : "pointer-events-none opacity-0",
      ].join(" ")}
      style={sizeStyle}
    >
      {/* Measurement layer: invisible-but-laid-out copy of the active panel
          drives width/height for the smooth transition. The visible content
          slides in over it; React keys force exit/enter when the key changes. */}
      <div ref={measureRef} className="invisible" aria-hidden="true">
        {state.openKey ? (
          <MenuPanelContent menu={MENUS.find((m) => m.key === state.openKey)!} />
        ) : null}
      </div>
      <div className="absolute inset-0 overflow-hidden">
        {state.openKey ? (
          <div
            key={state.openKey}
            data-direction={state.direction}
            className="h-full w-full animate-in fade-in slide-in-from-right-2 data-[direction=left]:slide-in-from-left-2 data-[direction=none]:slide-in-from-bottom-1 duration-200"
          >
            <MenuPanelContent menu={MENUS.find((m) => m.key === state.openKey)!} />
          </div>
        ) : null}
      </div>
    </div>
  );
}

function MenuPanelContent({ menu }: { readonly menu: MenuDefinition }) {
  return (
    <div className="grid min-w-[420px] auto-cols-fr grid-flow-col gap-8 p-6">
      {menu.columns.map((col, idx) => (
        <div key={idx} className="min-w-[180px]">
          {col.heading ? (
            <div className="mb-3 text-xs font-medium uppercase tracking-wider text-white/40">
              {col.heading}
            </div>
          ) : null}
          <ul className="space-y-2">
            {col.links.map((link) => (
              <li key={link.title}>
                <a
                  href={link.href ?? "#"}
                  className="-mx-2 block rounded-md px-2 py-1.5 transition-colors hover:bg-white/5"
                >
                  <div className="text-sm text-white">{link.title}</div>
                  {link.description ? (
                    <div className="mt-0.5 text-xs text-white/50">{link.description}</div>
                  ) : null}
                </a>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function VerselfWordmark() {
  return (
    <span
      className="text-[15px] font-medium tracking-tight text-white"
      style={{ fontFamily: "Geist, sans-serif" }}
    >
      <span aria-hidden="true" className="mr-1.5 inline-block translate-y-px text-white">
        ▽
      </span>
      Verself
    </span>
  );
}
