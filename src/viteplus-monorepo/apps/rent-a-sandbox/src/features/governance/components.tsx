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
              Recent authorization decisions and operational changes for this organization.
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
          <TableHead>Service</TableHead>
          <TableHead>Operation</TableHead>
          <TableHead>Result</TableHead>
          <TableHead>Principal</TableHead>
          <TableHead>Sequence</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => (
          <TableRow key={event.event_id}>
            <TableCell>{formatDate(event.recorded_at)}</TableCell>
            <TableCell>{event.service_name}</TableCell>
            <TableCell>{event.operation_id}</TableCell>
            <TableCell>
              <StatusBadge status={event.result} />
            </TableCell>
            <TableCell>{event.principal_email || event.principal_id}</TableCell>
            <TableCell>{event.sequence}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
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
