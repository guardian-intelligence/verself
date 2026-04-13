import { env } from "./env";
import { ensureTestUserExists, shortTimeoutMS, test, type SandboxHarness } from "./harness";
import {
  assertExecutionDetailHydratesLogs,
  assertJobsIndexHydratesExecutionList,
  assertRepoDetailSSRStable,
  importRepoFromURL,
  launchDirectExecution,
  rescanRepoMetadata,
  waitForExecutionSuccess,
} from "./repo-helpers";

function verificationRunCommand(marker: string): string {
  const quotedMarker = `'${marker.replaceAll("'", "'\\''")}'`;
  return [
    `test "$(id -u)" = "1000"`,
    `printf '%s\\n' ${quotedMarker} > ./forge-metal-proof.txt`,
    `printf '%s\\n' ${quotedMarker} > "$HOME/forge-metal-proof.txt"`,
    `test -s ./forge-metal-proof.txt`,
    `test -s "$HOME/forge-metal-proof.txt"`,
    `test -x /opt/actions-runner/run.sh`,
    `test -x /usr/local/bin/forgejo-runner`,
    `go version | grep -q 'go1.25.8'`,
    `node --version | grep -q '^v22.22.2$'`,
    `forgejo-runner --version | grep -q '12.8.2'`,
    `printf '%s\\n' ${quotedMarker}`,
  ].join(" && ");
}

test.describe("Rent-a-Sandbox Repo Journeys", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("repo import renders a stable metadata detail page after scan", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/repos/new";
      const importedRepo = await importRepoFromURL(app, env.verificationRepoURL);

      Object.assign(run, importedRepo, {
        detail_url: `/repos/${importedRepo.repo_id}`,
      });

      await assertRepoDetailSSRStable(
        app,
        importedRepo.repo_id,
        importedRepo.repo_name,
        importedRepo.import_scanned_sha,
      );

      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });

  test("repo rescan updates metadata without preparing an execution artifact", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      const importedRepo = await importRepoFromURL(app, env.verificationRepoURL);
      Object.assign(run, importedRepo, {
        detail_url: `/repos/${importedRepo.repo_id}`,
      });

      const refreshedRepoMeta = await app.pushVerificationRepoRevision(`${app.runID}-refresh`);
      const rescannedRepo = await rescanRepoMetadata(app, importedRepo.repo_id, refreshedRepoMeta);

      Object.assign(run, rescannedRepo);

      await assertRepoDetailSSRStable(
        app,
        importedRepo.repo_id,
        importedRepo.repo_name,
        rescannedRepo.rescan_scanned_sha,
      );

      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });

  test("direct execution preserves jobs index and job detail through hydration", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      const startedAccountBalance = await readVisibleAccountBalanceUnits(app);

      const importedRepo = await importRepoFromURL(app, env.verificationRepoURL);
      Object.assign(run, importedRepo);

      const execution = await launchDirectExecution(
        app,
        verificationRunCommand(env.verificationLogMarker),
      );
      run.execution_id = execution.execution_id;
      run.detail_url = `/jobs/${execution.execution_id}`;

      await assertJobsIndexHydratesExecutionList(app, execution.execution_id);
      await assertExecutionDetailHydratesLogs(
        app,
        execution.execution_id,
        env.verificationLogMarker,
      );

      run.finished_balance = await app.waitForCondition("balance decrease", 60_000, async () => {
        await app.goto("/");
        const currentBalance = await app.readBalance();
        return currentBalance < run.started_balance ? currentBalance : false;
      });
      await app.waitForCondition("account balance reservation released", 60_000, async () => {
        const currentAccountBalance = await readVisibleAccountBalanceUnits(app);
        return currentAccountBalance >= startedAccountBalance ? currentAccountBalance : false;
      });

      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });

  test("full lifecycle proof imports executes rescans and executes again", async ({ app }) => {
    test.skip(!env.proofMode, "Proof loop only");

    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();

      const importedRepo = await importRepoFromURL(app, env.verificationRepoURL);
      Object.assign(run, importedRepo);

      const firstExecution = await launchDirectExecution(
        app,
        verificationRunCommand(env.verificationLogMarker),
      );
      run.execution_id = firstExecution.execution_id;
      run.detail_url = `/jobs/${firstExecution.execution_id}`;
      await assertExecutionDetailHydratesLogs(
        app,
        firstExecution.execution_id,
        env.verificationLogMarker,
      );

      const refreshedRepoMeta = await app.pushVerificationRepoRevision(`${app.runID}-refresh`);
      const rescannedRepo = await rescanRepoMetadata(app, importedRepo.repo_id, refreshedRepoMeta);
      Object.assign(run, rescannedRepo);

      await assertRepoDetailSSRStable(
        app,
        importedRepo.repo_id,
        importedRepo.repo_name,
        rescannedRepo.rescan_scanned_sha,
      );

      const secondExecution = await launchDirectExecution(
        app,
        verificationRunCommand(env.verificationLogMarker),
      );
      run.execution_id = secondExecution.execution_id;
      run.detail_url = `/jobs/${secondExecution.execution_id}`;

      await waitForExecutionSuccess(app, secondExecution.execution_id, env.verificationLogMarker);
      await assertExecutionDetailHydratesLogs(
        app,
        secondExecution.execution_id,
        env.verificationLogMarker,
      );

      run.finished_balance = await app.waitForCondition(
        "balance decrease after rescan execution",
        60_000,
        async () => {
          await app.goto("/");
          const currentBalance = await app.readBalance();
          return currentBalance < run.started_balance ? currentBalance : false;
        },
      );

      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });
});

async function readVisibleAccountBalanceUnits(app: SandboxHarness) {
  await app.goto("/billing");
  const accountBalance = app.page.getByTestId("entitlements-account-balance").first();
  await accountBalance.waitFor({ state: "visible", timeout: shortTimeoutMS });
  const raw = await accountBalance.getAttribute("data-account-balance-units");
  if (raw === null) {
    throw new Error("account balance missing data-account-balance-units");
  }
  const units = Number.parseInt(raw, 10);
  if (!Number.isFinite(units)) {
    throw new Error(`account balance units is not numeric: ${raw}`);
  }
  return units;
}
