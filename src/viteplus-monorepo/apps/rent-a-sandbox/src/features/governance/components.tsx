import { useMutation } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  PageSection,
  PageSections,
  SectionActions,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { formatDateTimeUTC } from "~/lib/format";
import { createGovernanceDataExport, downloadGovernanceDataExport } from "~/server-fns/api";
import type { GovernanceAuditEvent, GovernanceExportJob } from "~/server-fns/api";

export function GovernanceSettings({
  auditEvents,
  exports,
}: {
  auditEvents: Array<GovernanceAuditEvent>;
  exports: Array<GovernanceExportJob>;
}) {
  const router = useRouter();
  const createExport = useMutation({
    mutationFn: () =>
      createGovernanceDataExport({
        data: {
          include_logs: false,
          scopes: ["identity", "billing", "sandbox", "audit"],
        },
      }),
    onSuccess: async () => {
      await router.invalidate();
    },
  });
  const downloadExport = useMutation({
    mutationFn: (exportID: string) =>
      downloadGovernanceDataExport({
        data: { export_id: exportID },
      }),
    onSuccess: (artifact) => {
      downloadBase64Artifact(artifact.data_base64, artifact.content_type, artifact.file_name);
    },
  });
  const error = createExport.error ?? downloadExport.error;

  return (
    <PageSections>
      <PageSection>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Data export</SectionTitle>
            <SectionDescription>
              Download organization data, billing records, sandbox metadata, and audit evidence.
            </SectionDescription>
          </SectionHeaderContent>
          <SectionActions>
            <Button
              type="button"
              onClick={() => createExport.mutate()}
              disabled={createExport.isPending}
              data-testid="create-data-export"
            >
              {createExport.isPending ? "Creating export" : "Create data export"}
            </Button>
          </SectionActions>
        </SectionHeader>
        {error ? <p className="text-sm text-destructive">{formatError(error)}</p> : null}
        <ExportsTable
          exports={exports}
          onDownload={(exportID) => downloadExport.mutate(exportID)}
          downloadingExportID={downloadExport.isPending ? downloadExport.variables : undefined}
        />
      </PageSection>

      <PageSection>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Audit trail</SectionTitle>
            <SectionDescription>
              High-risk operations, denied requests, and export activity for this organization.
            </SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>
        <AuditEventsTable events={auditEvents} />
      </PageSection>
    </PageSections>
  );
}

function ExportsTable({
  exports,
  onDownload,
  downloadingExportID,
}: {
  exports: Array<GovernanceExportJob>;
  onDownload: (exportID: string) => void;
  downloadingExportID: string | undefined;
}) {
  if (exports.length === 0) {
    return <p className="text-sm text-muted-foreground">No exports have been created yet.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Created</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Scope</TableHead>
          <TableHead>Files</TableHead>
          <TableHead>Size</TableHead>
          <TableHead>Artifact</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {exports.map((job) => (
          <TableRow key={job.export_id}>
            <TableCell>{formatDate(job.created_at)}</TableCell>
            <TableCell>
              <StatusBadge status={job.state} />
            </TableCell>
            <TableCell>{job.scopes.join(", ")}</TableCell>
            <TableCell>{job.files.length}</TableCell>
            <TableCell>{formatBytes(job.artifact_bytes)}</TableCell>
            <TableCell>
              {job.download_url ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onDownload(job.export_id)}
                  disabled={downloadingExportID === job.export_id}
                  data-testid={`download-data-export-${job.export_id}`}
                >
                  {downloadingExportID === job.export_id ? "Downloading" : "Download"}
                </Button>
              ) : (
                <span className="text-muted-foreground">Unavailable</span>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function AuditEventsTable({ events }: { events: Array<GovernanceAuditEvent> }) {
  if (events.length === 0) {
    return <p className="text-sm text-muted-foreground">No audit events recorded yet.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Time</TableHead>
          <TableHead>Risk</TableHead>
          <TableHead>Actor</TableHead>
          <TableHead>Operation</TableHead>
          <TableHead>Target</TableHead>
          <TableHead>Result</TableHead>
          <TableHead>Location</TableHead>
          <TableHead>Source</TableHead>
          <TableHead>Sequence</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => (
          <TableRow key={event.event_id}>
            <TableCell>{formatDate(event.recorded_at)}</TableCell>
            <TableCell>
              <RiskBadge risk={event.risk_level} />
            </TableCell>
            <TableCell>
              <div className="flex flex-col gap-1">
                <span>{actorLabel(event)}</span>
                <span className="text-muted-foreground">{event.actor_type}</span>
              </div>
            </TableCell>
            <TableCell>
              <div className="flex flex-col gap-1">
                <span>{event.operation_display || event.operation_id}</span>
                <span className="text-muted-foreground">{event.operation_type}</span>
              </div>
            </TableCell>
            <TableCell>{targetLabel(event)}</TableCell>
            <TableCell>
              <StatusBadge status={event.result} />
            </TableCell>
            <TableCell>{locationLabel(event)}</TableCell>
            <TableCell>{event.source_product_area || event.service_name}</TableCell>
            <TableCell>{event.sequence}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function RiskBadge({ risk }: { risk: string }) {
  const variant = risk === "critical" || risk === "high" ? "warning" : "secondary";
  return <Badge variant={variant}>{risk}</Badge>;
}

function StatusBadge({ status }: { status: string }) {
  const variant =
    status === "completed" || status === "allowed"
      ? "success"
      : status === "failed" || status === "denied" || status === "error"
        ? "destructive"
        : "secondary";
  return <Badge variant={variant}>{status}</Badge>;
}

function actorLabel(event: GovernanceAuditEvent) {
  if (event.actor_type === "api_credential" && event.credential_name) {
    return event.credential_name;
  }
  return event.actor_display || event.actor_id;
}

function targetLabel(event: GovernanceAuditEvent) {
  const target = event.target_display || event.target_id || "organization";
  return `${event.target_kind}: ${target}`;
}

function locationLabel(event: GovernanceAuditEvent) {
  const parts = [event.geo_country, event.client_ip_version].filter(Boolean);
  return parts.length > 0 ? parts.join(" ") : "unknown";
}

function formatDate(value: string) {
  return formatDateTimeUTC(value);
}

function formatBytes(value: string) {
  const bytes = Number(value);
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatError(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function downloadBase64Artifact(dataBase64: string, contentType: string, fileName: string) {
  const binary = atob(dataBase64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  const url = URL.createObjectURL(new Blob([bytes], { type: contentType }));
  const link = document.createElement("a");
  link.href = url;
  link.download = fileName;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
