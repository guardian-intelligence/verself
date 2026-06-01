export const CONSOLE_RAIL_CLASS = "w-[calc(100%-3rem)] max-w-sm";

export const CONSOLE_ABSOLUTE_RAIL_CLASS = `absolute left-1/2 ${CONSOLE_RAIL_CLASS} -translate-x-1/2`;

export const CONSOLE_PRIMARY_BACKGROUND_HEX = "#151514";
export const CONSOLE_PRIMARY_TEXT_HEX = "#d5d5d5";
export const CONSOLE_APP_SURFACE_RADIUS_CLASS = "rounded-[42px]";
export const CONSOLE_LIVE_ACTIVITY_INSET_CLASS = "px-3";

// Apple lists iPhone 16 Pro Max at 2868x1320 physical pixels. Its 3x logical
// viewport is 956x440, so 956px is the console stage height cap.
export const CONSOLE_STAGE_HEIGHT_CLASS = "h-[min(100svh,956px)]";
