const MAX_BODY_BYTES = 128 * 1024;
const MAX_FIELD_BYTES = 512;
const MAX_MESSAGE_BYTES = 1024;
const CORRELATION_COOKIE_NAME = "verself_correlation_id";

type BrowserEventKind =
  | "csp_violation"
  | "browser_runtime_error"
  | "browser_unhandled_rejection"
  | "browser_security_policy_violation"
  | "browser_report";

interface BrowserEventRow {
  readonly time: string;
  readonly level: "error";
  readonly msg: "browser observability event";
  readonly event_kind: BrowserEventKind;
  readonly event_source: string;
  readonly report_type: string;
  readonly document_url: string;
  readonly route_path: string;
  readonly blocked_url: string;
  readonly source_url: string;
  readonly effective_directive: string;
  readonly violated_directive: string;
  readonly disposition: string;
  readonly status_code: number;
  readonly line_number: number;
  readonly column_number: number;
  readonly error_name: string;
  readonly error_message: string;
  readonly verself_correlation_id: string;
  readonly traceparent: string;
  readonly user_agent: string;
  readonly body_bytes: number;
}

type UnknownRecord = Record<string, unknown>;

function isRecord(value: unknown): value is UnknownRecord {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function stringField(value: unknown, limit = MAX_FIELD_BYTES): string {
  if (typeof value !== "string") return "";
  return value.trim().slice(0, limit);
}

function numberField(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value)) return Math.trunc(value);
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

function nestedField(input: UnknownRecord, ...names: Array<string>): unknown {
  for (const name of names) {
    if (Object.prototype.hasOwnProperty.call(input, name)) {
      return input[name];
    }
  }
  return undefined;
}

function safeURL(value: unknown): string {
  const raw = stringField(value, 2048);
  if (raw === "") return "";
  if (raw === "inline" || raw === "eval" || raw === "self" || raw === "about") {
    return raw;
  }
  if (raw.startsWith("data:")) return "data:";
  if (raw.startsWith("blob:")) return "blob:";
  try {
    const url = new URL(raw, "https://verself.sh");
    return `${url.origin}${url.pathname}`.slice(0, MAX_FIELD_BYTES);
  } catch {
    return raw.slice(0, MAX_FIELD_BYTES);
  }
}

function routePathFromURL(value: string): string {
  if (value === "") return "";
  try {
    return new URL(value, "https://verself.sh").pathname.slice(0, MAX_FIELD_BYTES);
  } catch {
    return "";
  }
}

function cookieValue(cookieHeader: string, name: string): string {
  for (const part of cookieHeader.split(";")) {
    const index = part.indexOf("=");
    if (index === -1) continue;
    const key = part.slice(0, index).trim();
    if (key !== name) continue;
    return decodeURIComponent(part.slice(index + 1).trim()).slice(0, MAX_FIELD_BYTES);
  }
  return "";
}

function baseRow(request: Request, eventSource: string, bodyBytes: number): BrowserEventRow {
  return {
    time: new Date().toISOString(),
    level: "error",
    msg: "browser observability event",
    event_kind: "browser_report",
    event_source: eventSource,
    report_type: "",
    document_url: "",
    route_path: "",
    blocked_url: "",
    source_url: "",
    effective_directive: "",
    violated_directive: "",
    disposition: "",
    status_code: 0,
    line_number: 0,
    column_number: 0,
    error_name: "",
    error_message: "",
    verself_correlation_id: cookieValue(
      request.headers.get("cookie") ?? "",
      CORRELATION_COOKIE_NAME,
    ),
    traceparent: stringField(request.headers.get("traceparent") ?? "", 255),
    user_agent: stringField(request.headers.get("user-agent") ?? "", 512),
    body_bytes: bodyBytes,
  };
}

function cspRow(
  request: Request,
  eventSource: string,
  bodyBytes: number,
  reportType: string,
  input: UnknownRecord,
): BrowserEventRow {
  const documentURL = safeURL(
    nestedField(input, "document-uri", "documentURL", "documentUrl", "url"),
  );
  return {
    ...baseRow(request, eventSource, bodyBytes),
    event_kind: "csp_violation",
    report_type: reportType,
    document_url: documentURL,
    route_path: routePathFromURL(documentURL),
    blocked_url: safeURL(nestedField(input, "blocked-uri", "blockedURL", "blockedUrl")),
    source_url: safeURL(nestedField(input, "source-file", "sourceFile")),
    effective_directive: stringField(
      nestedField(input, "effective-directive", "effectiveDirective"),
    ),
    violated_directive: stringField(nestedField(input, "violated-directive", "violatedDirective")),
    disposition: stringField(nestedField(input, "disposition")),
    status_code: numberField(nestedField(input, "status-code", "statusCode")),
    line_number: numberField(nestedField(input, "line-number", "lineNumber", "lineno")),
    column_number: numberField(nestedField(input, "column-number", "columnNumber", "colno")),
  };
}

function runtimeRow(
  request: Request,
  eventSource: string,
  bodyBytes: number,
  input: UnknownRecord,
): BrowserEventRow {
  const kind = stringField(input.kind);
  const eventKind =
    kind === "unhandled_rejection"
      ? "browser_unhandled_rejection"
      : kind === "security_policy_violation"
        ? "browser_security_policy_violation"
        : "browser_runtime_error";
  const documentURL = safeURL(nestedField(input, "document_url", "documentURL"));
  return {
    ...baseRow(request, eventSource, bodyBytes),
    event_kind: eventKind,
    report_type: kind,
    document_url: documentURL,
    route_path: routePathFromURL(documentURL),
    blocked_url: safeURL(nestedField(input, "blocked_url", "blockedURL")),
    source_url: safeURL(nestedField(input, "source_url", "sourceURL", "filename")),
    effective_directive: stringField(
      nestedField(input, "effective_directive", "effectiveDirective"),
    ),
    violated_directive: stringField(nestedField(input, "violated_directive", "violatedDirective")),
    disposition: stringField(input.disposition),
    status_code: numberField(nestedField(input, "status_code", "statusCode")),
    line_number: numberField(nestedField(input, "line_number", "lineNumber", "lineno")),
    column_number: numberField(nestedField(input, "column_number", "columnNumber", "colno")),
    error_name: stringField(nestedField(input, "error_name", "errorName")),
    error_message: stringField(nestedField(input, "message", "error_message"), MAX_MESSAGE_BYTES),
  };
}

function rowsFromJSON(
  request: Request,
  eventSource: string,
  bodyBytes: number,
  input: unknown,
): BrowserEventRow[] {
  if (Array.isArray(input)) {
    return input.flatMap((report) => {
      if (!isRecord(report)) return [];
      const reportType = stringField(report.type);
      const body = isRecord(report.body) ? report.body : report;
      if (reportType === "csp-violation" || isLikelyCSPReport(body)) {
        return [cspRow(request, eventSource, bodyBytes, reportType || "csp-violation", body)];
      }
      return [
        {
          ...baseRow(request, eventSource, bodyBytes),
          report_type: reportType,
          document_url: safeURL(report.url),
          route_path: routePathFromURL(safeURL(report.url)),
        },
      ];
    });
  }
  if (!isRecord(input)) return [];
  if (isRecord(input["csp-report"])) {
    return [cspRow(request, eventSource, bodyBytes, "csp-report", input["csp-report"])];
  }
  if (stringField(input.kind) !== "") {
    return [runtimeRow(request, eventSource, bodyBytes, input)];
  }
  if (isLikelyCSPReport(input)) {
    return [cspRow(request, eventSource, bodyBytes, "csp-violation", input)];
  }
  return [{ ...baseRow(request, eventSource, bodyBytes), report_type: "unknown" }];
}

function isLikelyCSPReport(input: UnknownRecord): boolean {
  return (
    nestedField(input, "effective-directive", "effectiveDirective") !== undefined ||
    nestedField(input, "violated-directive", "violatedDirective") !== undefined ||
    nestedField(input, "blocked-uri", "blockedURL", "blockedUrl") !== undefined
  );
}

async function readBody(request: Request): Promise<ArrayBuffer> {
  const body = await request.arrayBuffer();
  if (body.byteLength === 0) {
    throw new Response("empty body", {
      status: 400,
      headers: { "content-type": "text/plain" },
    });
  }
  if (body.byteLength > MAX_BODY_BYTES) {
    throw new Response("payload too large", {
      status: 413,
      headers: { "content-type": "text/plain" },
    });
  }
  return body;
}

export async function ingestBrowserEvent(request: Request, eventSource: string): Promise<Response> {
  const contentType = request.headers.get("content-type") ?? "";
  if (
    !contentType.startsWith("application/json") &&
    !contentType.startsWith("application/csp-report") &&
    !contentType.startsWith("application/reports+json")
  ) {
    return new Response("expected json browser report", {
      status: 415,
      headers: { "content-type": "text/plain" },
    });
  }

  let body: ArrayBuffer;
  try {
    body = await readBody(request);
  } catch (error) {
    if (error instanceof Response) return error;
    throw error;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(body));
  } catch {
    return new Response("invalid json", {
      status: 400,
      headers: { "content-type": "text/plain" },
    });
  }

  const rows = rowsFromJSON(request, eventSource, body.byteLength, parsed);
  for (const row of rows) {
    console.error(JSON.stringify(row));
  }

  return new Response(null, { status: 204 });
}
