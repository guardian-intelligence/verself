import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import * as React from "react";
import { Loader2 } from "lucide-react";

import {
  AnchoredSheet,
  AnchoredSheetPrimaryActionButton,
  AnchoredSheetPrimaryActionSection,
  type AnchoredSheetRef,
} from "@verself/ui/components/ui/anchored-sheet";
import { Button } from "@verself/ui/components/ui/button";
import { GitHubIcon } from "~/features/landing/github-icon";
import { MarketingLandingPage } from "~/features/landing/marketing-page";

const loginRoutePathname = "/login";
const loginEmailRoutePathname = "/login/email";

function isLoginRoute(pathname: string): boolean {
  return pathname === loginRoutePathname || pathname === loginEmailRoutePathname;
}

function isEmailLoginRoute(pathname: string): boolean {
  return pathname === loginEmailRoutePathname;
}

export const Route = createFileRoute("/_landing")({
  component: MarketingLandingLayout,
});

function MarketingLandingLayout() {
  const location = useLocation();
  const navigate = useNavigate();
  const sheetRef = React.useRef<AnchoredSheetRef>(null);
  const { pathname } = location;
  const shouldPresentSheet = isLoginRoute(pathname);
  const isEmailLogin = isEmailLoginRoute(pathname);
  const isGithubLogin = pathname === loginRoutePathname;

  const openLoginRoute = React.useCallback(() => {
    void navigate({ to: loginRoutePathname });
  }, [navigate]);

  const openLoginWithEmailRoute = React.useCallback(() => {
    void navigate({ to: loginEmailRoutePathname });
  }, [navigate]);

  const openGitHubSignIn = React.useCallback(() => {
    openLoginRoute();
  }, [openLoginRoute]);

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
      <MarketingLandingPage
        onSignInWithEmail={openLoginWithEmailRoute}
        onSignInWithGitHub={openGitHubSignIn}
        isGithubLogin={isGithubLogin}
        isEmailLogin={isEmailLogin}
      />
      <AnchoredSheet
        ref={sheetRef}
        aria-label="Sign in options"
        defaultPresented={shouldPresentSheet}
        onHide={closeLoginRoute}
        onSheetDismissed={closeLoginRoute}
        onPresent={openLoginRoute}
      >
        <div data-slot="anchored-sheet-content-outer" className="contents">
          <Outlet />
        </div>
        <AnchoredSheetPrimaryActionSection>
          {isEmailLogin ? (
            <Button
              onClick={openLoginRoute}
              size="default"
              variant="link"
              className="mb-3 block w-full self-center text-sm font-medium text-foreground/60 hover:text-foreground/80"
            >
              Sign in with GitHub
            </Button>
          ) : (
            <Button
              onClick={openLoginWithEmailRoute}
              size="default"
              variant="link"
              className="mb-3 block w-full self-center text-sm font-medium text-foreground/60 hover:text-foreground/80"
            >
              {isGithubLogin ? "Sign in with email instead" : "Sign in with email"}
            </Button>
          )}
          <AnchoredSheetPrimaryActionButton
            onClick={isEmailLogin ? openGitHubSignIn : openLoginRoute}
          >
            <span className="flex w-full items-center justify-center gap-2">
              {isGithubLogin ? (
                <>
                  <GitHubIcon className="size-4" />
                  <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                  <span>Dialing GitHub...</span>
                </>
              ) : isEmailLogin ? (
                "Sign In"
              ) : (
                <>
                  <GitHubIcon className="size-4" />
                  <span>Sign in with GitHub</span>
                </>
              )}
            </span>
          </AnchoredSheetPrimaryActionButton>
        </AnchoredSheetPrimaryActionSection>
      </AnchoredSheet>
    </>
  );
}
