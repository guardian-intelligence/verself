import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireProductDomain } from "@verself/web-env";

import { SERVICE_CATALOG } from "~/lib/openapi-catalog";
import { SchemasSection, ServiceSection } from "~/features/reference/reference-renderer";

// The product bare domain (e.g. "verself.sh") comes from server env
// and is the base of every service API subdomain surfaced in the reference
// ("sandbox.api.<domain>", "billing.api.<domain>"). Reading it here rather
// than hardcoding keeps platform docs portable across deployments.
const getProductDomain = createServerFn({ method: "GET" }).handler(() => requireProductDomain());

export const Route = createFileRoute("/_workshop/docs/reference")({
  component: ReferencePage,
  loader: () => getProductDomain(),
  head: () => ({
    meta: [{ title: "API Reference — Verself Platform" }],
  }),
});

function ReferencePage() {
  const productDomain = Route.useLoaderData();

  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Reference
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">API Reference</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          HTTP surface of every Verself service. Operations below are generated from each service's
          committed OpenAPI 3.1 spec, so what you read here is the same contract the Go handlers
          serve and the typed TypeScript clients consume.
        </p>
      </header>

      {SERVICE_CATALOG.map((service) => (
        <ServiceSection key={service.id} service={service} publicOrigin={productDomain} />
      ))}

      <SchemasSection services={SERVICE_CATALOG} />
    </div>
  );
}
