import { ChevronRight } from "lucide-react";

import { Badge } from "@verself/ui/components/ui/badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@verself/ui/components/ui/collapsible";
import { cn } from "@verself/ui/lib/utils";

import {
  OPEN_API_METHODS,
  type OpenApiMethod,
  type OpenApiOperation,
  type OpenApiParameter,
  type OpenApiPathItem,
  type OpenApiResponse,
  type OpenApiSchema,
  type ServiceCatalogEntry,
} from "~/lib/openapi-catalog";

type MethodVariant = "info" | "success" | "warning" | "destructive" | "secondary";

const METHOD_VARIANT: Record<OpenApiMethod, MethodVariant> = {
  get: "info",
  post: "success",
  put: "warning",
  patch: "secondary",
  delete: "destructive",
};

type OperationEntry = {
  readonly path: string;
  readonly method: OpenApiMethod;
  readonly operation: OpenApiOperation;
};

// ─── service section ────────────────────────────────────────────────────────

export function ServiceSection({
  service,
  publicOrigin,
}: {
  service: ServiceCatalogEntry;
  publicOrigin: string;
}) {
  const doc = service.document;
  const operations = collectOperations(doc.paths);
  const schemas = doc.components?.schemas ?? {};

  return (
    <section id={`svc-${service.id}`} className="flex flex-col gap-4 border-t border-border pt-8">
      <header className="flex flex-col gap-2">
        <h2 className="text-2xl font-semibold tracking-tight">{service.title}</h2>
        <p className="text-sm leading-6 text-muted-foreground">
          {doc.info.title} v{doc.info.version}
          {service.subdomain ? (
            <>
              . Served at{" "}
              <code className="rounded bg-secondary px-1.5 py-0.5 text-[0.7rem]">
                https://{service.subdomain}.{publicOrigin}
              </code>
              .
            </>
          ) : service.publicSurface ? (
            "."
          ) : (
            ". Internal control-plane API; reached from other Verself services, not directly by customers."
          )}
        </p>
      </header>

      <div className="flex flex-col gap-3">
        {operations.map((entry) => (
          <OperationCard
            key={`${entry.method}-${entry.path}`}
            entry={entry}
            schemas={schemas}
            serviceId={service.id}
          />
        ))}
      </div>
    </section>
  );
}

// ─── schemas section ────────────────────────────────────────────────────────

export function SchemasSection({ services }: { services: readonly ServiceCatalogEntry[] }) {
  return (
    <section id="schemas" className="flex flex-col gap-4 border-t border-border pt-8">
      <header className="flex flex-col gap-2">
        <h2 className="text-2xl font-semibold tracking-tight">Schemas</h2>
        <p className="text-sm leading-6 text-muted-foreground">
          Shared object shapes referenced by operations above. Grouped by service so
          identically-named types across services don't collide.
        </p>
      </header>

      <div className="flex flex-col gap-6">
        {services.map((service) => {
          const schemas = service.document.components?.schemas ?? {};
          const schemaNames = Object.keys(schemas).sort((a, b) => a.localeCompare(b));
          if (schemaNames.length === 0) return null;
          return (
            <div key={service.id} className="flex flex-col gap-2">
              <h3 className="text-[0.7rem] font-semibold uppercase tracking-wide text-muted-foreground">
                {service.title}
              </h3>
              <div className="flex flex-col gap-2">
                {schemaNames.map((name) => {
                  const schema = schemas[name];
                  if (!schema) return null;
                  return (
                    <SchemaBlock
                      key={name}
                      name={name}
                      schema={schema}
                      schemas={schemas}
                      serviceId={service.id}
                    />
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

// ─── operation card ─────────────────────────────────────────────────────────

function OperationCard({
  entry,
  schemas,
  serviceId,
}: {
  entry: OperationEntry;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
}) {
  const { method, path, operation } = entry;
  const parameters = operation.parameters ?? [];
  const requestBody = operation.requestBody;
  const responses = operation.responses ?? {};
  const responseCodes = Object.keys(responses).sort((a, b) => {
    if (a === "default") return 1;
    if (b === "default") return -1;
    return a.localeCompare(b);
  });

  return (
    <article
      id={operation.operationId ?? `${method}-${path}`}
      className="rounded-lg border border-border bg-card"
    >
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
        <Badge
          variant={METHOD_VARIANT[method]}
          className="h-5 px-2 font-mono uppercase tracking-wide"
        >
          {method}
        </Badge>
        <code className="min-w-0 flex-1 break-all font-mono text-xs text-foreground">{path}</code>
      </header>

      <div className="flex flex-col gap-5 px-4 py-4">
        {operation.summary && (
          <p className="text-sm leading-6 text-foreground">{operation.summary}</p>
        )}

        {parameters.length > 0 && (
          <Subsection label="Parameters">
            <ul className="divide-y divide-border rounded-md border border-border">
              {parameters.map((param) => (
                <ParameterRow
                  key={`${param.in}-${param.name}`}
                  param={param}
                  schemas={schemas}
                  serviceId={serviceId}
                />
              ))}
            </ul>
          </Subsection>
        )}

        {requestBody?.content && (
          <Subsection label="Request Body" suffix={requestBody.required ? "required" : undefined}>
            {Object.entries(requestBody.content).map(([mediaType, media]) => (
              <MediaBlock
                key={mediaType}
                mediaType={mediaType}
                schema={media.schema}
                schemas={schemas}
                serviceId={serviceId}
              />
            ))}
          </Subsection>
        )}

        <Subsection label="Responses">
          <ul className="flex flex-col gap-2">
            {responseCodes.map((code) => {
              const response = responses[code];
              if (!response) return null;
              return (
                <ResponseRow
                  key={code}
                  code={code}
                  response={response}
                  schemas={schemas}
                  serviceId={serviceId}
                />
              );
            })}
          </ul>
        </Subsection>
      </div>
    </article>
  );
}

function Subsection({
  label,
  suffix,
  children,
}: {
  label: string;
  suffix?: string | undefined;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="flex items-center gap-2 text-[0.7rem] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
        {suffix && (
          <span className="font-medium normal-case tracking-normal text-muted-foreground/70">
            · {suffix}
          </span>
        )}
      </h3>
      {children}
    </section>
  );
}

function ParameterRow({
  param,
  schemas,
  serviceId,
}: {
  param: OpenApiParameter;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
}) {
  return (
    <li className="flex flex-col gap-1 px-3 py-2.5 text-sm">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <code className="font-mono text-xs text-foreground">{param.name}</code>
        <span className="text-[0.7rem] uppercase tracking-wide text-muted-foreground">
          {param.in}
        </span>
        {param.required && (
          <span className="text-[0.7rem] font-medium uppercase tracking-wide text-destructive">
            required
          </span>
        )}
        {param.schema && (
          <span className="text-xs text-muted-foreground">
            · {describeType(param.schema, schemas, serviceId)}
          </span>
        )}
      </div>
      {param.description && (
        <p className="text-xs leading-5 text-muted-foreground">{param.description}</p>
      )}
    </li>
  );
}

function ResponseRow({
  code,
  response,
  schemas,
  serviceId,
}: {
  code: string;
  response: OpenApiResponse;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
}) {
  const variant: MethodVariant =
    code === "default" || code.startsWith("5")
      ? "destructive"
      : code.startsWith("4")
        ? "warning"
        : code.startsWith("2")
          ? "success"
          : "secondary";
  const contentEntries = response.content ? Object.entries(response.content) : [];

  return (
    <li className="rounded-md border border-border">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <Badge variant={variant} className="h-5 px-2 font-mono">
          {code}
        </Badge>
        {response.description && (
          <span className="text-xs text-muted-foreground">{response.description}</span>
        )}
      </div>
      {contentEntries.length > 0 && (
        <div className="flex flex-col gap-2 px-3 py-2">
          {contentEntries.map(([mediaType, media]) => (
            <MediaBlock
              key={mediaType}
              mediaType={mediaType}
              schema={media.schema}
              schemas={schemas}
              serviceId={serviceId}
            />
          ))}
        </div>
      )}
    </li>
  );
}

function MediaBlock({
  mediaType,
  schema,
  schemas,
  serviceId,
}: {
  mediaType: string;
  schema?: OpenApiSchema | undefined;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
}) {
  if (!schema) {
    return (
      <p className="text-xs text-muted-foreground">
        <code className="font-mono">{mediaType}</code> · no schema
      </p>
    );
  }
  const refName = schema.$ref ? refShortName(schema.$ref) : undefined;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <code className="font-mono text-muted-foreground">{mediaType}</code>
        {refName && (
          <a
            href={`#schema-${serviceId}-${refName}`}
            className="font-mono font-medium text-foreground underline-offset-2 hover:underline"
          >
            {refName}
          </a>
        )}
      </div>
      <SchemaTree schema={schema} schemas={schemas} serviceId={serviceId} hideHeader />
    </div>
  );
}

// ─── schema tree ────────────────────────────────────────────────────────────

function SchemaBlock({
  name,
  schema,
  schemas,
  serviceId,
}: {
  name: string;
  schema: OpenApiSchema;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
}) {
  return (
    <Collapsible className="rounded-md border border-border" id={`schema-${serviceId}-${name}`}>
      <CollapsibleTrigger className="group flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-accent">
        <ChevronRight className="size-3.5 text-muted-foreground transition-transform group-data-[panel-open]:rotate-90" />
        <code className="font-mono text-xs font-medium text-foreground">{name}</code>
        <span className="text-xs text-muted-foreground">
          {describeType(schema, schemas, serviceId)}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="border-t border-border px-3 py-2">
          <SchemaTree
            schema={schema}
            schemas={schemas}
            serviceId={serviceId}
            initialDepth={0}
            hideHeader
          />
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function SchemaTree({
  schema,
  schemas,
  serviceId,
  initialDepth = 0,
  hideHeader = false,
}: {
  schema: OpenApiSchema;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
  initialDepth?: number;
  hideHeader?: boolean;
}) {
  return (
    <SchemaNode
      schema={schema}
      schemas={schemas}
      serviceId={serviceId}
      depth={initialDepth}
      seen={new Set()}
      hideHeader={hideHeader}
    />
  );
}

function SchemaNode({
  schema,
  schemas,
  serviceId,
  depth,
  required,
  seen,
  name,
  hideHeader = false,
}: {
  schema: OpenApiSchema;
  schemas: Readonly<Record<string, OpenApiSchema>>;
  serviceId: string;
  depth: number;
  required?: boolean | undefined;
  seen: ReadonlySet<string>;
  name?: string | undefined;
  hideHeader?: boolean;
}) {
  const refName = schema.$ref ? refShortName(schema.$ref) : undefined;
  const resolved = schema.$ref ? resolveRef(schema.$ref, schemas) : schema;

  // Cycle guard: if we've already expanded this $ref in the current chain,
  // render it as a link only so infinite recursion can't land us in the
  // browser's laps.
  if (refName && seen.has(refName)) {
    return (
      <SchemaLeaf
        name={name}
        required={required}
        summary={describeRef(refName, resolved, schemas, serviceId)}
      />
    );
  }
  const nextSeen = refName ? new Set(seen).add(refName) : seen;
  const effective = resolved ?? schema;
  const type = arrayFirst(effective.type);

  if (type === "object" && effective.properties) {
    const requiredFields = new Set(effective.required ?? []);
    const propertyEntries = Object.entries(effective.properties).filter(
      ([propName]) => !propName.startsWith("$"),
    );
    const showHeader = !hideHeader && (name !== undefined || refName !== undefined);
    return (
      <div className="flex flex-col gap-1">
        {showHeader && (
          <SchemaLeaf
            name={name}
            required={required}
            summary={refName ?? "object"}
            description={effective.description}
          />
        )}
        <ul className={cn("flex flex-col gap-1.5", depth > 0 && "border-l border-border pl-3")}>
          {propertyEntries.map(([propName, propSchema]) => (
            <li key={propName}>
              <SchemaNode
                schema={propSchema}
                schemas={schemas}
                serviceId={serviceId}
                depth={depth + 1}
                required={requiredFields.has(propName)}
                seen={nextSeen}
                name={propName}
              />
            </li>
          ))}
        </ul>
      </div>
    );
  }

  if (type === "array" && effective.items) {
    return (
      <div className="flex flex-col gap-1">
        <SchemaLeaf
          name={name}
          required={required}
          summary={`array of ${describeType(effective.items, schemas, serviceId)}`}
          description={effective.description}
        />
        {isObjectLike(effective.items, schemas) && (
          <ul className="flex flex-col gap-1.5 border-l border-border pl-3">
            <li>
              <SchemaNode
                schema={effective.items}
                schemas={schemas}
                serviceId={serviceId}
                depth={depth + 1}
                seen={nextSeen}
                hideHeader
              />
            </li>
          </ul>
        )}
      </div>
    );
  }

  return (
    <SchemaLeaf
      name={name}
      required={required}
      summary={describeType(effective, schemas, serviceId)}
      description={effective.description}
    />
  );
}

function SchemaLeaf({
  name,
  required,
  summary,
  description,
}: {
  name?: string | undefined;
  required?: boolean | undefined;
  summary: string;
  description?: string | undefined;
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        {name !== undefined && <code className="font-mono text-xs text-foreground">{name}</code>}
        <span className="text-xs text-muted-foreground">{summary}</span>
        {required && (
          <span className="text-[0.65rem] font-medium uppercase tracking-wide text-destructive">
            required
          </span>
        )}
      </div>
      {description && (
        <p className="text-[0.7rem] leading-4 text-muted-foreground/90">{description}</p>
      )}
    </div>
  );
}

// ─── helpers ────────────────────────────────────────────────────────────────

function collectOperations(
  paths: Readonly<Record<string, OpenApiPathItem>>,
): readonly OperationEntry[] {
  const entries: OperationEntry[] = [];
  for (const [path, item] of Object.entries(paths)) {
    for (const method of OPEN_API_METHODS) {
      const operation = item[method];
      if (operation) entries.push({ path, method, operation });
    }
  }
  entries.sort((a, b) => a.path.localeCompare(b.path) || a.method.localeCompare(b.method));
  return entries;
}

function describeType(
  schema: OpenApiSchema,
  schemas: Readonly<Record<string, OpenApiSchema>>,
  serviceId: string,
): string {
  if (schema.$ref) {
    const refName = refShortName(schema.$ref);
    const resolved = resolveRef(schema.$ref, schemas);
    return describeRef(refName, resolved, schemas, serviceId);
  }
  const type = arrayFirst(schema.type);
  if (type === "array" && schema.items) {
    return `array of ${describeType(schema.items, schemas, serviceId)}`;
  }
  if (schema.enum) {
    return `enum (${schema.enum.map(String).join(" | ")})`;
  }
  if (schema.format) return `${type ?? "any"} · ${schema.format}`;
  return type ?? "any";
}

function describeRef(
  refName: string,
  resolved: OpenApiSchema | undefined,
  schemas: Readonly<Record<string, OpenApiSchema>>,
  serviceId: string,
): string {
  if (!resolved) return refName;
  const type = arrayFirst(resolved.type);
  if (type === "object") return refName;
  return `${refName} · ${describeType(resolved, schemas, serviceId)}`;
}

function resolveRef(
  ref: string,
  schemas: Readonly<Record<string, OpenApiSchema>>,
): OpenApiSchema | undefined {
  const prefix = "#/components/schemas/";
  if (!ref.startsWith(prefix)) return undefined;
  return schemas[ref.slice(prefix.length)];
}

function refShortName(ref: string): string {
  const idx = ref.lastIndexOf("/");
  return idx === -1 ? ref : ref.slice(idx + 1);
}

function arrayFirst(value: string | readonly string[] | undefined): string | undefined {
  if (value === undefined) return undefined;
  if (typeof value === "string") return value;
  return value[0];
}

function isObjectLike(
  schema: OpenApiSchema,
  schemas: Readonly<Record<string, OpenApiSchema>>,
): boolean {
  if (schema.$ref) {
    const resolved = resolveRef(schema.$ref, schemas);
    return resolved ? arrayFirst(resolved.type) === "object" : false;
  }
  return arrayFirst(schema.type) === "object";
}
