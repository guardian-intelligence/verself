import {
  createContext,
  type Dispatch,
  type ReactNode,
  useContext,
  useMemo,
  useReducer,
} from "react";
import { useLocation } from "@tanstack/react-router";
import {
  initialNaveeState,
  type NaveeEvent,
  type NaveeRoutePosture,
  naveeReducer,
  type NaveeSnapshot,
  resolveNaveeSnapshot,
} from "./navee-reducer";

type NaveeContextValue = {
  readonly dispatch: Dispatch<NaveeEvent>;
  readonly routePosture: NaveeRoutePosture;
  readonly snapshot: NaveeSnapshot;
};

const NaveeContext = createContext<NaveeContextValue | undefined>(undefined);

export function NaveeProvider({ children }: { readonly children: ReactNode }) {
  const location = useLocation();
  const [state, dispatch] = useReducer(naveeReducer, initialNaveeState);
  const routePosture = resolveRoutePosture(location.pathname);
  const snapshot = resolveNaveeSnapshot({ routePosture, state });
  const value = useMemo(
    () => ({ dispatch, routePosture, snapshot }),
    [dispatch, routePosture, snapshot],
  );

  return <NaveeContext.Provider value={value}>{children}</NaveeContext.Provider>;
}

export function useNavee(): NaveeContextValue {
  const value = useContext(NaveeContext);
  if (value === undefined) {
    throw new Error("useNavee must be used inside NaveeProvider.");
  }
  return value;
}

function resolveRoutePosture(pathname: string): NaveeRoutePosture {
  if (pathname === "/" || pathname === "/login" || pathname === "/login/email") {
    return "expanded";
  }
  if (isNonNaveePath(pathname)) {
    return "hidden";
  }
  return "docked";
}

function isNonNaveePath(pathname: string): boolean {
  return (
    pathname === "/device" ||
    pathname === "/forgot-password" ||
    pathname === "/invite" ||
    pathname === "/logout" ||
    pathname === "/readyz" ||
    pathname === "/reset-password" ||
    pathname === "/signup" ||
    pathname.startsWith("/api/") ||
    pathname.startsWith("/docs") ||
    pathname.startsWith("/policy") ||
    pathname.startsWith("/scene/") ||
    pathname.startsWith("/signup/")
  );
}
