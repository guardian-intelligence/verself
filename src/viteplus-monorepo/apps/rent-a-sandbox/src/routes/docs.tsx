import { createFileRoute, Link } from "@tanstack/react-router";
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

export const Route = createFileRoute("/docs")({
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
      - uses: actions/checkout@v6

      - name: Run Go tests
        run: go test ./...`;

const EXPLICIT_CACHE_GO_CI = `jobs:
  unit:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: forge-metal/checkout@v1

      - uses: forge-metal/mount@v1
        with:
          key: go-mod-\${{ hashFiles('**/go.sum') }}
          path: ~/go/pkg/mod
          size: 20GiB
          scope: repo
          save: on-success

      - uses: forge-metal/mount@v1
        with:
          key: go-build-\${{ github.ref_name }}
          path: ~/.cache/go-build
          size: 10GiB
          scope: branch
          save: on-success

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
    <main className="min-h-screen bg-background">
      <div className="border-b border-border">
        <div className="mx-auto flex w-full max-w-6xl items-center justify-between px-4 py-4 md:px-8">
          <Link to="/docs" className="flex min-w-0 items-center gap-2 text-sm font-semibold">
            <span aria-hidden="true" className="text-base">
              ◼
            </span>
            <span className="truncate">Forge Metal Docs</span>
          </Link>
          <Link
            to="/login"
            search={{ redirect: undefined }}
            className="text-sm font-medium text-muted-foreground hover:text-foreground"
          >
            Sign in
          </Link>
        </div>
      </div>

      <div className="mx-auto w-full max-w-6xl px-4 py-10 md:px-8 md:py-12">
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
                  <SectionTitle>Public API Layers</SectionTitle>
                  <SectionDescription>
                    Each action owns one concern. Compose them per job instead of adopting a Forge
                    Metal build system.
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
                  description="Materializes source through an incrementally updated Git object store."
                />
                <ApiLayer
                  name="Mount"
                  syntax="uses: forge-metal/mount@v1"
                  description="Attaches a named persistent ZFS-backed disk at a path."
                />
                <ApiLayer
                  name="Setup"
                  syntax="uses: forge-metal/setup-go@v1"
                  description="Installs or selects a toolchain and mounts its high-value caches."
                />
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Explicit Persistent State</SectionTitle>
                  <SectionDescription>
                    Persistent mounts are the primitive. Tool-specific setup actions are convenience
                    wrappers over the same mount semantics.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
                <CodeBlock code={EXPLICIT_CACHE_GO_CI} />
                <Aside title="Mount semantics">
                  A mount restores the latest committed generation for its key, writes into the
                  job's private VM filesystem, and commits a new generation only when the configured
                  save policy allows it.
                </Aside>
              </div>
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
                    Cache scope is part of the API because forked pull requests and protected
                    branches cannot safely share writable state.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-3 md:grid-cols-3">
                <Policy title="Protected branches">
                  Protected branch jobs can write protected branch cache generations.
                </Policy>
                <Policy title="Pull requests">
                  Pull requests restore trusted cache generations but write isolated pull-request
                  generations by default.
                </Policy>
                <Policy title="Forks">
                  Forked runs do not write repo or branch caches unless an organization policy
                  explicitly allows it.
                </Policy>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>What Comes Later</SectionTitle>
                  <SectionDescription>
                    Workspace images are powerful, but they should sit above checkout and mounts
                    instead of replacing them.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="rounded-lg border border-border bg-secondary/40 p-4">
                <p className="text-sm leading-6 text-foreground">
                  A future workspace action can run declared preparation commands, persist selected
                  paths, and restore the resulting filesystem image for later jobs. That layer
                  should remain opt-in because normal workflows need transparent checkout, cache,
                  and command semantics first.
                </p>
              </div>
            </PageSection>
          </PageSections>
        </Page>
      </div>
    </main>
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
