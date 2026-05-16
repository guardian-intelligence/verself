import { createFileRoute, ClientOnly } from "@tanstack/react-router";
import { FlightBoard, FlightConsoleSkeleton } from "~/features/console/flight/flight-widget";
import { flightFixture, isFlightFixtureName } from "~/features/console/flight/fixtures";
import { useFlights } from "~/features/console/flight/live";

// The signed-in surface is exactly this widget: a live departures board of
// in-flight CI. Everything operators used to do here lives in the CLI/SDK/HTTP
// API. Reads come only from the Electric `flights` shape; there is no loader
// and no polling.
export const Route = createFileRoute("/_shell/_authenticated/$orgSlug/")({
  validateSearch: (search: Record<string, unknown>) => ({
    flight: typeof search.flight === "string" ? search.flight : undefined,
  }),
  component: FlightConsole,
});

function FlightConsole() {
  const { orgSlug } = Route.useParams();
  const { flight } = Route.useSearch();

  // Deterministic fixture states for `aspect dev verself-web` + agent-browser.
  if (import.meta.env.DEV && flight && isFlightFixtureName(flight)) {
    return <FlightBoard flights={flightFixture(flight)} orgSlug={orgSlug} />;
  }

  return (
    <ClientOnly fallback={<FlightConsoleSkeleton />}>
      <LiveFlightConsole orgSlug={orgSlug} />
    </ClientOnly>
  );
}

function LiveFlightConsole({ orgSlug }: { readonly orgSlug: string }) {
  const { flights, isLoading } = useFlights();
  if (isLoading && flights.length === 0) {
    return <FlightConsoleSkeleton />;
  }
  return <FlightBoard flights={flights} orgSlug={orgSlug} />;
}
