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
  IAM_AUTH_PROBLEM_GROUPS,
  iamAuthProblemAnchor,
  iamAuthProblemPath,
  type IamAuthProblem,
} from "~/lib/iam-auth-problem-catalog";

export const Route = createFileRoute("/_workshop/docs/reference/iam/errors")({
  component: IamAuthErrors,
  head: () => ({
    meta: [{ title: "IAM Auth Errors - Verself API Reference" }],
  }),
});

function IamAuthErrors() {
  return (
    <Page variant="full">
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>IAM Auth Errors</PageTitle>
          <PageDescription>
            Stable problem codes for account resolution, device sessions, provider linking,
            organization context, and OAuth device-code login.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection id="shape">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Problem Shape</SectionTitle>
              <SectionDescription>
                Runtime errors use problem details with a stable code and a public type URL anchored
                to this page.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-4 text-xs leading-5">
            {`{
  "type": "https://verself.sh/docs/reference/iam/errors#auth-session-revoked",
  "title": "Unauthorized",
  "status": 401,
  "detail": "device session is expired or revoked",
  "code": "auth.session_revoked",
  "requestId": "req_01J8QK4M5N6P7Q8R9S0T1V2W3X",
  "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
}`}
          </pre>
        </PageSection>

        {IAM_AUTH_PROBLEM_GROUPS.map((group) => (
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

function ProblemTable({ problems }: { problems: readonly IamAuthProblem[] }) {
  return (
    <div className="overflow-x-auto border-y border-border">
      <table className="min-w-[960px] border-collapse text-left text-sm">
        <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
          <tr>
            <th className="w-[260px] px-3 py-2 font-medium">Code</th>
            <th className="w-[180px] px-3 py-2 font-medium">Phase</th>
            <th className="w-[96px] px-3 py-2 font-medium">Status</th>
            <th className="w-[96px] px-3 py-2 font-medium">Retry</th>
            <th className="px-3 py-2 font-medium">Meaning</th>
          </tr>
        </thead>
        <tbody>
          {problems.map((problem) => (
            <tr
              className="border-t border-border align-top"
              id={iamAuthProblemAnchor(problem.code)}
              key={problem.code}
            >
              <td className="px-3 py-3 font-mono text-xs">
                <a className="underline underline-offset-4" href={iamAuthProblemPath(problem.code)}>
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
                    {`https://verself.sh${iamAuthProblemPath(problem.code)}`}
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
