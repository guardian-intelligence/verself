"use client";

import * as React from "react";
import {
  type ElapsedTimeInput,
  type UseElapsedTimeOptions,
  useElapsedTime,
} from "../hooks/use-elapsed-time";

export type ElapsedTimeProps = Omit<
  React.ComponentPropsWithoutRef<"time">,
  "children" | "dateTime"
> & {
  readonly dateTime?: string;
  readonly options?: UseElapsedTimeOptions;
  readonly value: ElapsedTimeInput;
};

export function ElapsedTime({
  dateTime,
  options,
  suppressHydrationWarning = true,
  title,
  value,
  ...props
}: ElapsedTimeProps) {
  const label = useElapsedTime(value, options);
  const dateTimeValue = dateTime ?? formatDateTimeAttribute(value);

  return (
    <time
      {...props}
      dateTime={dateTimeValue}
      suppressHydrationWarning={suppressHydrationWarning}
      title={title ?? dateTimeValue}
    >
      {label}
    </time>
  );
}

function formatDateTimeAttribute(value: ElapsedTimeInput): string {
  const date = value instanceof Date ? value : new Date(value);
  return Number.isFinite(date.getTime()) ? date.toISOString() : "";
}
