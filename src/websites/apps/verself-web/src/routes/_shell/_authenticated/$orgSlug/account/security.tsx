import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import type {
  AuthenticatedAuth,
  BrowserAccountSummary,
  BrowserLocation,
  BrowserSessionSummary,
} from "@verself/auth-web/isomorphic";
import { authQueryKey } from "@verself/auth-web/isomorphic";
import { useSession } from "@verself/auth-web/react";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@verself/ui/components/ui/table";
import { ArrowLeft, KeyRound, Laptop, MapPin, ShieldCheck, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import {
  getClientAuthAccounts,
  getClientAuthSessions,
  revokeClientAuthSession,
  selectActiveAccount,
} from "~/server-fns/auth";

const accountSessionsPart = "browser-sessions" as const;
const browserAccountsPart = "browser-accounts" as const;

function browserSessionsQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, accountSessionsPart),
    queryFn: () => getClientAuthSessions(),
  });
}

function browserAccountsQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, browserAccountsPart),
    queryFn: () => getClientAuthAccounts(),
  });
}

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/account/security")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(browserSessionsQuery(context.auth)),
      context.queryClient.ensureQueryData(browserAccountsQuery(context.auth)),
    ]);
  },
  component: AccountSecurity,
});

function AccountSecurity() {
  const { orgSlug } = Route.useParams();
  const { auth } = Route.useRouteContext();
  const session = useSession();
  const { data: sessionsData } = useSuspenseQuery(browserSessionsQuery(auth));
  const { data: accountsData } = useSuspenseQuery(browserAccountsQuery(auth));
  const queryClient = useQueryClient();
  const selectAccount = useMutation({
    mutationFn: (input: { readonly accountHandle: string }) =>
      selectActiveAccount({ data: { accountHandle: input.accountHandle } }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: authQueryKey(auth, browserAccountsPart) });
      window.location.assign("/");
    },
  });
  const revoke = useMutation({
    mutationFn: (input: { readonly sessionHandle: string; readonly isCurrent: boolean }) =>
      revokeClientAuthSession({ data: { sessionHandle: input.sessionHandle } }),
    onSuccess: async (_result, input) => {
      await queryClient.invalidateQueries({ queryKey: authQueryKey(auth, accountSessionsPart) });
      await queryClient.invalidateQueries({ queryKey: authQueryKey(auth, browserAccountsPart) });
      if (input.isCurrent) {
        window.location.assign("/login");
      }
    },
  });

  return (
    <div className="min-h-svh bg-background text-foreground">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-8 px-4 py-5 sm:px-6 lg:px-8">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-5">
          <div className="flex min-w-0 items-center gap-3">
            <Link
              to="/$orgSlug"
              params={{ orgSlug }}
              search={{
                flight: undefined,
                actor: undefined,
                src: undefined,
                dst: undefined,
                status: undefined,
                state: undefined,
                remaining: undefined,
                commits: undefined,
                frame: undefined,
              }}
              className="inline-flex size-9 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="Back"
            >
              <ArrowLeft className="size-4" aria-hidden="true" />
              <span className="sr-only">Back</span>
            </Link>
            <div className="min-w-0">
              <p className="font-mono text-xs text-muted-foreground">{orgSlug}</p>
              <h1 className="truncate text-xl font-semibold">Account security</h1>
            </div>
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <ShieldCheck className="size-4" aria-hidden="true" />
            <span>{accountsData.accounts.length} browser accounts</span>
          </div>
        </header>

        <section className="grid gap-3 sm:grid-cols-3">
          <Metric
            icon={<Laptop className="size-4" />}
            label="Current device"
            value={currentDevice(sessionsData.sessions)}
          />
          <Metric
            icon={<MapPin className="size-4" />}
            label="Location"
            value={currentLocation(sessionsData.sessions)}
          />
          <Metric
            icon={<KeyRound className="size-4" />}
            label="Auth method"
            value={
              session.isSignedIn
                ? authMethodLabel(session.session.authMethods)
                : "Identity provider"
            }
          />
        </section>

        <section className="overflow-x-auto border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Account</TableHead>
                <TableHead>Selected org</TableHead>
                <TableHead>Last seen</TableHead>
                <TableHead className="w-28 text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accountsData.accounts.map((account) => (
                <AccountRow
                  key={account.accountHandle}
                  account={account}
                  isSwitching={
                    selectAccount.isPending &&
                    selectAccount.variables?.accountHandle === account.accountHandle
                  }
                  onSelect={() => selectAccount.mutate({ accountHandle: account.accountHandle })}
                />
              ))}
            </TableBody>
          </Table>
        </section>

        <section className="overflow-x-auto border border-border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Device</TableHead>
                <TableHead>IP</TableHead>
                <TableHead>Location</TableHead>
                <TableHead>First seen</TableHead>
                <TableHead>Last seen</TableHead>
                <TableHead className="w-24 text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessionsData.sessions.map((session) => (
                <SessionRow
                  key={session.sessionHandle}
                  session={session}
                  isRevoking={
                    revoke.isPending && revoke.variables?.sessionHandle === session.sessionHandle
                  }
                  onRevoke={() =>
                    revoke.mutate({
                      sessionHandle: session.sessionHandle,
                      isCurrent: session.isCurrent,
                    })
                  }
                />
              ))}
            </TableBody>
          </Table>
        </section>
      </div>
    </div>
  );
}

function AccountRow({
  account,
  isSwitching,
  onSelect,
}: {
  readonly account: BrowserAccountSummary;
  readonly isSwitching: boolean;
  readonly onSelect: () => void;
}) {
  return (
    <TableRow>
      <TableCell className="min-w-64 whitespace-normal">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">
              {account.displayName ?? account.email ?? account.subject}
            </span>
            {account.isCurrent ? <Badge variant="success">Current</Badge> : null}
          </div>
          <p className="max-w-96 truncate text-xs text-muted-foreground">
            {account.email ?? account.subject}
          </p>
        </div>
      </TableCell>
      <TableCell className="font-mono text-xs">{account.selectedOrgID ?? "none"}</TableCell>
      <TableCell>{dateTimeLabel(account.lastSeenAt)}</TableCell>
      <TableCell className="text-right">
        <Button size="sm" disabled={account.isCurrent || isSwitching} onClick={onSelect}>
          <span>{isSwitching ? "Switching" : "Use"}</span>
        </Button>
      </TableCell>
    </TableRow>
  );
}

function Metric({
  icon,
  label,
  value,
}: {
  readonly icon: ReactNode;
  readonly label: string;
  readonly value: string;
}) {
  return (
    <div className="min-w-0 border border-border bg-card p-3">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <p className="mt-2 truncate text-sm font-medium text-foreground">{value}</p>
    </div>
  );
}

function SessionRow({
  session,
  isRevoking,
  onRevoke,
}: {
  readonly session: BrowserSessionSummary;
  readonly isRevoking: boolean;
  readonly onRevoke: () => void;
}) {
  return (
    <TableRow>
      <TableCell className="min-w-56 whitespace-normal">
        <div className="flex min-w-0 items-center gap-2">
          <Laptop className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{deviceLabel(session)}</span>
              {session.isCurrent ? <Badge variant="success">Current</Badge> : null}
            </div>
            <p className="max-w-80 truncate text-xs text-muted-foreground">{session.userAgent}</p>
          </div>
        </div>
      </TableCell>
      <TableCell>
        <div className="font-mono text-xs">{session.clientIP || "unknown"}</div>
        <div className="text-xs text-muted-foreground">{session.clientIPSource || "untrusted"}</div>
      </TableCell>
      <TableCell>{locationLabel(session.location)}</TableCell>
      <TableCell>{dateTimeLabel(session.createdAt)}</TableCell>
      <TableCell>{dateTimeLabel(session.lastSeenAt)}</TableCell>
      <TableCell className="text-right">
        <Button
          variant="destructive"
          size="sm"
          disabled={isRevoking}
          onClick={onRevoke}
          title="Revoke session"
        >
          <Trash2 className="size-3.5" aria-hidden="true" />
          <span>{isRevoking ? "Revoking" : "Revoke"}</span>
        </Button>
      </TableCell>
    </TableRow>
  );
}

function currentDevice(sessions: readonly BrowserSessionSummary[]): string {
  const session = sessions.find((candidate) => candidate.isCurrent);
  return session ? deviceLabel(session) : "Unknown device";
}

function currentLocation(sessions: readonly BrowserSessionSummary[]): string {
  return locationLabel(sessions.find((session) => session.isCurrent)?.location);
}

function authMethodLabel(methods: readonly string[]): string {
  if (methods.length === 0) {
    return "Identity provider";
  }
  return methods.map(authMethodDisplayName).join(", ");
}

function authMethodDisplayName(method: string): string {
  switch (method.toLowerCase()) {
    case "pwd":
    case "password":
      return "Password";
    case "otp":
    case "totp":
      return "Authenticator app";
    case "webauthn":
    case "passkey":
      return "Passkey";
    default:
      return method;
  }
}

function locationLabel(location: BrowserLocation | undefined): string {
  if (!location) {
    return "Unknown";
  }
  const parts = [location.city, location.region, location.countryCode].filter(Boolean);
  return parts.length > 0 ? parts.join(", ") : "Unknown";
}

function deviceLabel(session: BrowserSessionSummary): string {
  return session.device.label || session.createdDevice.label || "Unknown device";
}

function dateTimeLabel(value: Date | string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
