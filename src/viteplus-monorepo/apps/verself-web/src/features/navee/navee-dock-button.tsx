import { useLocation, useNavigate } from "@tanstack/react-router";
import { useNavee } from "./navee-context";

type DockTarget =
  | { readonly kind: "home"; readonly orgSlug: string }
  | { readonly kind: "unavailable" };

export function NaveeDockButton() {
  const location = useLocation();
  const navigate = useNavigate();
  const { dispatch, snapshot } = useNavee();
  const target = resolveDockTarget(location.pathname);

  return (
    <button
      aria-label={snapshot.message}
      className="navee-dock-button"
      data-navee-mode={snapshot.mode}
      disabled={target.kind === "unavailable"}
      onClick={() => {
        dispatch({ type: "dock.clicked" });
        if (target.kind === "home") {
          void navigate({
            params: { orgSlug: target.orgSlug },
            search: { app: "ci-flight" },
            to: "/$orgSlug",
          });
        }
      }}
      type="button"
    >
      <span className="relative z-10 font-mono text-xs font-semibold tracking-normal text-white">
        {snapshot.message}
      </span>
    </button>
  );
}

function resolveDockTarget(pathname: string): DockTarget {
  const [firstSegment = ""] = pathname.split("/").filter(Boolean);
  if (firstSegment === "") return { kind: "unavailable" };
  if (isPublicSegment(firstSegment)) return { kind: "unavailable" };
  return { kind: "home", orgSlug: firstSegment };
}

function isPublicSegment(segment: string): boolean {
  return (
    segment === "api" ||
    segment === "device" ||
    segment === "docs" ||
    segment === "forgot-password" ||
    segment === "invite" ||
    segment === "login" ||
    segment === "logout" ||
    segment === "policy" ||
    segment === "readyz" ||
    segment === "reset-password" ||
    segment === "scene" ||
    segment === "signup"
  );
}
