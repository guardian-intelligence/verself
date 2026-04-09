export const DEFAULT_EXECUTION_REF = "refs/heads/main";

export function validateExecutionRepoUrl(value: string): string | undefined {
  if (!value) return "Repository URL is required";
  return undefined;
}

export function validateExecutionRef(value: string): string | undefined {
  if (!value) return "Ref is required";
  if (!value.startsWith("refs/")) return "Ref must look like refs/heads/main";
  return undefined;
}
