export const DEFAULT_INTERVAL_SECONDS = 60;

const workflowPathPattern = /^\.((forgejo)|(github))\/workflows\/[^/][^]*\.ya?ml$/;

export function validateWorkflowPath(value: string) {
  if (!value.trim()) {
    return "Workflow path is required";
  }
  if (value.length > 512) {
    return "Workflow path must be 512 characters or fewer";
  }
  if (!workflowPathPattern.test(value.trim())) {
    return "Workflow path must point to .forgejo/workflows/*.yml or .github/workflows/*.yml";
  }
  return undefined;
}

export function validateSourceRepositoryID(value: string) {
  if (!value.trim()) {
    return "Repository is required";
  }
  return undefined;
}

export function validateRef(value: string) {
  if (value.length > 255) {
    return "Ref must be 255 characters or fewer";
  }
  return undefined;
}

export function parseScheduleInputs(value: string): Record<string, string> {
  const trimmed = value.trim();
  if (!trimmed) return {};

  const parsed: unknown = JSON.parse(trimmed);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new TypeError("Inputs must be a JSON object");
  }
  const entries = Object.entries(parsed);
  if (entries.some(([, entryValue]) => typeof entryValue !== "string")) {
    throw new TypeError("Inputs values must be strings");
  }
  return Object.fromEntries(entries) as Record<string, string>;
}

export function validateScheduleInputs(value: string) {
  try {
    parseScheduleInputs(value);
    return undefined;
  } catch {
    return "Inputs must be a JSON object with string values";
  }
}

export function validateIntervalSeconds(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "Interval is required";
  }
  const parsed = Number(trimmed);
  if (!Number.isInteger(parsed) || parsed < 15) {
    return "Interval must be an integer of at least 15 seconds";
  }
  return undefined;
}
