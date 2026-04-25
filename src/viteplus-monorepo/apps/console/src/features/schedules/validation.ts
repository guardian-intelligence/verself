export const DEFAULT_INTERVAL_SECONDS = 60;

export function validateScheduleCommand(value: string) {
  if (!value.trim()) {
    return "Run command is required";
  }
  if (value.length > 8192) {
    return "Run command must be 8192 characters or fewer";
  }
  return undefined;
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
