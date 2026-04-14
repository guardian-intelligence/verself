export const DEFAULT_RUN_COMMAND = "echo hello";

export function validateRunCommand(value: string): string | undefined {
  if (!value.trim()) return "Run command is required";
  return undefined;
}
