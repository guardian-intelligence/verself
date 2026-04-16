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
          path: ~/go/pkg/mod
          key: go-mod-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('**/go.sum') }}
          restore-keys: |
            go-mod-\${{ runner.os }}-\${{ runner.arch }}-
          size: 20GiB
          policy: pull-push
          save: on-success

      - uses: forge-metal/mount@v1
        with:
          path: ~/.cache/go-build
          key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
          restore-keys: |
            go-build-\${{ runner.os }}-\${{ runner.arch }}-
          size: 10GiB
          policy: pull

      - run: go test ./...`;

const PARALLEL_CACHE_GO_CI = `jobs:
  warm-go-cache:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: forge-metal/checkout@v1
      - uses: forge-metal/mount@v1
        with:
          path: ~/.cache/go-build
          key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
          policy: push
          save: on-success
      - run: go test ./...

  test:
    needs: warm-go-cache
    strategy:
      matrix:
        package: [apiwire, billing-service, sandbox-rental-service, vm-orchestrator]
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - uses: forge-metal/checkout@v1
      - uses: forge-metal/mount@v1
        with:
          path: ~/.cache/go-build
          key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
          policy: pull
      - run: go test ./src/\${{ matrix.package }}/...`;

const FORGE_METAL_CONFIG = `# forge-metal.yml
version: 1

defaults:
  runner: metal-4vcpu-ubuntu-2404

caches:
  go-mod:
    path: ~/go/pkg/mod
    key: go-mod-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('**/go.sum') }}
    restore-keys:
      - go-mod-\${{ runner.os }}-\${{ runner.arch }}-
    size: 20GiB
    policy: pull-push
    save: on-success

  go-build:
    path: ~/.cache/go-build
    key: go-build-\${{ runner.os }}-\${{ runner.arch }}-\${{ hashFiles('go.work', '**/go.sum') }}
    restore-keys:
      - go-build-\${{ runner.os }}-\${{ runner.arch }}-
    size: 10GiB
    policy: pull`;

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
                  <SectionTitle>Planned API Layers</SectionTitle>
                  <SectionDescription>
                    The runner label is live. The acceleration actions are the public API direction:
                    explicit, composable, and compatible with standard GitHub Actions workflows.
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
                  description="Restores a ZFS-backed cache generation at a path and optionally saves a new generation."
                />
                <ApiLayer
                  name="Setup"
                  syntax="uses: forge-metal/setup-go@v1"
                  description="Selects a toolchain and applies language-specific checkout and mount defaults."
                />
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Explicit Persistent State</SectionTitle>
                  <SectionDescription>
                    Mounts use the cache vocabulary CI users already know: path, key, restore keys,
                    pull/push policy, and save-on-success behavior.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
                <CodeBlock code={EXPLICIT_CACHE_GO_CI} />
                <Aside title="Cache semantics">
                  The job gets a private writable clone. Forge Metal saves a new immutable generation
                  only when policy, trust level, and the job result all allow it.
                </Aside>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Parallel Jobs</SectionTitle>
                  <SectionDescription>
                    Use one writer and many readers when a large matrix shares the same cache. This
                    follows the same pull, push, and pull-push model used by mature CI systems.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <CodeBlock code={PARALLEL_CACHE_GO_CI} />
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
                    Trust scope is platform policy, not a workflow knob. A pull request can use warm
                    state without gaining write access to protected branch cache generations.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-3 md:grid-cols-3">
                <Policy title="Protected branches">
                  Protected branch jobs can restore and write protected cache generations.
                </Policy>
                <Policy title="Pull requests">
                  Pull requests can restore trusted cache generations but write isolated pull-request
                  generations by default.
                </Policy>
                <Policy title="Forks">
                  Forked runs cannot update repo, branch, or protected caches from workflow YAML.
                </Policy>
              </div>
            </PageSection>

            <PageSection>
              <SectionHeader>
                <SectionHeaderContent>
                  <SectionTitle>Forge Metal YAML</SectionTitle>
                  <SectionDescription>
                    The first repository config file should remove repetition, not replace GitHub
                    Actions. Workflows can reference named defaults while the YAML keeps cache policy
                    reviewable in the repository.
                  </SectionDescription>
                </SectionHeaderContent>
              </SectionHeader>

              <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
                <CodeBlock code={FORGE_METAL_CONFIG} />
                <Aside title="Config direction">
                  This file is optional. The GitHub workflow remains authoritative for jobs and
                  steps; Forge Metal YAML centralizes runner and cache defaults.
                </Aside>
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
