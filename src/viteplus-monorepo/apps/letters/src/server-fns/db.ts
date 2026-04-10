import { requireURLFromEnv } from "@forge-metal/web-env";

export type LettersSql = import("postgres").Sql<Record<string, unknown>>;

export async function withLettersDb<T>(fn: (sql: LettersSql) => Promise<T>): Promise<T> {
  const { default: postgres } = await import("postgres");
  const sql = postgres(requireURLFromEnv("DATABASE_URL"), { max: 5 });
  try {
    return await fn(sql);
  } finally {
    await sql.end();
  }
}
