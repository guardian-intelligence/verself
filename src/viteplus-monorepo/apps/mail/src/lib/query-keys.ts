export const keys = {
  account: () => ["mail", "account"] as const,
  emailBody: (emailId: string) => ["mail", "body", emailId] as const,
  syncStatus: () => ["mail", "sync-status"] as const,
};
