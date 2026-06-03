// Meta-tag names used to bridge SSR-known deploy attributes onto the browser
// OTel resource. The platform service reads VERSELF_SITE,
// VERSELF_DEPLOY_*, and VERSELF_SUPERVISOR from its Nomad env, head() emits these meta tags into
// the SSR HTML, and browser.ts reads
// them on init so every span carries the right `verself.*` ResourceAttributes.
//
// Keep names in sync between server-deploy-meta.ts (writer) and browser.ts (reader).

export const DEPLOY_META = {
  site: "verself:site",
  runKey: "verself:deploy-run-key",
  id: "verself:deploy-id",
  commitSha: "verself:commit-sha",
  supervisor: "verself:supervisor",
} as const;

export const RESOURCE_ATTR_KEYS = {
  site: "verself.site",
  runKey: "verself.deploy_run_key",
  id: "verself.deploy_id",
  commitSha: "verself.commit_sha",
  supervisor: "verself.supervisor",
} as const;
