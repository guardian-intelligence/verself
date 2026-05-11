import { appendFileSync, existsSync, mkdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { performance } from "node:perf_hooks";
import { randomUUID } from "node:crypto";
import { defineConfig, type Plugin } from "vite-plus";

type BuildTelemetryAttribute = string | number | boolean;

type BuildTelemetryEvent = {
  name: string;
  attributes: Record<string, BuildTelemetryAttribute>;
};

type BundleItem = {
  code?: string;
  source?: string | Uint8Array;
};

export default defineConfig({
  plugins: [verselfBuildTelemetryPlugin()],
  fmt: {
    ignorePatterns: ["**/__generated/**", "**/routeTree.gen.ts"],
  },
  lint: {
    ignorePatterns: [
      "**/dist/**",
      "**/.output/**",
      "**/node_modules/**",
      "**/__generated/**",
      "**/routeTree.gen.ts",
    ],
    options: { typeAware: true, typeCheck: true },
    rules: {
      "no-console": ["error", { allow: ["error"] }],
    },
    overrides: [
      {
        files: ["packages/nitro-plugins/**", "scripts/**"],
        rules: {
          "no-console": "off",
        },
      },
    ],
  },
  test: {
    include: [
      "apps/**/*.test.ts",
      "apps/**/*.test.tsx",
      "packages/**/*.test.ts",
      "packages/**/*.test.tsx",
    ],
    exclude: ["**/node_modules/**", "**/dist/**", "**/.output/**"],
    environment: "node",
  },
  run: {
    cache: {
      scripts: true,
      tasks: true,
    },
    tasks: {
      lint: {
        command: "vp lint",
      },
    },
  },
});

function verselfBuildTelemetryPlugin(): Plugin {
  let startedAt = 0;
  let outputCount = 0;
  let outputBytes = 0;
  return {
    name: "verself-build-telemetry",
    apply: "build",
    buildStart() {
      startedAt = performance.now();
      outputCount = 0;
      outputBytes = 0;
    },
    generateBundle(_options: unknown, bundle: Record<string, BundleItem>) {
      outputCount = Object.keys(bundle).length;
      outputBytes = Object.values(bundle).reduce((sum, item) => {
        if (typeof item.code === "string") {
          return sum + Buffer.byteLength(item.code);
        }
        if (typeof item.source === "string") {
          return sum + Buffer.byteLength(item.source);
        }
        if (item.source instanceof Uint8Array) {
          return sum + item.source.byteLength;
        }
        return sum;
      }, 0);
    },
    closeBundle() {
      const workspaceRoot = findUp(process.cwd(), "pnpm-workspace.yaml");
      const packageDir = findUp(process.cwd(), "package.json");
      const packageJSON = JSON.parse(readFileSync(join(packageDir, "package.json"), "utf8"));
      const durationMs = Math.max(0, Math.round(performance.now() - startedAt));
      recordBuildEvent(workspaceRoot, {
        name: "build.bundle",
        attributes: {
          "analytics.dataset": "build",
          "build.tool": "vite",
          "build.package": packageJSON.name ?? rel(workspaceRoot, packageDir),
          "build.command": "build",
          "build.config_path": "vite.config.ts",
          "vite.config_path": "vite.config.ts",
          bundler: "rolldown",
          "bundle.output_count": outputCount,
          "bundle.output_bytes": outputBytes,
          duration_ms: durationMs,
          status: "succeeded",
        },
      });
    },
  };
}

function recordBuildEvent(workspaceRoot: string, event: BuildTelemetryEvent) {
  const queuePath =
    process.env.VERSELF_BUILD_TELEMETRY_QUEUE ??
    join(process.env.HOME ?? workspaceRoot, ".cache", "verself-web", "events", "build-events.ndjson");
  mkdirSync(dirname(queuePath), { recursive: true });
  const timestamp = BigInt(Date.now()) * 1_000_000n;
  appendFileSync(
    queuePath,
    `${JSON.stringify({
      name: event.name,
      timeUnixNano: timestamp.toString(),
      attributes: {
        "event.id": randomUUID(),
        "event.name": event.name,
        "service.name": "verself-build-telemetry",
        "service.version": "0.1.0",
        ...event.attributes,
      },
    })}\n`,
    "utf8",
  );
}

function findUp(start: string, marker: string) {
  let current = start;
  while (true) {
    if (existsSync(join(current, marker))) {
      return current;
    }
    const parent = dirname(current);
    if (parent === current) {
      throw new Error(`could not find ${marker} from ${start}`);
    }
    current = parent;
  }
}

function rel(from: string, to: string) {
  return relative(from, to).split(sep).join("/");
}
