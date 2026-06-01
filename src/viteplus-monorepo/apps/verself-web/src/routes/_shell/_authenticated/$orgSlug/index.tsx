import { createFileRoute } from "@tanstack/react-router";
import { FlightBoard } from "~/features/console/flight/flight-widget";
import {
  debugFlight,
  flightFixture,
  isFlightFixtureName,
} from "~/features/console/flight/fixtures";
import { useFlights } from "~/features/console/flight/live";
import { ConsoleAppHost } from "~/features/console/runtime/console-app-host";
import { parseConsoleSearch } from "~/features/console/runtime/console-search";

// The signed-in surface uses the shared Verself console shell. Until the CI
// flight data model is real, the live activity uses the same synthetic fixtures
// as QA / agent-browser / design review:
//   ?flight=building-on-time | running-late | boarding | no-build
//   ?flight=debug&actor=ash&src=PR47&dst=MAIN&state=ontime&remaining=12&commits=4
//            (state = ontime | late | cold)
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/")({
  validateSearch: parseConsoleSearch,
  component: FlightConsole,
});

function FlightConsole() {
  const { orgSlug } = Route.useParams();
  const search = Route.useSearch();
  const { flights } = useFlights();
  const liveActivityFlights =
    search.flight === "debug"
      ? debugFlight(search)
      : search.flight && isFlightFixtureName(search.flight)
        ? flightFixture(search.flight)
        : flights;

  return (
    <ConsoleAppHost
      liveActivity={
        <FlightBoard
          flights={liveActivityFlights}
          frame={search.frame}
          orgSlug={orgSlug}
          placement="inline"
        />
      }
      liveActivityVisible
      orgSlug={orgSlug}
      search={search}
    />
  );
}
