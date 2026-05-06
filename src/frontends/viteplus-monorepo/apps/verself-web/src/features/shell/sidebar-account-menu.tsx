import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClientOnly, Link, useHydrated, useRouter } from "@tanstack/react-router";
import {
  BookOpen,
  Check,
  ChevronDown,
  ChevronsUpDown,
  CircleHelp,
  Home,
  LoaderCircle,
  LogOut,
  MessageCircle,
  Monitor,
  Moon,
  PencilLine,
  Plus,
  Search,
  Settings,
  Sun,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@verself/ui/lib/utils";
import {
  SignedIn,
  SignedOut,
  SignInButton,
  availableOrganizationMetadataQuery,
  useIAMApi,
  type OrganizationMetadataValue,
} from "@verself/auth-web/components";
import { useClerk, useSignedInAuth, useUser } from "@verself/auth-web/react";
import type { AuthOrganizationContext } from "@verself/auth-web/isomorphic";
import { Avatar, AvatarFallback } from "@verself/ui/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@verself/ui/components/ui/dropdown-menu";
import { useBillingTierLabel } from "~/features/billing/use-billing-account";
import { profileQuery } from "~/features/profile/queries";
import { withInteractionSpan } from "~/lib/telemetry/interaction";
import {
  type AccountChrome,
  accountChromeFromProfile,
  pendingAccountChrome,
} from "./account-chrome";
import { orgPath, replaceOrgSlugInHref, useOrgRouteParams } from "./org-routing";
import { selectActiveOrganization } from "~/server-fns/auth";

// The Vercel-style shell treats organization context as the primary switcher
// and leaves the personal account menu anchored at the bottom of the rail.
export function SidebarOrganizationSwitcher() {
  return (
    <>
      <SignedIn>
        <OrganizationSwitcherMenu />
      </SignedIn>
      <SignedOut>
        <div className="px-1">
          <SignInButton variant="outline" className="w-full" />
        </div>
      </SignedOut>
    </>
  );
}

export function SidebarUserMenu() {
  return (
    <SignedIn>
      <ClientOnly fallback={<UserTriggerSkeleton />}>
        <UserAccountMenu />
      </ClientOnly>
    </SignedIn>
  );
}

function OrganizationSwitcherMenu() {
  const auth = useSignedInAuth();
  const { orgSlug } = useOrgRouteParams();
  const userState = useUser();
  if (!userState.isSignedIn) {
    throw new Error("OrganizationSwitcherMenu requires a signed-in user");
  }

  const user = userState.user;
  const organizationProfiles = useAvailableOrganizationProfiles();
  const organizationSwitcher = useOrganizationSwitcher(organizationProfiles);
  const tierLabel = useBillingTierLabel();
  const [query, setQuery] = useState("");
  const activeOrganization = user.availableOrganizations.find(
    (organization) => organization.orgID === auth.selectedOrgId,
  );
  const activeLabel = activeOrganization
    ? organizationLabel(activeOrganization, organizationProfiles) || "Organization"
    : "Organization";
  const visibleOrganizations = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return user.availableOrganizations;
    return user.availableOrganizations.filter((organization) => {
      const label = organizationLabel(organization, organizationProfiles).toLowerCase();
      const slug = organizationSlug(organization, organizationProfiles).toLowerCase();
      return label.includes(normalized) || slug.includes(normalized);
    });
  }, [organizationProfiles, query, user.availableOrganizations]);

  return (
    <ClientOnly fallback={<OrganizationTriggerSkeleton />}>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              data-testid="shell-account-trigger"
              className="group/trigger flex h-9 min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 text-left text-sm text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground group-data-[collapsible=icon]:justify-center"
            >
              <OrganizationMark label={activeLabel} />
              <span className="min-w-0 flex-1 truncate font-medium group-data-[collapsible=icon]:hidden">
                {activeLabel}
              </span>
              <PlanBadge label={tierLabel} className="group-data-[collapsible=icon]:hidden" />
              <ChevronsUpDown className="size-3.5 shrink-0 text-sidebar-foreground/60 group-data-[collapsible=icon]:hidden" />
            </button>
          }
        />
        <DropdownMenuContent
          data-testid="shell-organization-menu"
          side="bottom"
          align="start"
          sideOffset={7}
          className="w-[min(23.5rem,calc(100vw-0.75rem))] overflow-hidden rounded-lg border border-white/10 bg-[#050505] p-0 text-[#ededed] shadow-2xl ring-0"
        >
          <div className="grid h-12 grid-cols-[minmax(0,1fr)_9rem] border-b border-white/10">
            <button
              type="button"
              className="flex min-w-0 items-center gap-2 px-2.5 text-left transition-colors hover:bg-white/8"
            >
              <OrganizationMark label={activeLabel} />
              <span className="min-w-0 truncate text-sm font-medium">{activeLabel}</span>
              <PlanBadge label={tierLabel} />
              <ChevronsUpDown className="ml-auto size-3.5 shrink-0 text-white/55" />
            </button>
            <Link
              to={orgPath(orgSlug, "/builds")}
              className="flex items-center justify-center gap-1 border-l border-white/10 px-3 text-xs font-medium text-white/85 transition-colors hover:bg-white/8 hover:text-white"
            >
              All Projects
              <ChevronDown className="size-3.5 text-white/50" />
            </Link>
          </div>

          <label className="m-2 flex h-8 items-center gap-2 rounded-md border border-white/10 bg-white/[0.03] px-2 text-white/55 focus-within:border-white/20 focus-within:text-white/70">
            <Search className="size-4 shrink-0" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Find organization..."
              className="min-w-0 flex-1 bg-transparent text-sm text-white outline-none placeholder:text-white/45"
            />
            <kbd className="rounded border border-white/15 px-1.5 py-0.5 font-sans text-[10px] font-medium text-white/55">
              Esc
            </kbd>
          </label>

          <div className="max-h-64 overflow-y-auto px-2 pb-2">
            {visibleOrganizations.map((organization) => {
              const isActive = organization.orgID === auth.selectedOrgId;
              const label = organizationLabel(organization, organizationProfiles) || "Organization";
              const slug = organizationSlug(organization, organizationProfiles);
              const isSwitchingHere =
                organizationSwitcher.isPending &&
                organizationSwitcher.variables === organization.orgID;

              return (
                <DropdownMenuItem
                  key={organization.orgID}
                  data-testid="shell-organization-switch-item"
                  data-org-id={organization.orgID}
                  data-active={isActive ? "true" : "false"}
                  disabled={!isActive && organizationSwitcher.isPending}
                  onClick={() => {
                    if (!isActive) {
                      organizationSwitcher.mutate(organization.orgID);
                    }
                  }}
                  className={cn(
                    "flex h-10 gap-2 rounded-md px-2 py-0 text-white/75 focus:bg-white/10 focus:text-white",
                    isActive && "bg-white/10 text-white",
                  )}
                >
                  <OrganizationMark label={label} />
                  <span className="min-w-0 flex-1">
                    <span
                      className="block truncate text-sm font-medium"
                      {...(isActive ? { "data-testid": "shell-active-organization" } : {})}
                    >
                      {label}
                    </span>
                    {slug ? (
                      <span className="block truncate text-xs text-white/45">{slug}</span>
                    ) : null}
                  </span>
                  <span className="flex size-4 shrink-0 items-center justify-center text-white/70">
                    {isSwitchingHere ? (
                      <LoaderCircle className="animate-spin" />
                    ) : isActive ? (
                      <Check />
                    ) : null}
                  </span>
                </DropdownMenuItem>
              );
            })}
            {visibleOrganizations.length === 0 ? (
              <div className="grid min-h-28 place-items-center rounded-md px-6 py-8 text-center text-sm text-white/50">
                No organizations match this search.
              </div>
            ) : null}
            {user.availableOrganizations.length <= 1 && !query ? (
              <div className="grid min-h-32 place-items-center rounded-md px-8 py-8 text-center">
                <div>
                  <div className="mx-auto mb-3 flex size-9 items-center justify-center rounded-md border border-white/10 text-white/45">
                    <MessageCircle className="size-4" />
                  </div>
                  <p className="text-sm leading-5 text-white/50">
                    Organizations you create and join appear here for quick context switching.
                  </p>
                </div>
              </div>
            ) : null}
          </div>

          <DropdownMenuSeparator className="mx-0 my-0 bg-white/10" />
          <DropdownMenuItem
            render={
              <Link
                to={orgPath(orgSlug, "/settings/organization")}
                className="flex h-12 items-center gap-3 px-3 text-white/85 focus:bg-white/10 focus:text-white"
              >
                <Plus className="size-4 text-white/70" />
                <span className="min-w-0">
                  <span className="block text-sm font-medium">Manage organization</span>
                  <span className="block truncate text-xs text-white/50">
                    Members, governance, and organization profile
                  </span>
                </span>
              </Link>
            }
          />
        </DropdownMenuContent>
      </DropdownMenu>
    </ClientOnly>
  );
}

function UserAccountMenu() {
  const account = useAccountChrome();
  const auth = useSignedInAuth();
  const { orgSlug } = useOrgRouteParams();
  const userState = useUser();
  if (!userState.isSignedIn) {
    throw new Error("UserAccountMenu requires a signed-in user");
  }

  const { redirectToSignOut } = useClerk();
  const hydrated = useHydrated();
  const user = userState.user;
  const fallbackDisplayName = user.name || user.preferredUsername || user.email || "Account";
  const displayName = account.displayName || fallbackDisplayName;
  const email = account.email || user.email || "";
  const initials = account.initials || initialsFromText(displayName);
  const { data: profile } = useQuery({
    ...profileQuery(auth),
    enabled: hydrated,
  });
  const theme = profile?.preferences.theme ?? "system";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            data-testid="shell-user-menu-trigger"
            className="flex h-9 min-w-0 items-center gap-2 rounded-md px-1.5 text-left text-sm text-sidebar-foreground transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground group-data-[collapsible=icon]:justify-center"
          >
            <Avatar size="sm" className="size-5">
              <AvatarFallback className="bg-[#c084fc] text-[10px] font-semibold text-black">
                {initials}
              </AvatarFallback>
            </Avatar>
            <span
              data-testid="shell-account-display-name"
              data-account-source={account.source}
              className="min-w-0 flex-1 truncate text-sm font-medium group-data-[collapsible=icon]:hidden"
            >
              {displayName}
            </span>
            <span className="flex size-6 items-center justify-center rounded-full border border-sidebar-border text-sidebar-foreground/70 group-data-[collapsible=icon]:hidden">
              <span aria-hidden="true" className="text-sm leading-none">
                ...
              </span>
            </span>
          </button>
        }
      />
      <DropdownMenuContent
        data-testid="shell-user-menu"
        side="top"
        align="start"
        sideOffset={7}
        className="w-72 overflow-hidden rounded-lg border border-white/10 bg-[#050505] p-0 text-[#ededed] shadow-2xl ring-0"
      >
        <div className="flex items-start gap-3 px-3 py-3">
          <Avatar className="size-8">
            <AvatarFallback className="bg-[#c084fc] text-xs font-semibold text-black">
              {initials}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{displayName}</div>
            <div className="truncate text-xs text-white/55">{email}</div>
          </div>
          <Link
            to={orgPath(orgSlug, "/settings/profile")}
            aria-label="Profile settings"
            className="flex size-6 items-center justify-center rounded-sm text-white/70 transition-colors hover:bg-white/10 hover:text-white"
          >
            <Settings className="size-3.5" />
          </Link>
        </div>

        <DropdownMenuSeparator className="mx-0 my-0 bg-white/10" />
        <AccountMenuLink icon={MessageCircle} label="Feedback" to="/docs" />
        <div className="flex h-10 items-center gap-3 px-3 text-sm text-white/85">
          <span className="flex size-4 items-center justify-center text-white/60">
            <Monitor className="size-4" />
          </span>
          <span className="flex-1">Theme</span>
          <div className="flex rounded-full border border-white/15 bg-white/[0.03] p-0.5">
            <ThemeChip active={theme === "system"} icon={Monitor} label="System theme" />
            <ThemeChip active={theme === "light"} icon={Sun} label="Light theme" />
            <ThemeChip active={theme === "dark"} icon={Moon} label="Dark theme" />
          </div>
        </div>
        <AccountMenuLink icon={Home} label="Home Page" to={orgPath(orgSlug, "/executions")} />
        <AccountMenuLink icon={PencilLine} label="Changelog" to="/policy/changelog" />
        <AccountMenuLink icon={CircleHelp} label="Help" to="/docs" />
        <AccountMenuLink icon={BookOpen} label="Docs" to="/docs" />
        <DropdownMenuItem
          data-testid="shell-account-sign-out"
          onClick={withInteractionSpan("sign_out", async () => {
            await redirectToSignOut();
          })}
          className="flex h-10 gap-3 rounded-none px-3 py-0 text-white/85 focus:bg-white/10 focus:text-white"
        >
          <LogOut className="size-4 text-white/60" />
          Log Out
        </DropdownMenuItem>

        <div className="px-2 py-2">
          <Link
            to={orgPath(orgSlug, "/settings/billing/subscribe")}
            className="flex h-8 w-full items-center justify-center rounded-md border border-white/15 bg-white px-3 text-xs font-medium text-black transition-colors hover:bg-white/90"
          >
            Upgrade plan
          </Link>
        </div>
        <DropdownMenuSeparator className="mx-0 my-0 bg-white/10" />
        <div className="px-3 py-3">
          <div className="text-xs text-white/50">Platform Status</div>
          <div className="mt-1 flex items-center gap-2 text-sm text-white/85">
            <span>All systems normal.</span>
            <span className="ml-auto size-2 rounded-full bg-blue-500" aria-hidden />
          </div>
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AccountMenuLink({
  icon: Icon,
  label,
  to,
}: {
  readonly icon: LucideIcon;
  readonly label: string;
  readonly to: string;
}) {
  return (
    <DropdownMenuItem
      render={
        <Link
          to={to}
          className="flex h-10 items-center gap-3 px-3 text-sm text-white/85 focus:bg-white/10 focus:text-white"
        >
          <Icon className="size-4 text-white/60" />
          <span className="min-w-0 flex-1 truncate">{label}</span>
        </Link>
      }
    />
  );
}

function ThemeChip({
  active,
  icon: Icon,
  label,
}: {
  readonly active: boolean;
  readonly icon: LucideIcon;
  readonly label: string;
}) {
  const { orgSlug } = useOrgRouteParams();
  return (
    <Link
      to={orgPath(orgSlug, "/settings/profile")}
      aria-label={label}
      className={cn(
        "flex size-7 items-center justify-center rounded-full text-white/55 transition-colors hover:bg-white/10 hover:text-white",
        active && "bg-white text-black hover:bg-white hover:text-black",
      )}
    >
      <Icon className="size-3.5" />
    </Link>
  );
}

function OrganizationMark({ label }: { readonly label: string }) {
  const initial = label.trim().charAt(0).toUpperCase() || "V";
  return (
    <span
      className="flex size-5 shrink-0 items-center justify-center rounded-full bg-[#f5c400] text-[10px] font-semibold text-black"
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

function PlanBadge({
  className,
  label,
}: {
  readonly className?: string;
  readonly label: string | null;
}) {
  if (!label) return null;
  return (
    <span
      className={cn(
        "shrink-0 rounded-full bg-white/10 px-1.5 py-0.5 text-[10px] font-medium leading-none text-white/75",
        className,
      )}
    >
      {label}
    </span>
  );
}

function OrganizationTriggerSkeleton() {
  return (
    <button
      type="button"
      disabled
      aria-busy="true"
      data-testid="shell-account-trigger"
      className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 text-left text-sm"
    >
      <span className="size-5 shrink-0 rounded-full bg-sidebar-accent" />
      <span className="min-w-0 flex-1 group-data-[collapsible=icon]:hidden" />
      <ChevronsUpDown className="size-3.5 shrink-0 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden" />
    </button>
  );
}

function UserTriggerSkeleton() {
  return (
    <button
      type="button"
      disabled
      aria-busy="true"
      data-testid="shell-user-menu-trigger"
      className="flex h-9 min-w-0 items-center gap-2 rounded-md px-1.5 text-left text-sm"
    >
      <Avatar size="sm" className="size-5">
        <AvatarFallback />
      </Avatar>
      <span
        data-testid="shell-account-display-name"
        data-account-source="pending"
        className="min-w-0 flex-1 group-data-[collapsible=icon]:hidden"
      />
    </button>
  );
}

function useOrganizationSwitcher(
  organizationProfiles: ReadonlyMap<string, OrganizationMetadataValue> | null,
) {
  const router = useRouter();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (orgID: string) => selectActiveOrganization({ data: { orgID } }),
    onSuccess: async (_snapshot, orgID) => {
      await queryClient.cancelQueries();
      queryClient.clear();
      const nextSlug = organizationProfiles?.get(orgID)?.slug;
      if (nextSlug) {
        await router.navigate({
          href: replaceOrgSlugInHref(router.state.location.href, nextSlug),
          replace: true,
        });
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

function useAvailableOrganizationProfiles(): ReadonlyMap<string, OrganizationMetadataValue> | null {
  const auth = useSignedInAuth();
  const hydrated = useHydrated();
  const api = useIAMApi();
  const { data } = useQuery({
    ...availableOrganizationMetadataQuery(auth, api),
    enabled: hydrated,
  });
  return data ?? null;
}

function organizationLabel(
  organization: AuthOrganizationContext,
  profiles: ReadonlyMap<string, OrganizationMetadataValue> | null,
): string {
  return profiles?.get(organization.orgID)?.display_name ?? "";
}

function organizationSlug(
  organization: AuthOrganizationContext,
  profiles: ReadonlyMap<string, OrganizationMetadataValue> | null,
): string {
  const slug = profiles?.get(organization.orgID)?.slug;
  return slug ? `/${slug}` : "";
}

function initialsFromText(value: string): string {
  const parts = value
    .split(/\s+/)
    .map((part) => part.trim())
    .filter(Boolean);
  const initials = parts
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
  return initials || "?";
}
