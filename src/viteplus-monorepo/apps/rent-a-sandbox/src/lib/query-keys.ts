export const keys = {
  user: () => ["auth", "user"] as const,
  balance: () => ["billing", "balance"] as const,
  subscriptions: () => ["billing", "subscriptions"] as const,
  grants: (active?: boolean) => ["billing", "grants", { active }] as const,
  repos: () => ["repos"] as const,
  repo: (id: string) => ["repos", id] as const,
  repoGenerations: (id: string) => ["repos", id, "generations"] as const,
  jobs: () => ["jobs"] as const,
  job: (id: string) => ["jobs", id] as const,
  jobLogs: (id: string) => ["jobs", id, "logs"] as const,
};
