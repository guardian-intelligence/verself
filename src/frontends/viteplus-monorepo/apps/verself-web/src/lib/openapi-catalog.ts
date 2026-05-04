// OpenAPI catalog — parses every Verself service spec at module load
// and exposes a single ordered list the reference page renders against.
// Specs live in src/__generated/openapi-specs/<service>/openapi-3.1.yaml;
// the `generate` package script refreshes them from each Go service's
// canonical openapi/openapi-3.1.yaml. They sit under openapi-specs/ rather
// than alongside the Bazel-generated client SDKs so the openapi_clients
// rule's write_source_files snapshot stays clean.
import * as v from "valibot";
import { parse as parseYaml } from "yaml";

import sandboxRentalYaml from "../__generated/openapi-specs/sandbox-rental-api/openapi-3.1.yaml?raw";
import identityYaml from "../__generated/openapi-specs/iam-api/openapi-3.1.yaml?raw";
import mailboxYaml from "../__generated/openapi-specs/mailbox-api/openapi-3.1.yaml?raw";
import billingYaml from "../__generated/openapi-specs/billing-api/openapi-3.1.yaml?raw";

export type OpenApiSchema = {
  readonly type?: string | readonly string[] | undefined;
  readonly format?: string | undefined;
  readonly description?: string | undefined;
  readonly examples?: readonly unknown[] | undefined;
  readonly enum?: readonly unknown[] | undefined;
  readonly properties?: Readonly<Record<string, OpenApiSchema>> | undefined;
  readonly required?: readonly string[] | undefined;
  readonly items?: OpenApiSchema | undefined;
  readonly additionalProperties?: boolean | OpenApiSchema | undefined;
  readonly allOf?: readonly OpenApiSchema[] | undefined;
  readonly oneOf?: readonly OpenApiSchema[] | undefined;
  readonly anyOf?: readonly OpenApiSchema[] | undefined;
  readonly $ref?: string | undefined;
  readonly minimum?: number | undefined;
  readonly maximum?: number | undefined;
  readonly minLength?: number | undefined;
  readonly maxLength?: number | undefined;
  readonly readOnly?: boolean | undefined;
};

const OpenApiSchemaShape: v.GenericSchema<OpenApiSchema> = v.lazy(() =>
  v.object({
    type: v.optional(v.union([v.string(), v.array(v.string())])),
    format: v.optional(v.string()),
    description: v.optional(v.string()),
    examples: v.optional(v.array(v.unknown())),
    enum: v.optional(v.array(v.unknown())),
    properties: v.optional(v.record(v.string(), OpenApiSchemaShape)),
    required: v.optional(v.array(v.string())),
    items: v.optional(OpenApiSchemaShape),
    additionalProperties: v.optional(v.union([v.boolean(), OpenApiSchemaShape])),
    allOf: v.optional(v.array(OpenApiSchemaShape)),
    oneOf: v.optional(v.array(OpenApiSchemaShape)),
    anyOf: v.optional(v.array(OpenApiSchemaShape)),
    $ref: v.optional(v.string()),
    minimum: v.optional(v.number()),
    maximum: v.optional(v.number()),
    minLength: v.optional(v.number()),
    maxLength: v.optional(v.number()),
    readOnly: v.optional(v.boolean()),
  }),
);

const OpenApiParameterShape = v.object({
  name: v.string(),
  in: v.picklist(["path", "query", "header", "cookie"]),
  description: v.optional(v.string()),
  required: v.optional(v.boolean()),
  schema: v.optional(OpenApiSchemaShape),
});

const OpenApiMediaTypeShape = v.object({
  schema: v.optional(OpenApiSchemaShape),
});

const OpenApiRequestBodyShape = v.object({
  description: v.optional(v.string()),
  required: v.optional(v.boolean()),
  content: v.optional(v.record(v.string(), OpenApiMediaTypeShape)),
});

const OpenApiResponseShape = v.object({
  description: v.optional(v.string()),
  content: v.optional(v.record(v.string(), OpenApiMediaTypeShape)),
});

const OpenApiOperationShape = v.object({
  operationId: v.optional(v.string()),
  summary: v.optional(v.string()),
  description: v.optional(v.string()),
  parameters: v.optional(v.array(OpenApiParameterShape)),
  requestBody: v.optional(OpenApiRequestBodyShape),
  responses: v.optional(v.record(v.string(), OpenApiResponseShape)),
  security: v.optional(v.array(v.record(v.string(), v.array(v.string())))),
  "x-verself-iam": v.optional(v.unknown()),
});

const METHODS = ["get", "post", "put", "patch", "delete"] as const;

const OpenApiPathItemShape = v.object({
  get: v.optional(OpenApiOperationShape),
  post: v.optional(OpenApiOperationShape),
  put: v.optional(OpenApiOperationShape),
  patch: v.optional(OpenApiOperationShape),
  delete: v.optional(OpenApiOperationShape),
});

const OpenApiDocumentShape = v.object({
  openapi: v.string(),
  info: v.object({ title: v.string(), version: v.string() }),
  servers: v.optional(v.array(v.object({ url: v.string() }))),
  paths: v.record(v.string(), OpenApiPathItemShape),
  components: v.optional(
    v.object({
      schemas: v.optional(v.record(v.string(), OpenApiSchemaShape)),
    }),
  ),
});

export type OpenApiParameter = v.InferOutput<typeof OpenApiParameterShape>;
export type OpenApiMediaType = v.InferOutput<typeof OpenApiMediaTypeShape>;
export type OpenApiRequestBody = v.InferOutput<typeof OpenApiRequestBodyShape>;
export type OpenApiResponse = v.InferOutput<typeof OpenApiResponseShape>;
export type OpenApiOperation = v.InferOutput<typeof OpenApiOperationShape>;
export type OpenApiPathItem = v.InferOutput<typeof OpenApiPathItemShape>;
export type OpenApiDocument = v.InferOutput<typeof OpenApiDocumentShape>;
export type OpenApiMethod = (typeof METHODS)[number];

export const OPEN_API_METHODS: readonly OpenApiMethod[] = METHODS;

export type ServiceCatalogEntry = {
  readonly id: string; // kebab-case service identifier, used in anchors (#svc-<id>)
  readonly title: string; // display label
  readonly subdomain: string | undefined; // billing.api.<domain>, etc. — undefined for internal-only services
  readonly publicSurface: boolean;
  readonly document: OpenApiDocument;
};

function parseDocument(yaml: string, label: string): OpenApiDocument {
  return (
    v.parse(OpenApiDocumentShape, parseYaml(yaml), { abortEarly: false }) ??
    (() => {
      throw new Error(`OpenAPI spec ${label} failed validation`);
    })()
  );
}

// Order here is the order sections render on /docs/reference and the
// order they appear in the docs rail. Keep the customer-facing services
// first (*.api.<domain>) so the most-read pages are at the top of the TOC.
export const SERVICE_CATALOG: readonly ServiceCatalogEntry[] = [
  {
    id: "sandbox-rental",
    title: "Sandbox Rental",
    subdomain: "sandbox.api",
    publicSurface: true,
    document: parseDocument(sandboxRentalYaml, "sandbox-rental-service"),
  },
  {
    id: "identity",
    title: "Identity",
    subdomain: "iam.api",
    publicSurface: true,
    document: parseDocument(identityYaml, "iam-service"),
  },
  {
    id: "mailbox",
    title: "Mailbox",
    subdomain: "mail.api",
    publicSurface: true,
    document: parseDocument(mailboxYaml, "mailbox-service"),
  },
  {
    id: "billing",
    title: "Billing",
    subdomain: "billing.api",
    publicSurface: true,
    document: parseDocument(billingYaml, "billing-service"),
  },
];

// Docs-rail entries derived from the catalog so services added/removed in
// SERVICE_CATALOG automatically update the nav. `schemas` is a fixed
// trailing anchor pointing at the unified Schemas section.
export const REFERENCE_SECTIONS: ReadonlyArray<{ readonly id: string; readonly label: string }> = [
  ...SERVICE_CATALOG.map((s) => ({ id: `svc-${s.id}`, label: s.title })),
  { id: "schemas", label: "Schemas" },
];
