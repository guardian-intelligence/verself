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
} from "@forge-metal/ui/components/ui/page";

export const Route = createFileRoute("/_shell/docs")({
  component: DocsPage,
  head: () => ({
    meta: [
      {
        title: "Forge Metal CI Docs",
      },
    ],
  }),
});

const CURRENT_GO_CI = `name: Forge Metal Go CI

on:
  pull_request:
  workflow_dispatch:

jobs:
  unit:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: actions/checkout@v5

      - name: Run Go tests
        run: go test ./...`;

const EXPLICIT_CACHE_GO_CI = `jobs:
  unit:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: actions/checkout@v5

      - uses: forge-metal/stickydisk@v1
        with:
          key: go-mod-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('**/go.sum') }}
          path: ~/go/pkg/mod

      - uses: forge-metal/stickydisk@v1
        with:
          key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
          path: ~/.cache/go-build

      - run: go test ./...`;

const TRANSPARENT_CACHE_GO_CI = `jobs:
  unit:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: actions/checkout@v5

      - uses: actions/cache@v4
        with:
          path: ~/.cache/go-build
          key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
          restore-keys: |
            go-build-\${{ runner.os }}-\${{ runner.arch }}-

      - run: go test ./...`;

const POLYGLOT_CI = `jobs:
  test:
    runs-on: metal-8vcpu-ubuntu-2404
    steps:
      - uses: forge-metal/checkout@v1

      - uses: forge-metal/setup-go@v1
        with:
          go-version-file: go.work

      - uses: forge-metal/setup-node@v1
        with:
          node-version-file: .nvmrc
          package-manager: pnpm

      - uses: forge-metal/setup-postgres@v1
        with:
          version: "16"
          seed: ./testdata/schema.sql

      - run: go test ./...
      - run: pnpm install --frozen-lockfile
      - run: pnpm test
      - run: make integration-test`;

function DocsPage() {
  return (
    <Page variant="full">
          <PageHeader>
            <PageHeaderContent className="max-w-3xl">
              <PageTitle>GitHub Actions on Forge Metal</PageTitle>
              <PageDescription>
                Run normal GitHub Actions jobs on isolated Firecracker VMs, then opt into ZFS-backed
                checkout and persistent mounts where they make the job faster.
              </PageDescription>
            </PageHeaderContent>
          </PageHeader>

          <PageSections>
            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Start With A Normal Runner</SectionTitle>
                  <SectionDescription>
                    The first public contract is the runner label. A job that runs on Forge Metal
                    should still look like a standard GitHub Actions job.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
                <CodeBlock code={CURRENT_GO_CI} />
                <Aside title="Current baseline">
                  This is the shape we use for our own first CI canary. It keeps checkout explicit,
                  uses GitHub's standard job model, and gives us end-to-end proof before adding
                  acceleration APIs.
                </Aside>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>API Layers</SectionTitle>
                  <SectionDescription>
                    The runner label is live. Sticky disks are the first explicit acceleration
                    action; transparent cache and checkout acceleration follow the same GitHub
                    Actions-native model.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-3 md:grid-cols-2">
                <ApiLayer
                  name="Runner"
                  syntax="runs-on: metal-4vcpu-ubuntu-2404"
                  description="Selects an isolated VM resource shape and Ubuntu image."
                />
                <ApiLayer
                  name="Checkout"
                  syntax="uses: forge-metal/checkout@v1"
                  description="Planned drop-in replacement for actions/checkout using a local Git mirror."
                />
                <ApiLayer
                  name="Sticky Disk"
                  syntax="uses: forge-metal/stickydisk@v1"
                  description="Restores a named persistent disk at a path and commits it after the job."
                />
                <ApiLayer
                  name="Transparent Cache"
                  syntax="uses: actions/cache@v4"
                  description="Planned acceleration for the standard GitHub cache protocol."
                />
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Sticky Disks</SectionTitle>
                  <SectionDescription>
                    The public contract is intentionally small: key and path. Forge Metal owns the
                    restore, commit, scoping, eviction, observability, and billing behavior.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
                <CodeBlock code={EXPLICIT_CACHE_GO_CI} />
                <Aside title="Current tracer bullet">
                  The first implementation restores an archive into the path and commits it during
                  post-job cleanup. The API is the durable part; ZFS-backed mounts replace the
                  archive path after the end-to-end flow is proven.
                </Aside>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Standard Cache</SectionTitle>
                  <SectionDescription>
                    The next compatibility layer is accelerating the normal GitHub cache action
                    without asking customers to adopt a Forge Metal config file.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <CodeBlock code={TRANSPARENT_CACHE_GO_CI} />
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Polyglot Repositories</SectionTitle>
                  <SectionDescription>
                    Repositories can compose multiple setup actions in one job. The workflow remains
                    GitHub Actions; Forge Metal supplies faster substrate primitives.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <CodeBlock code={POLYGLOT_CI} />
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Security Defaults</SectionTitle>
                  <SectionDescription>
                    Trust scope is platform policy, not a workflow knob. Pull requests can use warm
                    state without gaining write access to protected branch sticky disks or caches.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-3 md:grid-cols-3">
                <Policy title="Protected branches">
                  Protected branch jobs can restore and write protected sticky disks.
                </Policy>
                <Policy title="Pull requests">
                  Pull requests can restore trusted state but write isolated pull-request state by
                  default.
                </Policy>
                <Policy title="Forks">
                  Forked runs cannot update repo, branch, or protected state from workflow YAML.
                </Policy>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>What Comes Later</SectionTitle>
                  <SectionDescription>
                    Workspace images are powerful, but they should sit above checkout, cache, and
                    sticky disks instead of replacing them.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="rounded-lg border border-border bg-secondary/40 p-4">
                <p className="text-sm leading-6 text-foreground">
                  A future workspace action can run declared preparation commands, persist selected
                  paths, and restore the resulting filesystem image for later jobs. That layer
                  should remain opt-in because normal workflows need transparent checkout, cache,
                  sticky-disk, and command semantics first.
                </p>
              </div>
            </PageSection>
          </PageSections>
    </Page>
  );
}

function CodeBlock({ code }: { code: string }) {
  return (
    <pre className="overflow-x-auto rounded-lg border border-border bg-foreground p-4 text-sm leading-6 text-background">
      <code>{code}</code>
    </pre>
  );
}

function Aside({ title, children }: { title: string; children: string }) {
  return (
    <aside className="rounded-lg border border-border bg-secondary/40 p-4">
      <h3 className="text-sm font-semibold leading-5">{title}</h3>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{children}</p>
    </aside>
  );
}

function ApiLayer({
  name,
  syntax,
  description,
}: {
  name: string;
  syntax: string;
  description: string;
}) {
  return (
    <div className="rounded-lg border border-border p-4">
      <h3 className="text-sm font-semibold leading-5">{name}</h3>
      <code className="mt-3 block overflow-x-auto rounded-md bg-secondary px-3 py-2 text-xs leading-5">
        {syntax}
      </code>
      <p className="mt-3 text-sm leading-6 text-muted-foreground">{description}</p>
    </div>
  );
}

function Policy({ title, children }: { title: string; children: string }) {
  return (
    <div className="rounded-lg border border-border p-4">
      <h3 className="text-sm font-semibold leading-5">{title}</h3>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{children}</p>
    </div>
  );
}
