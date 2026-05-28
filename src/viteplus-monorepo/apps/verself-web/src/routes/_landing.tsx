import { createFileRoute, useLocation, useNavigate, Outlet } from "@tanstack/react-router";
import * as React from "react";

import {
  AnchoredSheet,
  AnchoredSheetPrimaryActionButton,
  AnchoredSheetPrimaryActionSection,
  type AnchoredSheetRef,
} from "@verself/ui/components/ui/anchored-sheet";
import { MarketingLandingPage } from "~/features/landing/marketing-page";

const loginRoutePathname = "/login";

function isLoginRoute(pathname: string): boolean {
  return pathname === loginRoutePathname;
}

export const Route = createFileRoute("/_landing")({
  component: MarketingLandingLayout,
});

function MarketingLandingLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const sheetRef = React.useRef<AnchoredSheetRef>(null);
  const shouldPresentSheet = isLoginRoute(location.pathname);

  const openLoginRoute = React.useCallback(() => {
    void navigate({ to: loginRoutePathname });
  }, [navigate]);

  const closeLoginRoute = React.useCallback(() => {
    if (location.pathname !== "/") {
      void navigate({ to: "/" });
    }
  }, [location.pathname, navigate]);

  React.useLayoutEffect(() => {
    if (shouldPresentSheet) {
      sheetRef.current?.presentSheet();
      return;
    }
    sheetRef.current?.hideSheet();
  }, [shouldPresentSheet]);

  return (
    <>
      <MarketingLandingPage onPrimaryAction={openLoginRoute} />
      <AnchoredSheet
        ref={sheetRef}
        aria-label="Sign in options"
        defaultPresented={shouldPresentSheet}
        onSheetDismissed={closeLoginRoute}
      >
        <AnchoredSheetPrimaryActionSection>
          <AnchoredSheetPrimaryActionButton onClick={openLoginRoute}>
            <span className="flex w-full items-center justify-center gap-2">
              <GitHubIcon className="size-4" />
              <span>Sign in with GitHub</span>
            </span>
          </AnchoredSheetPrimaryActionButton>
        </AnchoredSheetPrimaryActionSection>
        <section className="px-5 pb-4 pt-3">
          <Outlet />
        </section>
      </AnchoredSheet>
    </>
  );
}

function GitHubIcon({ className }: { readonly className: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      fill="currentColor"
      viewBox="0 0 24 24"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path d="M12 0C5.373 0 0 5.373 0 12c0 5.303 3.438 9.8 8.207 11.387.6.113.793-.258.793-.577v-2.234c-3.338.726-4.043-1.416-4.043-1.416-.546-1.387-1.334-1.756-1.334-1.756-1.09-.745.083-.73.083-.73 1.205.085 1.839 1.24 1.839 1.24 1.07 1.834 2.809 1.304 3.492.997.107-.776.42-1.304.764-1.604-2.665-.303-5.466-1.333-5.466-5.931 0-1.312.469-2.385 1.236-3.221-.124-.303-.535-1.523.116-3.176 0 0 1.008-.322 3.301 1.23  .957-.266 1.984-.399 3-.399 1.016 0 2.043.133 3 .399 2.293-1.552 3.299-1.23 3.299-1.23.651 1.653.24 2.873.117 3.176.767.836 1.235 1.91 1.235 3.221 0 4.61-2.803 5.624-5.475 5.922.43.372.824 1.103.824 2.222v3.293c0 .322.193.697.801.577C20.565 21.798 24 17.302 24 12 24 5.373 18.627 0 12 0z" />
    </svg>
  );
}
