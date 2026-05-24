import { createFileRoute, ClientOnly, Link } from "@tanstack/react-router";
import { ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";
import { FlightBoard, FlightConsoleSkeleton } from "~/features/console/flight/flight-widget";
import {
  debugFlight,
  flightFixture,
  isFlightFixtureName,
} from "~/features/console/flight/fixtures";
import type { FrameKind } from "~/features/console/flight/iphone-frame";
import { useFlights } from "~/features/console/flight/live";

function str(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

// Dev-only calibration overlay opt-in (DESIGN.md). Only the known device kind
// is accepted; anything else is ignored so the param can never wrap the
// shipped surface by accident.
function frameKind(value: unknown): FrameKind | undefined {
  return value === "iphone17" ? "iphone17" : undefined;
}

// The signed-in surface is exactly this widget. Until the CI flight data model
// is real, the default state uses the same synthetic fixtures as QA /
// agent-browser / design review:
//   ?flight=building-on-time | running-late | boarding | no-build
//   ?flight=debug&actor=ash&src=PR47&dst=MAIN&state=ontime&remaining=12&commits=4
//            (state = ontime | late | cold)
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/")({
  validateSearch: (search: Record<string, unknown>) => ({
    flight: str(search.flight),
    actor: str(search.actor),
    src: str(search.src),
    dst: str(search.dst),
    status: str(search.status),
    state: str(search.state),
    remaining: str(search.remaining),
    commits: str(search.commits),
    frame: frameKind(search.frame),
  }),
  component: FlightConsole,
});

function FlightConsole() {
  const { orgSlug } = Route.useParams();
  const { flight, actor, src, dst, status, state, remaining, commits, frame } = Route.useSearch();

  if (flight === "debug") {
    return (
      <ConsoleSurface orgSlug={orgSlug}>
        <FlightBoard
          flights={debugFlight({ actor, src, dst, status, state, remaining, commits })}
          orgSlug={orgSlug}
          frame={frame}
        />
      </ConsoleSurface>
    );
  }
  if (flight && isFlightFixtureName(flight)) {
    return (
      <ConsoleSurface orgSlug={orgSlug}>
        <FlightBoard flights={flightFixture(flight)} orgSlug={orgSlug} frame={frame} />
      </ConsoleSurface>
    );
  }

  return (
    <ClientOnly
      fallback={
        <ConsoleSurface orgSlug={orgSlug}>
          <FlightConsoleSkeleton />
        </ConsoleSurface>
      }
    >
      <LiveFlightConsole orgSlug={orgSlug} frame={frame} />
    </ClientOnly>
  );
}

function ConsoleSurface({
  orgSlug,
  children,
}: {
  readonly orgSlug: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="relative min-h-svh">
      <Link
        to="/$orgSlug/account/security"
        params={{ orgSlug }}
        className="fixed right-3 top-3 z-50 inline-flex size-9 items-center justify-center rounded-md border border-white/15 bg-black/20 text-white/80 backdrop-blur transition-colors hover:bg-black/35 hover:text-white"
        title="Account security"
      >
        <ShieldCheck className="size-4" aria-hidden="true" />
        <span className="sr-only">Account security</span>
      </Link>
      {children}
    </div>
  );
}

function LiveFlightConsole({
  orgSlug,
  frame,
}: {
  readonly orgSlug: string;
  readonly frame?: FrameKind | undefined;
}) {
  const { flights, isLoading } = useFlights();
  if (isLoading && flights.length === 0) {
    return (
      <ConsoleSurface orgSlug={orgSlug}>
        <FlightConsoleSkeleton />
      </ConsoleSurface>
    );
  }
  return (
    <ConsoleSurface orgSlug={orgSlug}>
      <FlightBoard flights={flights} orgSlug={orgSlug} frame={frame} />
    </ConsoleSurface>
  );
}
