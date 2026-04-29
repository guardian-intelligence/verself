import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey } from "@verself/auth-web/isomorphic";
import { useSignedInAuth } from "@verself/auth-web/react";
import { createProject, type CreateProjectRequest, type Project } from "~/server-fns/api";

export function useCreateProjectMutation({
  onSuccess,
}: {
  onSuccess?: (project: Project) => void | Promise<void>;
} = {}) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<Project, Error, CreateProjectRequest>({
    mutationFn: (data) => createProject({ data }),
    onSuccess: async (project) => {
      await queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "projects") });
      await onSuccess?.(project);
    },
  });
}
