import { createFileRoute, ClientOnly } from "@tanstack/react-router";
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

// The signed-in surface is exactly this widget: a live departures board of
// in-flight CI. Reads come only from the Electric `flights` shape; there is no
// loader and no polling. `?flight=` swaps in synthetic states for QA /
// agent-browser / design review (opt-in, auth-gated, default is live):
//   ?flight=building-on-time | running-late | no-history | no-build
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
      <FlightBoard
        flights={debugFlight({ actor, src, dst, status, state, remaining, commits })}
        orgSlug={orgSlug}
        frame={frame}
      />
    );
  }
  if (flight && isFlightFixtureName(flight)) {
    return <FlightBoard flights={flightFixture(flight)} orgSlug={orgSlug} frame={frame} />;
  }

  return (
    <ClientOnly fallback={<FlightConsoleSkeleton />}>
      <LiveFlightConsole orgSlug={orgSlug} frame={frame} />
    </ClientOnly>
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
    return <FlightConsoleSkeleton />;
  }
  return <FlightBoard flights={flights} orgSlug={orgSlug} frame={frame} />;
}
