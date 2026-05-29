const DEVELOPMENT_MODE_PARAM = "developmentMode";
const DEVELOPMENT_MODE_VALUE_LITERAL = "1";

export function isDevelopmentModeEnabled(): boolean {
  if (typeof window === "undefined") return false;
  return (
    new URLSearchParams(window.location.search).get(DEVELOPMENT_MODE_PARAM) ===
    DEVELOPMENT_MODE_VALUE_LITERAL
  );
}

export const DEVELOPMENT_MODE_PARAM_NAME = DEVELOPMENT_MODE_PARAM;
export const DEVELOPMENT_MODE_VALUE = DEVELOPMENT_MODE_VALUE_LITERAL;
