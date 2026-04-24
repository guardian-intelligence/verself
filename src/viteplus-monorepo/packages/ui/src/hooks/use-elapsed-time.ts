"use client";

import * as React from "react";
import { pluralize } from "../lib/text";

export type ElapsedTimeInput = Date | number | string;

export type ElapsedTimeFormatterInput = {
  readonly direction: "future" | "past";
  readonly elapsedMs: number;
  readonly invalidLabel: string;
  readonly justNowThresholdSeconds: number;
  readonly locale: string;
  readonly nowMs: number;
  readonly pendingLabel: string;
  readonly targetMs: number;
};

export type ElapsedTimeFormatter = (input: ElapsedTimeFormatterInput) => string;

export type ElapsedTimeConfig = {
  readonly formatElapsedTime: ElapsedTimeFormatter;
  readonly invalidLabel: string;
  readonly justNowThresholdSeconds: number;
  readonly locale: string;
  readonly pendingLabel: string;
  readonly pollIntervalMs: number;
};

export type ElapsedTimeProviderProps = React.PropsWithChildren<Partial<ElapsedTimeConfig>>;

export type UseElapsedTimeOptions = Partial<ElapsedTimeConfig>;

const defaultConfig: ElapsedTimeConfig = {
  formatElapsedTime: defaultElapsedTimeFormatter,
  invalidLabel: "",
  justNowThresholdSeconds: 3,
  locale: "en-US",
  pendingLabel: "—",
  pollIntervalMs: 1_000,
};

const ElapsedTimeContext = React.createContext<ElapsedTimeConfig>(defaultConfig);

export function ElapsedTimeProvider({
  children,
  formatElapsedTime,
  invalidLabel,
  justNowThresholdSeconds,
  locale,
  pendingLabel,
  pollIntervalMs,
}: ElapsedTimeProviderProps) {
  const parent = React.useContext(ElapsedTimeContext);
  const value = React.useMemo(
    () => ({
      formatElapsedTime: formatElapsedTime ?? parent.formatElapsedTime,
      invalidLabel: invalidLabel ?? parent.invalidLabel,
      justNowThresholdSeconds: justNowThresholdSeconds ?? parent.justNowThresholdSeconds,
      locale: locale ?? parent.locale,
      pendingLabel: pendingLabel ?? parent.pendingLabel,
      pollIntervalMs: pollIntervalMs ?? parent.pollIntervalMs,
    }),
    [
      formatElapsedTime,
      invalidLabel,
      justNowThresholdSeconds,
      locale,
      parent,
      pendingLabel,
      pollIntervalMs,
    ],
  );

  return React.createElement(ElapsedTimeContext.Provider, { value }, children);
}

export function useElapsedTime(
  value: ElapsedTimeInput,
  options: UseElapsedTimeOptions = {},
): string {
  const context = React.useContext(ElapsedTimeContext);
  const config = resolveConfig(context, options);
  const nowMs = useElapsedTimeNow(config.pollIntervalMs);
  const targetMs = parseElapsedTimeInput(value);
  const elapsedMs = Math.abs(nowMs - targetMs);
  const direction = nowMs >= targetMs ? "past" : "future";

  return config.formatElapsedTime({
    direction,
    elapsedMs,
    invalidLabel: config.invalidLabel,
    justNowThresholdSeconds: config.justNowThresholdSeconds,
    locale: config.locale,
    nowMs,
    pendingLabel: config.pendingLabel,
    targetMs,
  });
}

export function useElapsedTimeNow(pollIntervalMs?: number): number {
  const store = getClockStore(normalizePollIntervalMs(pollIntervalMs));
  return React.useSyncExternalStore(store.subscribe, store.getSnapshot, store.getServerSnapshot);
}

export function defaultElapsedTimeFormatter({
  direction,
  elapsedMs,
  invalidLabel,
  justNowThresholdSeconds,
  nowMs,
  pendingLabel,
  targetMs,
}: ElapsedTimeFormatterInput): string {
  if (!Number.isFinite(targetMs)) {
    return invalidLabel;
  }
  if (nowMs <= 0) {
    return pendingLabel;
  }

  const elapsedSeconds = Math.floor(elapsedMs / 1_000);
  if (elapsedSeconds < Math.max(0, justNowThresholdSeconds)) {
    return "Just now";
  }
  if (elapsedSeconds < 60) {
    return direction === "past" ? "Less than a minute ago" : "Less than a minute from now";
  }

  const { unit, value } = elapsedTimeUnit(elapsedSeconds);
  return formatElapsedUnit(value, unit, direction);
}

type ClockStore = {
  readonly getServerSnapshot: () => number;
  readonly getSnapshot: () => number;
  readonly subscribe: (callback: () => void) => () => void;
};

const clockStores = new Map<number, ClockStore>();
function resolveConfig(
  context: ElapsedTimeConfig,
  options: UseElapsedTimeOptions,
): ElapsedTimeConfig {
  return {
    formatElapsedTime: options.formatElapsedTime ?? context.formatElapsedTime,
    invalidLabel: options.invalidLabel ?? context.invalidLabel,
    justNowThresholdSeconds: options.justNowThresholdSeconds ?? context.justNowThresholdSeconds,
    locale: options.locale ?? context.locale,
    pendingLabel: options.pendingLabel ?? context.pendingLabel,
    pollIntervalMs: options.pollIntervalMs ?? context.pollIntervalMs,
  };
}

function getClockStore(pollIntervalMs: number): ClockStore {
  const cached = clockStores.get(pollIntervalMs);
  if (cached) {
    return cached;
  }

  const store = createClockStore(pollIntervalMs);
  clockStores.set(pollIntervalMs, store);
  return store;
}

function createClockStore(pollIntervalMs: number): ClockStore {
  let nowMs = typeof window === "undefined" ? 0 : Date.now();
  let intervalID: number | undefined;
  const subscribers = new Set<() => void>();

  const tick = () => {
    const next = Date.now();
    if (next === nowMs) {
      return;
    }
    nowMs = next;
    for (const subscriber of subscribers) {
      subscriber();
    }
  };

  return {
    getServerSnapshot: () => 0,
    getSnapshot: () => nowMs,
    subscribe: (callback) => {
      if (typeof window === "undefined") {
        return () => {};
      }

      subscribers.add(callback);
      if (intervalID === undefined) {
        intervalID = window.setInterval(tick, pollIntervalMs);
      }
      nowMs = Date.now();
      window.queueMicrotask(callback);

      return () => {
        subscribers.delete(callback);
        if (subscribers.size === 0 && intervalID !== undefined) {
          window.clearInterval(intervalID);
          intervalID = undefined;
          clockStores.delete(pollIntervalMs);
        }
      };
    },
  };
}

function parseElapsedTimeInput(value: ElapsedTimeInput): number {
  if (value instanceof Date) {
    return value.getTime();
  }
  if (typeof value === "number") {
    return value;
  }
  return Date.parse(value);
}

function elapsedTimeUnit(seconds: number): {
  readonly unit: "day" | "hour" | "minute" | "month" | "year";
  readonly value: number;
} {
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return { unit: "minute", value: minutes };
  }

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return { unit: "hour", value: hours };
  }

  const days = Math.floor(hours / 24);
  if (days < 30) {
    return { unit: "day", value: days };
  }

  const months = Math.floor(days / 30);
  if (months < 12) {
    return { unit: "month", value: months };
  }

  return { unit: "year", value: Math.floor(months / 12) };
}

function formatElapsedUnit(
  value: number,
  unit: "day" | "hour" | "minute" | "month" | "year",
  direction: "future" | "past",
): string {
  const phrase = `${value} ${pluralize(value, unit)}`;
  return direction === "past" ? `${phrase} ago` : `in ${phrase}`;
}

function normalizePollIntervalMs(value: number | undefined): number {
  if (value === undefined || !Number.isFinite(value) || value <= 0) {
    return defaultConfig.pollIntervalMs;
  }
  return Math.max(100, Math.floor(value));
}
