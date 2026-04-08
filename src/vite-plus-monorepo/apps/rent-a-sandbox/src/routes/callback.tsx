import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { handleCallback } from "~/lib/auth";
import { keys } from "~/lib/query-keys";

export const Route = createFileRoute("/callback")({
  component: CallbackPage,
});

function CallbackPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { error } = useQuery({
    queryKey: ["auth", "callback"],
    queryFn: async () => {
      const user = await handleCallback();
      queryClient.setQueryData(keys.user(), user);
      void navigate({ to: "/", search: { purchased: false, subscribed: false }, replace: true });
      return user;
    },
    retry: false,
    staleTime: Infinity,
  });

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <h1 className="text-2xl font-bold text-destructive">Authentication failed</h1>
        <p className="text-muted-foreground">{error.message}</p>
      </div>
    );
  }

  return (
    <div className="flex items-center justify-center py-24">
      <p className="text-muted-foreground">Completing sign in...</p>
    </div>
  );
}
