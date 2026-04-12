import { useSuspenseQuery } from "@tanstack/react-query";
import { type ReactNode, useMemo } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@forge-metal/ui/components/ui/avatar";
import { Button, type buttonVariants } from "@forge-metal/ui/components/ui/button";
import { useAuth, useClerk, useSignedInAuth, useUser } from "../react.ts";
import { useIdentityApi } from "./identity-api.ts";
import { organizationQuery } from "./queries.ts";

type ButtonVariant = NonNullable<Parameters<typeof buttonVariants>[0]>["variant"];

// <SignedIn> and <SignedOut> mirror Clerk's auth-state slot components: render
// children only when the auth state matches. They never block rendering or
// trigger a redirect — that's the route guard's job.

export function SignedIn({ children }: { children: ReactNode }) {
  const auth = useAuth();
  return auth.isSignedIn ? <>{children}</> : null;
}

export function SignedOut({ children }: { children: ReactNode }) {
  const auth = useAuth();
  return auth.isSignedIn ? null : <>{children}</>;
}

export interface SignInButtonProps {
  readonly children?: ReactNode;
  readonly redirectTo?: string;
  readonly variant?: ButtonVariant;
  readonly className?: string;
}

export function SignInButton({
  children = "Sign in",
  redirectTo,
  variant = "default",
  className,
}: SignInButtonProps) {
  const { redirectToSignIn } = useClerk();
  return (
    <Button
      type="button"
      variant={variant}
      className={className}
      onClick={() => {
        void redirectToSignIn(redirectTo ? { redirectTo } : undefined);
      }}
    >
      {children}
    </Button>
  );
}

function initialsFor(input: { name?: string | null; email?: string | null }): string {
  const source = input.name?.trim() || input.email?.trim() || "";
  if (!source) return "?";
  const parts = source.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) {
    return `${parts[0]![0] ?? ""}${parts[1]![0] ?? ""}`.toUpperCase();
  }
  return source.slice(0, 2).toUpperCase();
}

export interface UserButtonProps {
  /** Path the avatar links to (the org page hosts both settings and sign out). */
  readonly organizationPath?: string;
  /** Optional avatar image URL — falls back to initials. */
  readonly imageUrl?: string;
}

// Minimal user-button: an avatar that links to the organization page where
// the signed-in user can manage their identity and sign out. We deliberately
// avoid a Radix DropdownMenu/Popover wrapper because every Radix overlay
// primitive transitively pulls in `aria-hidden` + the theKashey scroll-lock
// family, all of which use a tslib UMD CJS shape that nitro's SSR bundler
// cannot interop. Until vite-plus or nitro grow a fix, the dropdown remains
// off-table for the SSR import graph; the org page itself owns the menu.
export function UserButton({ organizationPath = "/organization", imageUrl }: UserButtonProps = {}) {
  const { user } = useUser();
  if (!user) return null;
  const display = user.name ?? user.preferredUsername ?? user.email ?? user.sub;
  const initials = initialsFor(user);
  return (
    <a
      href={organizationPath}
      aria-label={`Open organization settings for ${display}`}
      title={display}
      className="inline-flex rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
    >
      <Avatar>
        {imageUrl ? <AvatarImage src={imageUrl} alt={display} /> : null}
        <AvatarFallback>{initials}</AvatarFallback>
      </Avatar>
    </a>
  );
}

// usePermissions resolves the caller's permission set for the current
// organization. Inside a page that already loads the org via its loader
// (e.g. `<OrganizationProfile>`), this hits the React Query cache. On a page
// that does not, the first call triggers a server-fn fetch through the
// IdentityApiClient and suspends.
export function usePermissions(): ReadonlySet<string> {
  const auth = useSignedInAuth();
  const api = useIdentityApi();
  const organization = useSuspenseQuery(organizationQuery(auth, api)).data;
  return useMemo(() => new Set(organization.permissions), [organization.permissions]);
}

export interface ProtectProps {
  readonly permission: string;
  readonly children: ReactNode;
  readonly fallback?: ReactNode;
}

// <Protect permission="x:y:z"> renders children only if the caller has the
// named permission. UX gating only — the Go services remain the security
// boundary.
export function Protect({ permission, children, fallback = null }: ProtectProps) {
  const permissions = usePermissions();
  return permissions.has(permission) ? <>{children}</> : <>{fallback}</>;
}
