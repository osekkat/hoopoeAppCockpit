import { FileSearch, ShieldCheck } from "lucide-react";
import { AuditLogExplorer } from "./AuditLogExplorer.tsx";

export function DiagnosticsStage({ projectId }: { readonly projectId: string }) {
  return (
    <div className="hh-live-stage hh-diagnostics-stage" data-testid="diagnostics-audit-stage">
      <section className="hh-fixture-strip" aria-label="Diagnostics audit source">
        <span>audit-log</span>
        <strong>{projectId}</strong>
        <span>redacted export</span>
        <span>correlation browser</span>
      </section>

      <section className="hh-diagnostics-summary" aria-label="Diagnostics scope">
        <span>
          <FileSearch size={15} strokeWidth={2.1} />
          filterable audit history
        </span>
        <span>
          <ShieldCheck size={15} strokeWidth={2.1} />
          redaction-aware detail
        </span>
      </section>

      <AuditLogExplorer projectId={projectId} />
    </div>
  );
}
