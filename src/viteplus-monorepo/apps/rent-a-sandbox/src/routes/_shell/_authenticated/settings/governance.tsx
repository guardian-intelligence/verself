import { createFileRoute } from "@tanstack/react-router";
import { GovernanceSettings } from "~/features/governance/components";
import { listGovernanceAuditEvents, listGovernanceDataExports } from "~/server-fns/api";

export const Route = createFileRoute("/_shell/_authenticated/settings/governance")({
  loader: async () => {
    const [audit, exports] = await Promise.all([
      listGovernanceAuditEvents(),
      listGovernanceDataExports(),
    ]);
    return { audit, exports };
  },
  component: GovernanceSettingsRoute,
});

function GovernanceSettingsRoute() {
  const { audit, exports } = Route.useLoaderData();
  return <GovernanceSettings auditEvents={audit.events} exports={exports} />;
}
