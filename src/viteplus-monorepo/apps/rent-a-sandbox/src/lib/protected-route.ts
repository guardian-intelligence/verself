import { redirect } from "@tanstack/react-router";
import { getViewer } from "~/server-fns/auth";

export function loginRedirect(locationHref: string) {
  return redirect({
    to: "/login",
    search: { redirect: locationHref },
  });
}

export async function requireViewer(locationHref: string) {
  const viewer = await getViewer();
  if (!viewer) {
    throw loginRedirect(locationHref);
  }
  return viewer;
}
