import { ClientOnly, createFileRoute } from "@tanstack/react-router";
import {
  NotificationsPage,
  NotificationsPageFallback,
} from "~/features/notifications/notification-bell";

export const Route = createFileRoute("/_shell/_authenticated/notifications")({
  component: NotificationsRoute,
});

function NotificationsRoute() {
  return (
    <ClientOnly fallback={<NotificationsPageFallback />}>
      <NotificationsPage />
    </ClientOnly>
  );
}
