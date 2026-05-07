import { Projects } from "./projects";

export const DEFAULT_SERVER_URL = "https://verself.sh";
export const DEFAULT_PROJECTS_URL = "https://projects.api.verself.sh";

export type VerselfOptions = {
  bearerToken: string;
  serverURL?: string | undefined;
  projectsURL?: string | undefined;
  fetch?: typeof fetch | undefined;
  traceparent?: string | undefined;
};

export class Verself {
  readonly projects: Projects;

  constructor(options: VerselfOptions) {
    const token = options.bearerToken.trim();
    if (token === "") {
      throw new Error("Verself SDK requires bearerToken");
    }
    this.projects = new Projects({
      accessToken: token,
      baseUrl: resolveProjectsURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
  }
}

function resolveProjectsURL(options: VerselfOptions): string {
  const value = options.projectsURL ?? options.serverURL ?? DEFAULT_PROJECTS_URL;
  const url = new URL(value);
  return url.toString().replace(/\/$/, "");
}

export {
  Projects,
  ProjectsApiError,
  createProjectRequestSchema,
  isProjectsApiError,
  type CreateProjectRequest,
  type ListProjectsInput,
  type Project,
  type ProjectList,
  type ProjectsClientOptions,
} from "./projects";
