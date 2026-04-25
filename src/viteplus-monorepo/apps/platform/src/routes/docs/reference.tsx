import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";

import { SERVICE_CATALOG } from "~/lib/openapi-catalog";
import { SchemasSection, ServiceSection } from "~/features/reference/reference-renderer";

// The operator's bare domain (e.g. "anveio.com") comes from server env
// and is the base of every service API subdomain surfaced in the reference
// ("sandbox.api.<domain>", "billing.api.<domain>"). Reading it here rather
// than hardcoding keeps platform docs portable across deployments.
const getOperatorDomain = createServerFn({ method: "GET" }).handler(() => requireOperatorDomain());

export const Route = createFileRoute("/docs/reference")({
  component: ReferencePage,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [{ title: "API Reference — Forge Metal Platform" }],
  }),
});

function ReferencePage() {
  const operatorDomain = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Reference
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">API Reference</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          HTTP surface of every Forge Metal service. Operations below are generated from each
          service's committed OpenAPI 3.1 spec, so what you read here is the same contract the Go
          handlers serve and the typed TypeScript clients consume.
        </p>
      </header>

      {SERVICE_CATALOG.map((service) => (
        <ServiceSection key={service.id} service={service} publicOrigin={operatorDomain} />
      ))}

      <SchemasSection services={SERVICE_CATALOG} />
    </div>
  );
}
