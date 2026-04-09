import { redirect } from "@tanstack/react-router";
import { getViewer } from "~/server-fns/auth";

export async function requireViewer(locationHref: string) {
  const viewer = await getViewer();
  if (!viewer) {
    throw redirect({
      to: "/login",
      search: { redirect: locationHref },
    });
  }
  return viewer;
}
