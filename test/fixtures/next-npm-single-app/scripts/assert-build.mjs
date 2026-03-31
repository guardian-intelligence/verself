import { access } from "node:fs/promises";

await access(".next/BUILD_ID");
console.log("build artifact ok");
