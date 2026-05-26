import { createFileRoute } from "@tanstack/react-router";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@verself/ui/components/ui/page";
import {
  GITHUB_INTEGRATION_PROBLEM_GROUPS,
  githubIntegrationProblemAnchor,
  githubIntegrationProblemPath,
  type GithubIntegrationProblem,
} from "~/lib/github-integration-problem-catalog";

export const Route = createFileRoute("/_workshop/docs/reference/github-integration/errors")({
  component: GithubIntegrationErrors,
  head: () => ({
    meta: [{ title: "GitHub Integration Errors - Verself API Reference" }],
  }),
});

function GithubIntegrationErrors() {
  return (
    <Page variant="full">
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>GitHub Integration Errors</PageTitle>
          <PageDescription>
            Stable problem codes emitted by github-integration-service while accepting GitHub
            webhooks, creating runner capacity, submitting sandbox jobs, and surfacing failures back
            to GitHub.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection id="shape">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Problem Shape</SectionTitle>
              <SectionDescription>
                Error responses and durable lifecycle resources use RFC 9457 problem fields with
                plural problem occurrences.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-4 text-xs leading-5">
            {`{
  "type": "urn:verself:problem:github-runner:jit_config_failed",
  "title": "GitHub runner JIT config failed",
  "status": 0,
  "detail": "Verself could not create a GitHub JIT runner configuration. See https://verself.sh/docs/reference/github-integration/errors#github-runner-jit-config-failed.",
  "code": "github_runner.jit_config_failed",
  "docs_url": "https://verself.sh/docs/reference/github-integration/errors#github-runner-jit-config-failed",
  "errors": [
    {
      "type": "urn:verself:problem:github-runner:jit_config_failed",
      "code": "github_runner.jit_config_failed",
      "title": "GitHub runner JIT config failed",
      "phase": "jit_config",
      "retryable": false,
      "docs_url": "https://verself.sh/docs/reference/github-integration/errors#github-runner-jit-config-failed"
    }
  ]
}`}
          </pre>
        </PageSection>

        {GITHUB_INTEGRATION_PROBLEM_GROUPS.map((group) => (
          <PageSection id={group.id} key={group.id}>
            <SectionHeader>
              <SectionHeaderContent>
                <SectionTitle>{group.label}</SectionTitle>
                <SectionDescription>{group.description}</SectionDescription>
              </SectionHeaderContent>
            </SectionHeader>
            <ProblemTable problems={group.problems} />
          </PageSection>
        ))}
      </PageSections>
    </Page>
  );
}

function ProblemTable({ problems }: { problems: readonly GithubIntegrationProblem[] }) {
  return (
    <div className="overflow-x-auto border-y border-border">
      <table className="min-w-[960px] border-collapse text-left text-sm">
        <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
          <tr>
            <th className="w-[260px] px-3 py-2 font-medium">Code</th>
            <th className="w-[180px] px-3 py-2 font-medium">Phase</th>
            <th className="w-[96px] px-3 py-2 font-medium">Status</th>
            <th className="w-[96px] px-3 py-2 font-medium">Retry</th>
            <th className="px-3 py-2 font-medium">Message</th>
          </tr>
        </thead>
        <tbody>
          {problems.map((problem) => (
            <tr
              className="border-t border-border align-top"
              id={githubIntegrationProblemAnchor(problem.code)}
              key={problem.code}
            >
              <td className="px-3 py-3 font-mono text-xs">
                <a
                  className="underline underline-offset-4"
                  href={githubIntegrationProblemPath(problem.code)}
                >
                  {problem.code}
                </a>
              </td>
              <td className="px-3 py-3 font-mono text-xs">{problem.phase}</td>
              <td className="px-3 py-3 font-mono text-xs">{problem.status ?? "n/a"}</td>
              <td className="px-3 py-3">{problem.retryable ? "yes" : "no"}</td>
              <td className="px-3 py-3">
                <div className="flex flex-col gap-1">
                  <span className="font-medium">{problem.title}</span>
                  <span className="text-muted-foreground">{problem.message}</span>
                  <span className="break-all font-mono text-xs text-muted-foreground">
                    {`https://verself.sh${githubIntegrationProblemPath(problem.code)}`}
                  </span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
