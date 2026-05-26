import { useMutation } from "@tanstack/react-query";
import { useLiveQuery } from "@tanstack/react-db";
import { createFileRoute, Link } from "@tanstack/react-router";
import type { BrowserLocation, WebAuthAccount, WebAuthSession } from "@verself/auth-web/isomorphic";
import { useAuthCollections } from "@verself/auth-web/react";
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
import { ArrowLeft, Laptop, MapPin, ShieldCheck, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { revokeClientAuthDevice, selectActiveAccount } from "~/server-fns/auth";

export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/account/security")({
  component: AccountSecurity,
});

function AccountSecurity() {
  const { orgSlug } = Route.useParams();
  const collections = useAuthCollections();
  const { data: accountRows = [] } = useLiveQuery(collections.accounts);
  const { data: sessionRows = [] } = useLiveQuery(collections.sessions);
  const accounts = accountRows.map(accountRowModel);
  const sessions = sessionRows.map(sessionRowModel);
  const selectAccount = useMutation({
    mutationFn: (input: { readonly accountHandle: string }) =>
      selectActiveAccount({ data: { accountHandle: input.accountHandle } }),
  });
  const revoke = useMutation({
    mutationFn: (input: { readonly sessionHandle: string }) =>
      revokeClientAuthDevice({ data: { sessionHandle: input.sessionHandle } }),
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
            <span>{accounts.length} browser accounts</span>
          </div>
        </header>

        <section className="grid gap-3 sm:grid-cols-2">
          <Metric
            icon={<Laptop className="size-4" />}
            label="Current device"
            value={currentDevice(sessions)}
          />
          <Metric
            icon={<MapPin className="size-4" />}
            label="Location"
            value={currentLocation(sessions)}
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
              {accounts.map((account) => (
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
              {sessions.map((session) => (
                <SessionRow
                  key={session.sessionHandle}
                  session={session}
                  isRevoking={
                    revoke.isPending && revoke.variables?.sessionHandle === session.sessionHandle
                  }
                  onRevoke={() =>
                    revoke.mutate({
                      sessionHandle: session.sessionHandle,
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
  readonly account: AccountRowModel;
  readonly isSwitching: boolean;
  readonly onSelect: () => void;
}) {
  return (
    <TableRow>
      <TableCell className="min-w-64 whitespace-normal">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">
              {account.displayName || account.email || account.subject}
            </span>
            {account.isCurrent ? <Badge variant="success">Current</Badge> : null}
          </div>
          <p className="max-w-96 truncate text-xs text-muted-foreground">
            {account.email || account.subject}
          </p>
        </div>
      </TableCell>
      <TableCell className="font-mono text-xs">{account.selectedOrgID || "none"}</TableCell>
      <TableCell>{dateTimeLabel(account.lastSeenAt)}</TableCell>
      <TableCell className="text-right">
        <Button size="sm" disabled={account.isCurrent || isSwitching} onClick={onSelect}>
          <span>{isSwitching ? "Switching" : "Use"}</span>
        </Button>
      </TableCell>
    </TableRow>
  );
}

type AccountRowModel = {
  readonly accountHandle: string;
  readonly isCurrent: boolean;
  readonly subject: string;
  readonly email: string;
  readonly displayName: string;
  readonly selectedOrgID: string;
  readonly lastSeenAt: Date;
};

type SessionRowModel = {
  readonly sessionHandle: string;
  readonly isCurrent: boolean;
  readonly clientIP: string;
  readonly clientIPSource: string;
  readonly userAgent: string;
  readonly device: {
    readonly label: string;
    readonly kind: string;
    readonly browserName: string;
    readonly osName: string;
  };
  readonly location: BrowserLocation;
  readonly createdAt: Date;
  readonly lastSeenAt: Date;
};

function accountRowModel(account: WebAuthAccount): AccountRowModel {
  return {
    accountHandle: account.account_handle,
    isCurrent: account.is_current,
    subject: account.subject,
    email: account.email,
    displayName: account.display_name,
    selectedOrgID: account.selected_org_id,
    lastSeenAt: account.last_seen_at,
  };
}

function sessionRowModel(session: WebAuthSession): SessionRowModel {
  return {
    sessionHandle: session.session_handle,
    isCurrent: session.is_current,
    clientIP: session.client_ip,
    clientIPSource: session.client_ip_source,
    userAgent: session.user_agent,
    device: {
      label: session.device_label,
      kind: session.device_kind,
      browserName: session.browser_name,
      osName: session.os_name,
    },
    location: {
      countryCode: session.location_country_code,
      region: session.location_region,
      city: session.location_city,
    },
    createdAt: session.created_at,
    lastSeenAt: session.last_seen_at,
  };
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
  readonly session: SessionRowModel;
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
          title="Revoke device"
        >
          <Trash2 className="size-3.5" aria-hidden="true" />
          <span>{isRevoking ? "Revoking" : "Revoke device"}</span>
        </Button>
      </TableCell>
    </TableRow>
  );
}

function currentDevice(sessions: readonly SessionRowModel[]): string {
  const session = sessions.find((candidate) => candidate.isCurrent);
  return session ? deviceLabel(session) : "Unknown device";
}

function currentLocation(sessions: readonly SessionRowModel[]): string {
  return locationLabel(sessions.find((session) => session.isCurrent)?.location);
}

function locationLabel(location: BrowserLocation | undefined): string {
  if (!location) {
    return "Unknown";
  }
  const parts = [location.city, location.region, location.countryCode].filter(Boolean);
  return parts.length > 0 ? parts.join(", ") : "Unknown";
}

function deviceLabel(session: SessionRowModel): string {
  return session.device.label || "Unknown device";
}

function dateTimeLabel(value: Date | string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
