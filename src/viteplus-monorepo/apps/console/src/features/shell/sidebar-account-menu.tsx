import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClientOnly, Link, useHydrated, useRouter } from "@tanstack/react-router";
import { Building2, Check, ChevronDown, LoaderCircle, LogOut } from "lucide-react";
import {
  SignedIn,
  SignedOut,
  SignInButton,
  organizationMembersQuery,
  useIdentityApi,
} from "@verself/auth-web/components";
import { useClerk, useSignedInAuth, useUser } from "@verself/auth-web/react";
import type { AuthOrganizationContext } from "@verself/auth-web/isomorphic";
import { Avatar, AvatarFallback } from "@verself/ui/components/ui/avatar";
import { Button } from "@verself/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@verself/ui/components/ui/dropdown-menu";
import { profileQuery } from "~/features/profile/queries";
import {
  type AccountChrome,
  accountChromeFromProfile,
  pendingAccountChrome,
} from "./account-chrome";
import { selectActiveOrganization } from "~/server-fns/auth";

// Header trigger that lives at the top of the rail. Renders a compact "[avatar]
// Display name ▼" row and opens the org/account dropdown described in
// Image #4. The signed-out variant is a Sign-in CTA.
export function SidebarAccountTrigger() {
  return (
    <>
      <SignedIn>
        <SidebarAccountMenu />
      </SignedIn>
      <SignedOut>
        <div className="px-1">
          <SignInButton variant="outline" className="w-full" />
        </div>
      </SignedOut>
    </>
  );
}

function SidebarAccountMenu() {
  const account = useAccountChrome();
  const auth = useSignedInAuth();
  const userState = useUser();
  if (!userState.isSignedIn) {
    throw new Error("SidebarAccountMenu requires a signed-in user");
  }
  const user = userState.user;
  const { redirectToSignOut } = useClerk();
  const activeOrganization = user.availableOrganizations.find(
    (organization) => organization.orgID === auth.selectedOrgId,
  );
  const organizationSwitcher = useOrganizationSwitcher();
  const memberCount = useActiveMemberCount();

  const orgLabel = activeOrganization ? organizationLabel(activeOrganization) : null;
  const initials = account.initials || (orgLabel ? orgLabel.slice(0, 2).toUpperCase() : "?");

  return (
    <ClientOnly fallback={<TriggerSkeleton initials={initials} label={account.displayName} />}>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              data-testid="shell-account-trigger"
              data-account-source={account.source}
              aria-busy={account.source === "pending"}
              className="group/trigger flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 text-left text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground"
            >
              <Avatar className="size-5 shrink-0 rounded-md">
                <AvatarFallback className="rounded-md text-[10px] font-medium">
                  {initials}
                </AvatarFallback>
              </Avatar>
              <span
                className="min-w-0 flex-1 truncate text-sm font-medium group-data-[collapsible=icon]:hidden"
                data-testid="shell-account-display-name"
              >
                {account.displayName || orgLabel || "Account"}
              </span>
              <ChevronDown className="size-3.5 shrink-0 text-muted-foreground group-data-[collapsible=icon]:hidden" />
            </button>
          }
        />
        <DropdownMenuContent
          data-testid="shell-account-menu"
          side="bottom"
          align="start"
          sideOffset={6}
          className="w-64"
        >
          <div className="flex flex-col gap-3 px-2 py-2">
            <div className="flex items-center gap-2.5">
              <Avatar className="size-8 shrink-0 rounded-md">
                <AvatarFallback className="rounded-md text-xs font-medium">
                  {initials}
                </AvatarFallback>
              </Avatar>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-foreground">
                  {orgLabel ?? account.displayName}
                </div>
                <div
                  className="truncate text-xs text-muted-foreground"
                  data-testid="shell-active-organization"
                >
                  {orgLabel ? `Organization · ${formatMemberCount(memberCount)}` : account.email}
                </div>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <Button
                size="sm"
                variant="outline"
                render={
                  <Link to="/settings" data-testid="shell-account-settings">
                    Settings
                  </Link>
                }
              />
              <Button
                size="sm"
                variant="outline"
                render={
                  <Link to="/settings/organization" data-testid="shell-account-invite-members">
                    Invite members
                  </Link>
                }
              />
            </div>
          </div>
          {user.availableOrganizations.length > 1 ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel className="text-xs text-muted-foreground">
                  Switch organization
                </DropdownMenuLabel>
                {user.availableOrganizations.map((organization) => {
                  const isActive = organization.orgID === auth.selectedOrgId;
                  return (
                    <DropdownMenuItem
                      key={organization.orgID}
                      data-testid="shell-organization-switch-item"
                      data-org-id={organization.orgID}
                      data-active={isActive ? "true" : "false"}
                      disabled={isActive || organizationSwitcher.isPending}
                      onClick={() => {
                        if (!isActive) {
                          organizationSwitcher.mutate(organization.orgID);
                        }
                      }}
                    >
                      {organizationSwitcher.isPending &&
                      organizationSwitcher.variables === organization.orgID ? (
                        <LoaderCircle className="animate-spin" />
                      ) : isActive ? (
                        <Check />
                      ) : (
                        <Building2 />
                      )}
                      <span className="min-w-0 flex-1 truncate">
                        {organizationLabel(organization)}
                      </span>
                    </DropdownMenuItem>
                  );
                })}
              </DropdownMenuGroup>
            </>
          ) : null}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            data-testid="shell-account-sign-out"
            onClick={() => {
              void redirectToSignOut();
            }}
          >
            <LogOut />
            Log out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </ClientOnly>
  );
}

function TriggerSkeleton({ initials, label }: { readonly initials: string; readonly label: string }) {
  return (
    <button
      type="button"
      disabled
      aria-busy="true"
      data-testid="shell-account-trigger"
      className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 text-left text-sm"
    >
      <Avatar className="size-5 shrink-0 rounded-md">
        <AvatarFallback className="rounded-md text-[10px] font-medium">{initials}</AvatarFallback>
      </Avatar>
      <span className="min-w-0 flex-1 truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
        {label}
      </span>
      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground group-data-[collapsible=icon]:hidden" />
    </button>
  );
}

function useOrganizationSwitcher() {
  const router = useRouter();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (orgID: string) => selectActiveOrganization({ data: { orgID } }),
    onSuccess: async () => {
      await queryClient.cancelQueries();
      queryClient.clear();
      if (typeof window !== "undefined") {
        // Organization switches are auth partition changes; reload to discard stale client code and caches.
        window.location.assign(window.location.href);
        return;
      }
      await router.invalidate();
    },
  });
}

function useAccountChrome(): AccountChrome {
  const auth = useSignedInAuth();
  const hydrated = useHydrated();
  const { user } = useUser();
  const { data: account } = useQuery({
    ...profileQuery(auth),
    enabled: hydrated,
    select: (profile) => accountChromeFromProfile(profile, user),
  });

  if (!hydrated) {
    return pendingAccountChrome;
  }

  return account ?? pendingAccountChrome;
}

// "Active members" is the same metric the Members page surfaces — invited
// users that haven't accepted aren't counted as members yet. Zitadel's user
// state literal is `USER_STATE_ACTIVE`; matches what organization-profile uses.
const ACTIVE_MEMBER_STATE = "USER_STATE_ACTIVE";

function useActiveMemberCount(): number | null {
  const auth = useSignedInAuth();
  const hydrated = useHydrated();
  const api = useIdentityApi();
  const { data } = useQuery({
    ...organizationMembersQuery(auth, api),
    enabled: hydrated,
  });
  if (!data) return null;
  return data.filter((member) => member.state === ACTIVE_MEMBER_STATE).length;
}

function formatMemberCount(count: number | null): string {
  if (count === null) return "Organization";
  return `${count} member${count === 1 ? "" : "s"}`;
}

function organizationLabel(organization: AuthOrganizationContext): string {
  return organization.orgName?.trim() || organization.orgID;
}
