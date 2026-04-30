import postgres from "postgres";
import { readFileSync } from "node:fs";

import { getAuthConfig } from "./auth";

const schemaPath = new URL("./schema/auth_sessions.sql", import.meta.url);

async function main() {
  const config = getAuthConfig();
  const sql = postgres(config.sessionDatabaseURL, {
    max: 1,
    prepare: false,
  });
  try {
    await sql.unsafe(readFileSync(schemaPath, "utf8"));
    console.log("auth-web session schema applied");
  } finally {
    await sql.end({ timeout: 5 });
  }
}

await main();
