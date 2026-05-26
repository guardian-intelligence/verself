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

const githubRunnerToolchainDir = "/opt/verself/toolchains/github-actions-runner";

export const Route = createFileRoute("/_workshop/docs/reference/hosted-runners/runtime")({
  component: HostedRunnerRuntime,
  head: () => ({
    meta: [{ title: "Hosted Runner Runtime - Verself API Reference" }],
  }),
});

function HostedRunnerRuntime() {
  return (
    <Page variant="full">
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Hosted Runner Runtime</PageTitle>
          <PageDescription>
            Guest filesystem paths and runtime contracts for Verself-hosted Linux CI runners.
          </PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <PageSections>
        <PageSection id="toolchain-mounts">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Toolchain Mounts</SectionTitle>
              <SectionDescription>
                Verself mounts platform-owned runner toolchains into customer VMs.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <div className="space-y-3 text-sm leading-6">
            <p>
              GitHub Actions jobs currently receive a read-only runner toolchain at{" "}
              <code>{githubRunnerToolchainDir}</code>. The directory contains the upstream runner
              distribution and platform-managed runtime helpers.
            </p>
            <p>
              Customer jobs should treat <code>/opt/verself/toolchains</code> as Verself-owned
              runtime material. Do not store durable job output, caches, credentials, or repository
              state in that tree.
            </p>
          </div>
        </PageSection>

        <PageSection id="writable-state">
          <SectionHeader>
            <SectionHeaderContent>
              <SectionTitle>Writable State</SectionTitle>
              <SectionDescription>
                Customer writes belong in workspace, home, or declared cache paths.
              </SectionDescription>
            </SectionHeaderContent>
          </SectionHeader>
          <div className="space-y-3 text-sm leading-6">
            <p>
              The repository workspace is mounted through the GitHub runner <code>_work</code> tree
              and backed by Verself durable storage. Cache manifests may request additional mounted
              directories for package-manager and build-system state.
            </p>
            <p>
              Runner diagnostics and scratch directories under the toolchain are ephemeral tmpfs
              overlays. They are not durable customer data and are not restored as cache
              generations.
            </p>
          </div>
        </PageSection>
      </PageSections>
    </Page>
  );
}
