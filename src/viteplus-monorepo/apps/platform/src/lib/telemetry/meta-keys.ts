// Meta-tag names used to bridge SSR-known deploy attributes onto the browser
// OTel resource. The platform service reads FORGE_METAL_DEPLOY_* from systemd
// env, head() emits these meta tags into the SSR HTML, and browser.ts reads
// them on init so every span carries the right `forge_metal.*` ResourceAttributes.
//
// Keep names in sync between server-deploy-meta.ts (writer) and browser.ts (reader).

export const DEPLOY_META = {
  runKey: "forge-metal:deploy-run-key",
  id: "forge-metal:deploy-id",
  commitSha: "forge-metal:commit-sha",
  profile: "forge-metal:deploy-profile",
} as const;

export const RESOURCE_ATTR_KEYS = {
  runKey: "forge_metal.deploy_run_key",
  id: "forge_metal.deploy_id",
  commitSha: "forge_metal.commit_sha",
  profile: "forge_metal.deploy_profile",
} as const;
