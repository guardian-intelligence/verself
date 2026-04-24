import { useQuery } from "@tanstack/react-query";
import { ClientOnly, useHydrated } from "@tanstack/react-router";
import { LogOut } from "lucide-react";
import { SignedIn, SignedOut, SignInButton } from "@forge-metal/auth-web/components";
import { useClerk, useSignedInAuth, useUser } from "@forge-metal/auth-web/react";
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
  accountChromeFromAuthUser,
  accountChromeFromProfile,
} from "./account-chrome";

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
  const { redirectToSignOut } = useClerk();
  const tierLabel = useBillingTierLabel();
  const triggerContent = <AccountTriggerContent account={account} tierLabel={tierLabel} />;

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <ClientOnly
          fallback={
            <SidebarMenuButton
              size="lg"
              variant="outline"
              data-testid="shell-account-trigger"
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

function useAccountChrome(): AccountChrome {
  const auth = useSignedInAuth();
  const hydrated = useHydrated();
  const { user } = useUser();
  const fallback = accountChromeFromAuthUser(user);
  const { data: account } = useQuery({
    ...profileQuery(auth),
    enabled: hydrated,
    select: (profile) => accountChromeFromProfile(profile, user),
  });

  return hydrated ? (account ?? fallback) : fallback;
}

function AccountTriggerContent({
  account,
  tierLabel,
}: {
  readonly account: AccountChrome;
  readonly tierLabel: string | null;
}) {
  return (
    <>
      <Avatar className="size-8 shrink-0">
        <AvatarFallback className="text-xs">{account.initials}</AvatarFallback>
      </Avatar>
      <span
        className="min-w-0 flex-1 truncate text-left text-sm font-medium"
        data-testid="shell-account-display-name"
      >
        {account.displayName}
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
