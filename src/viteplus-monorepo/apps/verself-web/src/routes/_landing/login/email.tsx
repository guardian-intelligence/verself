import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_landing/login/email")({
  component: LoginEmailSheetStateRoute,
});

function LoginEmailSheetStateRoute() {
  return null;
}
