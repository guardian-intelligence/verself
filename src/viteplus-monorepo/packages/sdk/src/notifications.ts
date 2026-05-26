import * as v from "valibot";
import { createClient, type Client } from "./__generated/notifications-api/client/index.js";
import {
  advanceNotificationReadCursor as advanceGeneratedNotificationReadCursor,
  clearNotifications as clearGeneratedNotifications,
  dismissNotification as dismissGeneratedNotification,
  getNotificationSummary as getGeneratedNotificationSummary,
  listNotifications as listGeneratedNotifications,
  markNotificationRead as markGeneratedNotificationRead,
  publishTestNotification as publishGeneratedTestNotification,
  putNotificationPreferences as putGeneratedNotificationPreferences,
} from "./__generated/notifications-api/index.js";
import type {
  AdvanceNotificationReadCursorData,
  PublishTestNotificationData,
  PutNotificationPreferencesData,
} from "./__generated/notifications-api/types.gen.js";
import {
  vAdvanceNotificationReadCursorBody,
  vListNotificationsResponse,
  vPutNotificationPreferencesBody,
  vNotificationSummary,
  vPublishTestNotificationBody,
  vPublishTestNotificationResponse,
} from "./__generated/notifications-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type NotificationsClientOptions = BearerClientOptions;

export type NotificationsMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class NotificationsApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Notifications API", status, path, body);
    this.name = "NotificationsApiError";
  }
}

export function isNotificationsApiError(error: unknown): error is NotificationsApiError {
  return error instanceof NotificationsApiError;
}

function createNotificationsClient(options: NotificationsClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwNotificationsError(
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  throwGeneratedServiceError(NotificationsApiError, path, response, error);
}

export const notificationsListQuerySchema = v.strictObject({
  limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(500))),
  cursor: v.optional(v.pipe(v.string(), v.maxLength(4096))),
});

export const putNotificationPreferencesRequestSchema = v.strictObject({
  enabled: v.boolean(),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
  web_enabled: v.optional(v.boolean()),
  email_enabled: v.optional(v.boolean()),
  push_enabled: v.optional(v.boolean()),
  sms_enabled: v.optional(v.boolean()),
});

export const markNotificationReadRequestSchema = v.strictObject({
  read_up_to_sequence: v.pipe(v.string(), v.regex(/^[0-9]+$/)),
});

export const dismissNotificationRequestSchema = v.strictObject({
  notification_id: v.pipe(v.string(), v.uuid()),
});

export const publishTestNotificationRequestSchema = v.strictObject({
  action_url: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(8192))),
  body: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(4096))),
  title: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(240))),
});

export type NotificationsListQuery = v.InferOutput<typeof notificationsListQuerySchema>;
export type PutNotificationPreferencesRequest = v.InferOutput<
  typeof putNotificationPreferencesRequestSchema
>;
export type MarkNotificationReadRequest = v.InferOutput<typeof markNotificationReadRequestSchema>;
export type DismissNotificationRequest = v.InferOutput<typeof dismissNotificationRequestSchema>;
export type PublishTestNotificationRequest = v.InferOutput<
  typeof publishTestNotificationRequestSchema
>;

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

function parseNotificationList(input: unknown) {
  const list = v.parse(vListNotificationsResponse, input);
  return {
    ...list,
    notifications: list.notifications ?? [],
  };
}

function parseNotificationSummary(input: unknown) {
  return v.parse(vNotificationSummary, input);
}

function parseNotificationAccepted(input: unknown) {
  return v.parse(vPublishTestNotificationResponse, input);
}

export type NotificationList = ReturnType<typeof parseNotificationList>;
export type NotificationSummary = ReturnType<typeof parseNotificationSummary>;
export type NotificationAccepted = ReturnType<typeof parseNotificationAccepted>;
export type Notification = NotificationList["notifications"][number];

export class Notifications {
  readonly #options: NotificationsClientOptions;

  constructor(options: NotificationsClientOptions) {
    this.#options = options;
  }

  async list(input: NotificationsListQuery = {}): Promise<NotificationList> {
    const client = createNotificationsClient(this.#options);
    const parsedQuery = removeUndefined(v.parse(notificationsListQuerySchema, input));
    const path = "/api/v1/notifications";
    const result = await listGeneratedNotifications({
      client,
      query: parsedQuery,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationList(result.data);
  }

  async summary(): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
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

  async putPreferences(
    body: PutNotificationPreferencesRequest,
    options: NotificationsMutationOptions = {},
  ): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(putNotificationPreferencesRequestSchema, body),
    ) as PutNotificationPreferencesData["body"];
    const path = "/api/v1/notifications/preferences";
    const result = await putGeneratedNotificationPreferences({
      body: v.parse(
        vPutNotificationPreferencesBody,
        parsedBody,
      ) as PutNotificationPreferencesData["body"],
      client,
      headers: idempotencyHeaders("notification-preferences", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationSummary(result.data);
  }

  async markRead(
    body: MarkNotificationReadRequest,
    options: NotificationsMutationOptions = {},
  ): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
    const parsedBody = v.parse(
      vAdvanceNotificationReadCursorBody,
      v.parse(markNotificationReadRequestSchema, body),
    ) as AdvanceNotificationReadCursorData["body"];
    const path = "/api/v1/notifications/read-cursor";
    const result = await advanceGeneratedNotificationReadCursor({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("notification-read-cursor", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationSummary(result.data);
  }

  async markNotificationRead(
    notificationId: string,
    options: NotificationsMutationOptions = {},
  ): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
    const input = v.parse(dismissNotificationRequestSchema, { notification_id: notificationId });
    const path = `/api/v1/notifications/${input.notification_id}/read`;
    const result = await markGeneratedNotificationRead({
      client,
      headers: idempotencyHeaders("notification-read", options.idempotencyKey),
      path: { notification_id: input.notification_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationSummary(result.data);
  }

  async dismiss(
    notificationId: string,
    options: NotificationsMutationOptions = {},
  ): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
    const input = v.parse(dismissNotificationRequestSchema, { notification_id: notificationId });
    const path = `/api/v1/notifications/${input.notification_id}/dismiss`;
    const result = await dismissGeneratedNotification({
      client,
      headers: idempotencyHeaders("notification-dismiss", options.idempotencyKey),
      path: { notification_id: input.notification_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationSummary(result.data);
  }

  async clear(options: NotificationsMutationOptions = {}): Promise<NotificationSummary> {
    const client = createNotificationsClient(this.#options);
    const path = "/api/v1/notifications/clear";
    const result = await clearGeneratedNotifications({
      client,
      headers: idempotencyHeaders("notification-clear", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationSummary(result.data);
  }

  async publishTest(
    body: PublishTestNotificationRequest,
    options: NotificationsMutationOptions = {},
  ): Promise<NotificationAccepted> {
    const client = createNotificationsClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(publishTestNotificationRequestSchema, body),
    ) as NonNullable<PublishTestNotificationData["body"]>;
    const path = "/api/v1/notifications/test";
    const result = await publishGeneratedTestNotification({
      body: v.parse(vPublishTestNotificationBody, parsedBody) as NonNullable<
        PublishTestNotificationData["body"]
      >,
      client,
      headers: idempotencyHeaders("notification-test", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwNotificationsError(path, result.response, result.error);
    }
    return parseNotificationAccepted(result.data);
  }
}
