// Meta-tag names used to bridge SSR-known deploy attributes onto the browser
// OTel resource. The company service reads VERSELF_DEPLOY_* plus
// VERSELF_SUPERVISOR from its Nomad env, head() emits these meta tags into
// the SSR HTML, and browser.ts reads
// them on init so every span carries the right `verself.*` ResourceAttributes.
//
// Keep names in sync between server-deploy-meta.ts (writer) and browser.ts (reader).

export const DEPLOY_META = {
  runKey: "verself:deploy-run-key",
  id: "verself:deploy-id",
  commitSha: "verself:commit-sha",
  supervisor: "verself:supervisor",
} as const;

export const RESOURCE_ATTR_KEYS = {
  runKey: "verself.deploy_run_key",
  id: "verself.deploy_id",
  commitSha: "verself.commit_sha",
  supervisor: "verself.supervisor",
} as const;
