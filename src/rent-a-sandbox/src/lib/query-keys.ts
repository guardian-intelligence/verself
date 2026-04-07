export const keys = {
  balance: () => ["billing", "balance"] as const,
  subscriptions: () => ["billing", "subscriptions"] as const,
  grants: (active?: boolean) => ["billing", "grants", { active }] as const,
  jobs: () => ["jobs"] as const,
  job: (id: string) => ["jobs", id] as const,
  jobLogs: (id: string) => ["jobs", id, "logs"] as const,
};
