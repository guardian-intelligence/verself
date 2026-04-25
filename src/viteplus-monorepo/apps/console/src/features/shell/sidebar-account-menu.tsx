import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClientOnly, useHydrated, useRouter } from "@tanstack/react-router";
import { Building2, Check, LoaderCircle, LogOut } from "lucide-react";
import { SignedIn, SignedOut, SignInButton } from "@forge-metal/auth-web/components";
import { useClerk, useSignedInAuth, useUser } from "@forge-metal/auth-web/react";
import type { AuthOrganizationContext } from "@forge-metal/auth-web/isomorphic";
import { Avatar, AvatarFallback } from "@forge-metal/ui/components/ui/avatar";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@forge-metal/ui/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@forge-metal/ui/components/ui/sidebar";
import { useBillingTierLabel } from "~/features/billing/use-billing-account";
import { profileQuery } from "~/features/profile/queries";
import {
  type AccountChrome,
  accountChromeFromProfile,
  pendingAccountChrome,
} from "./account-chrome";
import { selectActiveOrganization } from "~/server-fns/auth";

export function SidebarAccountSlot() {
  return (
    <>
      <SignedIn>
        <SidebarAccountMenu />
      </SignedIn>
      <SignedOut>
        <SignInButton variant="outline" className="w-full" />
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
  const tierLabel = useBillingTierLabel();
  const activeOrganization = user.availableOrganizations.find(
    (organization) => organization.orgID === auth.selectedOrgId,
  );
  const organizationSwitcher = useOrganizationSwitcher();
  const triggerContent = (
    <AccountTriggerContent
      account={account}
      activeOrganization={activeOrganization ?? null}
      tierLabel={tierLabel}
    />
  );

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <ClientOnly
          fallback={
            <SidebarMenuButton
              size="lg"
              variant="outline"
              data-testid="shell-account-trigger"
              data-account-source={account.source}
              disabled
              aria-busy="true"
            >
              {triggerContent}
            </SidebarMenuButton>
          }
        >
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <SidebarMenuButton
                  size="lg"
                  variant="outline"
                  data-testid="shell-account-trigger"
                  data-account-source={account.source}
                  aria-busy={account.source === "pending"}
                  className="data-popup-open:bg-sidebar-accent"
                >
                  {triggerContent}
                </SidebarMenuButton>
              }
            />
            <DropdownMenuContent
              data-testid="shell-account-menu"
              side="top"
              align="end"
              sideOffset={8}
              className="min-w-60"
            >
              <DropdownMenuGroup>
                <DropdownMenuLabel className="flex flex-col gap-0.5">
                  <span className="truncate text-sm font-medium text-foreground">
                    {account.displayName}
                  </span>
                  {account.email ? (
                    <span className="truncate text-xs text-muted-foreground">{account.email}</span>
                  ) : null}
                </DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              {user.availableOrganizations.length > 1 ? (
                <>
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Organization</DropdownMenuLabel>
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
                  <DropdownMenuSeparator />
                </>
              ) : null}
              <DropdownMenuItem
                data-testid="shell-account-sign-out"
                onClick={() => {
                  void redirectToSignOut();
                }}
              >
                <LogOut />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </ClientOnly>
      </SidebarMenuItem>
    </SidebarMenu>
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

function AccountTriggerContent({
  account,
  activeOrganization,
  tierLabel,
}: {
  readonly account: AccountChrome;
  readonly activeOrganization: AuthOrganizationContext | null;
  readonly tierLabel: string | null;
}) {
  const activeOrganizationLabel = activeOrganization ? organizationLabel(activeOrganization) : null;
  return (
    <>
      <Avatar className="size-8 shrink-0">
        <AvatarFallback className="text-xs">{account.initials}</AvatarFallback>
      </Avatar>
      <span className="min-w-0 flex-1 text-left">
        <span
          className="block truncate text-sm font-medium"
          data-testid="shell-account-display-name"
          data-account-source={account.source}
        >
          {account.displayName}
        </span>
        {activeOrganizationLabel ? (
          <span
            className="block truncate text-xs text-muted-foreground"
            data-testid="shell-active-organization"
          >
            {activeOrganizationLabel}
          </span>
        ) : null}
      </span>
      {tierLabel ? (
        <Badge
          variant="secondary"
          data-testid="shell-account-tier"
          className="shrink-0 group-data-[collapsible=icon]:hidden"
        >
          {tierLabel}
        </Badge>
      ) : null}
    </>
  );
}

function organizationLabel(organization: AuthOrganizationContext): string {
  return organization.orgName?.trim() || organization.orgID;
}
