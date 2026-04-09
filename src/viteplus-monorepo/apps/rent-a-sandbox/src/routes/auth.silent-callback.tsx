import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { handleSilentCallback } from "~/lib/auth";

export const Route = createFileRoute("/auth/silent-callback")({
  component: SilentCallbackPage,
});

function SilentCallbackPage() {
  const { error } = useQuery({
    queryKey: ["auth", "silent-callback"],
    queryFn: async () => {
      await handleSilentCallback();
      return true;
    },
    retry: false,
    staleTime: Infinity,
  });

  if (error) {
    return <p className="sr-only">Silent authentication refresh failed.</p>;
  }

  return <p className="sr-only">Refreshing session.</p>;
}
