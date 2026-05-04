import {
  Download,
  FileSearch,
  Filter,
  Link2,
  Search,
  ShieldCheck,
} from "lucide-react";
import { useMemo, useState } from "react";
import {
  auditActorKinds,
  auditCategories,
  auditCorrelationChain,
  auditOutcomes,
  auditSeverities,
  buildAuditExplorerModel,
  defaultAuditFilters,
  updateAuditFilterSet,
  type AuditActorKind,
  type AuditCategory,
  type AuditExplorerModel,
  type AuditFilterState,
  type AuditGroupBy,
  type AuditLogEntry,
  type AuditOutcome,
  type AuditSeverity,
  type AuditTimeRange,
} from "./audit-log-model.ts";
import "./AuditLogExplorer.css";

const timeRangeOptions: readonly { readonly value: AuditTimeRange; readonly label: string }[] = [
  { value: "1h", label: "1 hour" },
  { value: "24h", label: "24 hours" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
  { value: "custom", label: "Custom" },
];

const groupByOptions: readonly { readonly value: AuditGroupBy; readonly label: string }[] = [
  { value: "correlation", label: "Correlation" },
  { value: "hour", label: "Hour" },
  { value: "actor", label: "Actor" },
  { value: "none", label: "None" },
];

const emptyAuditEntries: readonly AuditLogEntry[] = [];

export function AuditLogExplorer({
  entries,
  now,
}: {
  readonly entries?: readonly AuditLogEntry[];
  readonly now?: Date;
}) {
  const [filters, setFilters] = useState<AuditFilterState>(defaultAuditFilters);
  const auditEntries = entries ?? emptyAuditEntries;
  const model = useMemo(
    () => buildAuditExplorerModel(auditEntries, filters, now),
    [auditEntries, filters, now],
  );
  const selectedEntry = model.selectedEntry;
  const correlationEntries = selectedEntry
    ? auditCorrelationChain(auditEntries, selectedEntry.correlationId)
    : [];

  return (
    <section className="hh-audit-explorer" data-testid="audit-log-explorer">
      <AuditFilterRail filters={filters} model={model} setFilters={setFilters} />

      <section className="hh-audit-main" aria-label="Audit timeline">
        <header className="hh-audit-toolbar">
          <div>
            <span>Diagnostics</span>
            <h2>Audit log explorer</h2>
          </div>
          <div className="hh-audit-export-preview" aria-label="Redacted export preview">
            <Download size={16} strokeWidth={2.1} />
            <div>
              <strong>{model.exportPreview.fileName}</strong>
              <span>
                {model.exportPreview.totalEntries} entries - fingerprint {model.exportPreview.fingerprint}
              </span>
            </div>
          </div>
        </header>

        <div className="hh-audit-result-count" aria-live="polite">
          {model.filteredEntries.length} of {model.entries.length} entries
        </div>

        <div className="hh-audit-groups">
          {model.emptyReason ? (
            <div className="hh-audit-empty" data-testid="audit-log-empty">
              <FileSearch size={19} strokeWidth={2.1} />
              <strong>{model.emptyReason === "no-entries" ? "No audit entries" : "No matching entries"}</strong>
              <span>Adjust filters or reconnect the daemon to refresh diagnostics state.</span>
            </div>
          ) : (
            model.groups.map((group) => (
              <section className="hh-audit-group" key={group.id}>
                <header>{group.label}</header>
                <div className="hh-audit-entry-list">
                  {group.entries.map((entry) => (
                    <button
                      className="hh-audit-entry-row"
                      data-selected={entry.id === selectedEntry?.id}
                      key={entry.id}
                      onClick={() => setFilters((current) => ({ ...current, selectedEntryId: entry.id }))}
                      type="button"
                    >
                      <span className={`hh-audit-severity hh-audit-severity-${entry.severity}`}>
                        {entry.severity}
                      </span>
                      <span className="hh-audit-row-body">
                        <strong>{entry.summary}</strong>
                        <small>
                          {formatAuditTime(entry.timestamp)} - {entry.actor.displayName} - {entry.actionType}
                        </small>
                      </span>
                      <span className={`hh-audit-outcome hh-audit-outcome-${entry.outcome}`}>
                        {entry.outcome.replaceAll("_", " ")}
                      </span>
                    </button>
                  ))}
                </div>
              </section>
            ))
          )}
        </div>
      </section>

      <AuditDetailPane entry={selectedEntry} correlationEntries={correlationEntries} />
    </section>
  );
}

function AuditFilterRail({
  filters,
  model,
  setFilters,
}: {
  readonly filters: AuditFilterState;
  readonly model: AuditExplorerModel;
  readonly setFilters: (update: (filters: AuditFilterState) => AuditFilterState) => void;
}) {
  return (
    <aside className="hh-audit-filter-rail" aria-label="Audit filters">
      <div className="hh-audit-filter-heading">
        <Filter size={17} strokeWidth={2.1} />
        <h3>Filters</h3>
      </div>

      <label className="hh-audit-field">
        <span>Search</span>
        <div className="hh-audit-search-box">
          <Search size={15} strokeWidth={2.1} />
          <input
            aria-label="Search audit entries"
            onChange={(event) => setFilters((current) => ({ ...current, query: event.currentTarget.value }))}
            placeholder="actor, action, reason"
            value={filters.query}
          />
        </div>
      </label>

      <label className="hh-audit-field">
        <span>Time range</span>
        <select
          aria-label="Audit time range"
          onChange={(event) => setFilters((current) => ({ ...current, timeRange: event.currentTarget.value as AuditTimeRange }))}
          value={filters.timeRange}
        >
          {timeRangeOptions.map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </select>
      </label>

      {filters.timeRange === "custom" ? (
        <div className="hh-audit-custom-range">
          <input
            aria-label="Audit custom from"
            onChange={(event) => setFilters((current) => ({ ...current, customFrom: event.currentTarget.value }))}
            placeholder="from ISO time"
            value={filters.customFrom}
          />
          <input
            aria-label="Audit custom to"
            onChange={(event) => setFilters((current) => ({ ...current, customTo: event.currentTarget.value }))}
            placeholder="to ISO time"
            value={filters.customTo}
          />
        </div>
      ) : null}

      <label className="hh-audit-field">
        <span>Group by</span>
        <select
          aria-label="Group audit entries"
          onChange={(event) => setFilters((current) => ({ ...current, groupBy: event.currentTarget.value as AuditGroupBy }))}
          value={filters.groupBy}
        >
          {groupByOptions.map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </select>
      </label>

      <div className="hh-audit-field">
        <span>Correlation</span>
        <input
          aria-label="Correlation id"
          onChange={(event) => setFilters((current) => ({ ...current, correlationId: event.currentTarget.value }))}
          placeholder="corr-*"
          value={filters.correlationId}
        />
        <input
          aria-label="Causation id"
          onChange={(event) => setFilters((current) => ({ ...current, causationId: event.currentTarget.value }))}
          placeholder="caused by event"
          value={filters.causationId}
        />
      </div>

      <ChipGroup
        label="Actors"
        values={auditActorKinds}
        selected={filters.actorKinds}
        counts={model.facets.actorKinds}
        onToggle={(value) => setFilters((current) => ({
          ...current,
          actorKinds: updateAuditFilterSet(current.actorKinds, value),
        }))}
      />
      <ChipGroup
        label="Categories"
        values={auditCategories}
        selected={filters.categories}
        counts={model.facets.categories}
        onToggle={(value) => setFilters((current) => ({
          ...current,
          categories: updateAuditFilterSet(current.categories, value),
        }))}
      />
      <ChipGroup
        label="Severity"
        values={auditSeverities}
        selected={filters.severities}
        counts={model.facets.severities}
        onToggle={(value) => setFilters((current) => ({
          ...current,
          severities: updateAuditFilterSet(current.severities, value),
        }))}
      />
      <ChipGroup
        label="Outcomes"
        values={auditOutcomes}
        selected={filters.outcomes}
        counts={model.facets.outcomes}
        onToggle={(value) => setFilters((current) => ({
          ...current,
          outcomes: updateAuditFilterSet(current.outcomes, value),
        }))}
      />
    </aside>
  );
}

function ChipGroup<TValue extends AuditActorKind | AuditCategory | AuditSeverity | AuditOutcome>({
  counts,
  label,
  onToggle,
  selected,
  values,
}: {
  readonly counts: readonly { readonly value: string; readonly count: number }[];
  readonly label: string;
  readonly onToggle: (value: TValue) => void;
  readonly selected: readonly TValue[];
  readonly values: readonly TValue[];
}) {
  const countByValue = new Map(counts.map((facet) => [facet.value, facet.count]));
  return (
    <fieldset className="hh-audit-chip-group">
      <legend>{label}</legend>
      <div>
        {values.map((value) => (
          <button
            aria-pressed={selected.includes(value)}
            className="hh-audit-chip"
            data-active={selected.includes(value)}
            key={value}
            onClick={() => onToggle(value)}
            type="button"
          >
            <span>{value.replaceAll("_", " ")}</span>
            <small>{countByValue.get(value) ?? 0}</small>
          </button>
        ))}
      </div>
    </fieldset>
  );
}

function AuditDetailPane({
  correlationEntries,
  entry,
}: {
  readonly correlationEntries: readonly AuditLogEntry[];
  readonly entry: AuditLogEntry | null;
}) {
  if (!entry) {
    return (
      <aside className="hh-audit-detail" aria-label="Audit detail">
        <div className="hh-audit-empty">
          <FileSearch size={19} strokeWidth={2.1} />
          <strong>Select an entry</strong>
          <span>Filtered audit details appear here.</span>
        </div>
      </aside>
    );
  }

  return (
    <aside className="hh-audit-detail" aria-label="Audit detail">
      <header>
        <span className={`hh-audit-severity hh-audit-severity-${entry.severity}`}>
          {entry.severity}
        </span>
        <h3>{entry.summary}</h3>
        <small>{entry.id} - {formatAuditTime(entry.timestamp)}</small>
      </header>

      <dl className="hh-audit-metadata">
        <div>
          <dt>Actor</dt>
          <dd>{entry.actor.displayName} ({entry.actor.kind})</dd>
        </div>
        <div>
          <dt>Action</dt>
          <dd>{entry.actionType}</dd>
        </div>
        <div>
          <dt>Outcome</dt>
          <dd>{entry.outcome.replaceAll("_", " ")}</dd>
        </div>
        <div>
          <dt>Correlation</dt>
          <dd>{entry.correlationId}</dd>
        </div>
        <div>
          <dt>Causation</dt>
          <dd>{entry.causationId ?? "root event"}</dd>
        </div>
      </dl>

      <section className="hh-audit-detail-section">
        <h4>Reason</h4>
        <p>{entry.reason}</p>
      </section>

      {entry.commandPreview ? (
        <section className="hh-audit-detail-section">
          <h4>Command preview</h4>
          <code>{entry.commandPreview}</code>
        </section>
      ) : null}

      {entry.redactionMarkers?.length ? (
        <section className="hh-audit-detail-section">
          <h4>
            <ShieldCheck size={15} strokeWidth={2.1} />
            Redacted
          </h4>
          <div className="hh-audit-marker-list">
            {entry.redactionMarkers.map((marker) => <code key={marker}>{marker}</code>)}
          </div>
        </section>
      ) : null}

      {entry.postconditions?.length ? (
        <section className="hh-audit-detail-section">
          <h4>Postconditions</h4>
          <ul>
            {entry.postconditions.map((postcondition) => <li key={postcondition}>{postcondition}</li>)}
          </ul>
        </section>
      ) : null}

      {entry.linkedArtifacts?.length ? (
        <section className="hh-audit-detail-section">
          <h4>
            <Link2 size={15} strokeWidth={2.1} />
            Linked artifacts
          </h4>
          <div className="hh-audit-artifact-list">
            {entry.linkedArtifacts.map((artifact) => (
              <span data-resolved={artifact.resolved} key={`${artifact.kind}:${artifact.id}`}>
                <strong>{artifact.kind}</strong>
                <code>{artifact.label}</code>
              </span>
            ))}
          </div>
        </section>
      ) : null}

      <section className="hh-audit-detail-section">
        <h4>Correlation chain</h4>
        <ol className="hh-audit-chain">
          {correlationEntries.map((candidate) => (
            <li data-current={candidate.id === entry.id} key={candidate.id}>
              <span>{formatAuditTime(candidate.timestamp)}</span>
              <strong>{candidate.actionType}</strong>
            </li>
          ))}
        </ol>
      </section>
    </aside>
  );
}

function formatAuditTime(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toISOString().slice(11, 19);
}
