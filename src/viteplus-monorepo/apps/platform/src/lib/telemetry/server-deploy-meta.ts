import { DEPLOY_META } from "./meta-keys";

export interface DeployMetaTag {
  readonly name: string;
  readonly content: string;
}

// Server-only reader. `import.meta.env.SSR` is replaced at build time so the
// process.env access is dead code on the client and tree-shaken away. Returns
// the head() meta-tag list to embed in SSR HTML, where browser.ts can read it.
export function deployMetaTags(): DeployMetaTag[] {
  if (!import.meta.env.SSR) {
    return [];
  }
  const env = process.env;
  const tags: DeployMetaTag[] = [
    { name: DEPLOY_META.runKey, content: env.VERSELF_DEPLOY_RUN_KEY ?? "" },
    { name: DEPLOY_META.id, content: env.VERSELF_DEPLOY_ID ?? "" },
    { name: DEPLOY_META.commitSha, content: env.VERSELF_COMMIT_SHA ?? "" },
    { name: DEPLOY_META.profile, content: env.VERSELF_DEPLOY_PROFILE ?? "" },
  ];
  return tags.filter((tag) => tag.content !== "");
}
