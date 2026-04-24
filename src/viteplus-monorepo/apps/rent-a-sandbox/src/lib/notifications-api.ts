import * as v from "valibot";
import { createClient, type Client } from "../__generated/notifications-api/client/index.js";
import {
  advanceNotificationReadCursor as advanceGeneratedNotificationReadCursor,
  dismissNotification as dismissGeneratedNotification,
  getNotificationSummary as getGeneratedNotificationSummary,
  listNotifications as listGeneratedNotifications,
  publishTestNotification as publishGeneratedTestNotification,
  putNotificationPreferences as putGeneratedNotificationPreferences,
} from "../__generated/notifications-api/index.js";
import type {
  NotificationMarkReadRequestWritable,
  NotificationPutPreferencesRequestWritable,
  NotificationTestRequestWritable,
} from "../__generated/notifications-api/types.gen.js";
import {
  vNotificationAccepted,
  vNotificationList,
  vNotificationMarkReadRequestWritable,
  vNotificationPutPreferencesRequestWritable,
  vNotificationSummary,
  vNotificationTestRequestWritable,
} from "../__generated/notifications-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export interface NotificationsClientOptions extends BearerClientOptions {}

export class NotificationsApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Notifications API", status, path, body);
    this.name = "NotificationsApiError";
  }
}

export function isNotificationsApiError(error: unknown): error is NotificationsApiError {
  return error instanceof NotificationsApiError;
}

function throwNotificationsError(
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  throwGeneratedServiceError(NotificationsApiError, path, response, error);
}

function createNotificationsClient(options: NotificationsClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

export const notificationsListQuerySchema = v.strictObject({
  limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(100))),
});

export const putNotificationPreferencesRequestSchema = v.strictObject({
  enabled: v.boolean(),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export const markNotificationReadRequestSchema = v.strictObject({
  read_up_to_sequence: v.pipe(v.string(), v.regex(/^[0-9]+$/)),
});

export const dismissNotificationRequestSchema = v.strictObject({
  notification_id: v.pipe(v.string(), v.uuid()),
});

export const publishTestNotificationRequestSchema = v.strictObject({
  action_url: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(500))),
  body: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(500))),
  title: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(120))),
});

export type NotificationsListQuery = v.InferInput<typeof notificationsListQuerySchema>;
export type PutNotificationPreferencesRequest = v.InferInput<
  typeof putNotificationPreferencesRequestSchema
>;
export type MarkNotificationReadRequest = v.InferInput<typeof markNotificationReadRequestSchema>;
export type DismissNotificationRequest = v.InferInput<typeof dismissNotificationRequestSchema>;
export type PublishTestNotificationRequest = v.InferInput<
  typeof publishTestNotificationRequestSchema
>;

function parseNotificationList(input: unknown) {
  const { $schema: _schema, ...list } = v.parse(vNotificationList, input);
  return {
    ...list,
    notifications: list.notifications ?? [],
  };
}

function parseNotificationSummary(input: unknown) {
  const { $schema: _schema, ...summary } = v.parse(vNotificationSummary, input);
  return summary;
}

function parseNotificationAccepted(input: unknown) {
  const { $schema: _schema, ...accepted } = v.parse(vNotificationAccepted, input);
  return accepted;
}

export type NotificationList = ReturnType<typeof parseNotificationList>;
export type NotificationSummary = ReturnType<typeof parseNotificationSummary>;
export type NotificationAccepted = ReturnType<typeof parseNotificationAccepted>;
export type Notification = NotificationList["notifications"][number];

export async function listNotifications(
  options: NotificationsClientOptions & { query?: NotificationsListQuery },
): Promise<NotificationList> {
  const client = createNotificationsClient(options);
  const parsedQuery = v.parse(notificationsListQuerySchema, options.query ?? {});
  const query = parsedQuery.limit === undefined ? {} : { limit: parsedQuery.limit };
  const path = "/api/v1/notifications";
  const result = await listGeneratedNotifications({
    client,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationList(result.data);
}

export async function getNotificationSummary(
  options: NotificationsClientOptions,
): Promise<NotificationSummary> {
  const client = createNotificationsClient(options);
  const path = "/api/v1/notifications/summary";
  const result = await getGeneratedNotificationSummary({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationSummary(result.data);
}

export async function putNotificationPreferences(
  options: NotificationsClientOptions & { body: PutNotificationPreferencesRequest },
): Promise<NotificationSummary> {
  const client = createNotificationsClient(options);
  const body = v.parse(
    vNotificationPutPreferencesRequestWritable,
    v.parse(putNotificationPreferencesRequestSchema, options.body),
  ) as NotificationPutPreferencesRequestWritable;
  const path = "/api/v1/notifications/preferences";
  const result = await putGeneratedNotificationPreferences({
    body,
    client,
    headers: idempotencyHeaders("notification-preferences"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationSummary(result.data);
}

export async function markNotificationRead(
  options: NotificationsClientOptions & { body: MarkNotificationReadRequest },
): Promise<NotificationSummary> {
  const client = createNotificationsClient(options);
  const body = v.parse(
    vNotificationMarkReadRequestWritable,
    v.parse(markNotificationReadRequestSchema, options.body),
  ) as NotificationMarkReadRequestWritable;
  const path = "/api/v1/notifications/read-cursor";
  const result = await advanceGeneratedNotificationReadCursor({
    body,
    client,
    headers: idempotencyHeaders("notification-read-cursor"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationSummary(result.data);
}

export async function dismissNotification(
  options: NotificationsClientOptions & { body: DismissNotificationRequest },
): Promise<NotificationSummary> {
  const client = createNotificationsClient(options);
  const input = v.parse(dismissNotificationRequestSchema, options.body);
  const path = "/api/v1/notifications/{notification_id}/dismiss";
  const result = await dismissGeneratedNotification({
    client,
    headers: idempotencyHeaders("notification-dismiss"),
    path: { notification_id: input.notification_id },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationSummary(result.data);
}

export async function publishTestNotification(
  options: NotificationsClientOptions & { body: PublishTestNotificationRequest },
): Promise<NotificationAccepted> {
  const client = createNotificationsClient(options);
  const body = v.parse(
    vNotificationTestRequestWritable,
    v.parse(publishTestNotificationRequestSchema, options.body),
  ) as NotificationTestRequestWritable;
  const path = "/api/v1/notifications/test";
  const result = await publishGeneratedTestNotification({
    body,
    client,
    headers: idempotencyHeaders("notification-test"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwNotificationsError(path, result.response, result.error);
  }

  return parseNotificationAccepted(result.data);
}
