import * as v from "valibot";
import { requireURLFromEnv } from "@forge-metal/web-env";

type LettersDb = import("postgres").Sql<Record<string, unknown>>;
export type JsonValue = string | number | boolean | null | JsonObject | Array<JsonValue>;
export type JsonObject = { [key: string]: JsonValue };

export const emptyPostContent: JsonObject = {
  type: "doc",
  content: [{ type: "paragraph" }],
};

const requiredText = v.string();
const optionalText = v.optional(v.string());
export const jsonValueSchema: v.GenericSchema<JsonValue> = v.lazy(() =>
  v.union([
    v.string(),
    v.number(),
    v.boolean(),
    v.null(),
    v.array(jsonValueSchema),
    v.record(v.string(), jsonValueSchema),
  ]),
);

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isProseMirrorMark(value: unknown): value is JsonObject {
  return isJsonObject(value) && typeof value.type === "string" && (!("attrs" in value) || isJsonObject(value.attrs));
}

function isProseMirrorNode(value: unknown): value is JsonObject {
  if (!isJsonObject(value) || typeof value.type !== "string") {
    return false;
  }
  if ("attrs" in value && !isJsonObject(value.attrs)) {
    return false;
  }
  if ("marks" in value && (!Array.isArray(value.marks) || !value.marks.every(isProseMirrorMark))) {
    return false;
  }
  if ("content" in value && (!Array.isArray(value.content) || !value.content.every(isProseMirrorNode))) {
    return false;
  }
  if ("text" in value && typeof value.text !== "string") {
    return false;
  }
  return true;
}

// Electric/ProseMirror content must start at a document node; scalars and raw arrays are rejected.
const optionalContent = v.optional(
  v.custom<JsonValue>((value) => isProseMirrorNode(value) && value.type === "doc"),
);
const transportInteger = v.pipe(
  v.union([v.number(), v.pipe(v.string(), v.regex(/^-?\d+$/), v.transform(Number))]),
  // Server-function payload decoding can hand numbers back as strings.
  v.integer(),
);

export const postSlugSchema = v.string();

export const createPostInputSchema = v.strictObject({
  title: requiredText,
  content: optionalContent,
  subtitle: optionalText,
  cover_image_url: optionalText,
  tags: v.optional(v.array(v.string())),
  author_name: optionalText,
});

export const updatePostInputSchema = v.strictObject({
  slug: postSlugSchema,
  title: optionalText,
  subtitle: optionalText,
  cover_image_url: optionalText,
  content: optionalContent,
  tags: v.optional(v.array(v.string())),
  author_name: optionalText,
  newSlug: v.optional(postSlugSchema),
});

export const clapPostInputSchema = v.strictObject({
  slug: postSlugSchema,
  sessionId: v.pipe(v.string(), v.regex(/^[a-f0-9]{32}$/i)),
  count: v.optional(transportInteger),
});

export const postOnlySlugInputSchema = v.strictObject({
  slug: postSlugSchema,
});

export async function withLettersDb<T>(fn: (sql: LettersDb) => Promise<T>): Promise<T> {
  const { default: postgres } = await import("postgres");
  const sql = postgres(requireURLFromEnv("DATABASE_URL"), { max: 5 });
  try {
    return await fn(sql);
  } finally {
    await sql.end();
  }
}
