import pg from "pg";

const client = new pg.Client({
  host: "127.0.0.1",
  user: "postgres",
  database: "postgres",
  ssl: false
});

await client.connect();
const result = await client.query("select 1 as ok");
await client.end();

if (result.rows[0]?.ok !== 1) {
  throw new Error("expected postgres query to return 1");
}

console.log("postgres ok");

