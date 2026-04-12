import { useSuspenseQuery } from "@tanstack/react-query";
import { LogOutIcon, SettingsIcon } from "lucide-react";
import { type ReactNode, useMemo } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@forge-metal/ui/components/ui/avatar";
import { Button, type buttonVariants } from "@forge-metal/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@forge-metal/ui/components/ui/dropdown-menu";
import { useAuth, useClerk, useUser } from "../react.ts";
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
  /** Path the "Organization settings" menu item links to. Set to null to hide. */
  readonly organizationPath?: string | null;
  /** Path the "Sign out" menu item links to. */
  readonly signOutPath?: string;
  /** Optional avatar image URL — falls back to initials. */
  readonly imageUrl?: string;
}

export function UserButton({
  organizationPath = "/organization",
  signOutPath = "/logout",
  imageUrl,
}: UserButtonProps = {}) {
  const { user } = useUser();
  if (!user) return null;
  const display = user.name ?? user.preferredUsername ?? user.email ?? user.sub;
  const initials = initialsFor(user);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`Open user menu for ${display}`}
          className="rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <Avatar>
            {imageUrl ? <AvatarImage src={imageUrl} alt={display} /> : null}
            <AvatarFallback>{initials}</AvatarFallback>
          </Avatar>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuLabel className="flex flex-col gap-0.5">
          <span className="truncate font-medium">{display}</span>
          {user.email && user.email !== display ? (
            <span className="truncate text-xs font-normal text-muted-foreground">{user.email}</span>
          ) : null}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {organizationPath ? (
          <DropdownMenuItem asChild>
            <a href={organizationPath}>
              <SettingsIcon />
              Organization settings
            </a>
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem asChild>
          <a href={signOutPath}>
            <LogOutIcon />
            Sign out
          </a>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// usePermissions resolves the caller's permission set for the current
// organization. It uses the same suspense-loaded organization query the
// profile uses, so calling sites within the same suspense boundary share the
// same fetch.
export function usePermissions(): ReadonlySet<string> {
  // Importing useSignedInAuth lazily would create a cycle; this hook is
  // documented to require an authenticated subtree.
  const auth = useSignedInAuthInternal();
  const api = useIdentityApi();
  const organization = useSuspenseQuery(organizationQuery(auth, api)).data;
  return useMemo(() => new Set(organization.permissions), [organization.permissions]);
}

function useSignedInAuthInternal() {
  const auth = useAuth();
  if (!auth.isSignedIn) {
    throw new Error("usePermissions() requires a signed-in user");
  }
  return auth;
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
