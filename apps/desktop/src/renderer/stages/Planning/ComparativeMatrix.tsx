import { ArrowLeftRight, Highlighter } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { PlanArtifact } from "../../data/plan-data.ts";

interface ComparativeMatrixProps {
  readonly candidates: readonly PlanArtifact[];
}

export function ComparativeMatrix({ candidates }: ComparativeMatrixProps) {
  const columnRefs = useRef<HTMLDivElement[]>([]);
  const programmaticScrollRef = useRef(false);
  const [syncedScroll, setSyncedScroll] = useState(true);
  const [highlightDiff, setHighlightDiff] = useState(true);

  useEffect(() => {
    if (!syncedScroll) return;

    const handlers = columnRefs.current.map((column, index) => {
      const handler = () => {
        if (programmaticScrollRef.current) return;
        programmaticScrollRef.current = true;
        for (let other = 0; other < columnRefs.current.length; other += 1) {
          if (other === index) continue;
          const otherColumn = columnRefs.current[other];
          if (!otherColumn) continue;
          otherColumn.scrollTop = column.scrollTop;
        }
        requestAnimationFrame(() => {
          programmaticScrollRef.current = false;
        });
      };
      column.addEventListener("scroll", handler, { passive: true });
      return () => column.removeEventListener("scroll", handler);
    });

    return () => {
      for (const cleanup of handlers) cleanup();
    };
  }, [syncedScroll, candidates.length]);

  if (candidates.length < 2) {
    return (
      <div className="hh-plan-compare-empty" role="status">
        <ArrowLeftRight size={18} strokeWidth={2.1} />
        <span>Need at least 2 candidates to render the comparative matrix.</span>
      </div>
    );
  }

  return (
    <div className="hh-plan-compare" data-testid="comparative-matrix">
      <header className="hh-plan-compare-header">
        <div className="hh-plan-compare-title">
          <ArrowLeftRight size={14} strokeWidth={2.1} />
          <span>Side-by-side compare</span>
        </div>
        <div className="hh-plan-compare-controls">
          <label className="hh-plan-compare-control">
            <input
              type="checkbox"
              checked={syncedScroll}
              onChange={(event) => setSyncedScroll(event.target.checked)}
              data-testid="compare-sync-scroll"
            />
            <span>Sync scroll</span>
          </label>
          <label className="hh-plan-compare-control">
            <input
              type="checkbox"
              checked={highlightDiff}
              onChange={(event) => setHighlightDiff(event.target.checked)}
              data-testid="compare-highlight-diff"
            />
            <Highlighter size={11} strokeWidth={2.1} />
            <span>Highlight diff</span>
          </label>
        </div>
      </header>
      <div className="hh-plan-compare-grid" data-candidate-count={candidates.length}>
        {candidates.map((candidate, index) => (
          <article
            key={candidate.path}
            className="hh-plan-compare-column"
            data-testid={`compare-col-${index}`}
          >
            <header className="hh-plan-compare-column-header">
              <span className="hh-plan-compare-column-label">{candidate.label}</span>
              <dl className="hh-plan-compare-column-meta">
                {candidate.model ? (
                  <div>
                    <dt>Model</dt>
                    <dd>{candidate.model}</dd>
                  </div>
                ) : null}
                {candidate.harness ? (
                  <div>
                    <dt>Harness</dt>
                    <dd>{candidate.harness}</dd>
                  </div>
                ) : null}
                {candidate.caamAccount ? (
                  <div>
                    <dt>CAAM</dt>
                    <dd>{candidate.caamAccount}</dd>
                  </div>
                ) : null}
                {typeof candidate.latencyMs === "number" ? (
                  <div>
                    <dt>Latency</dt>
                    <dd>{(candidate.latencyMs / 1000).toFixed(1)}s</dd>
                  </div>
                ) : null}
                <div>
                  <dt>Status</dt>
                  <dd>{candidate.status}</dd>
                </div>
              </dl>
            </header>
            <div
              className={`hh-plan-compare-column-body${
                highlightDiff ? " hh-plan-compare-highlight" : ""
              }`}
              ref={(node) => {
                if (node) columnRefs.current[index] = node;
              }}
            >
              <pre className="hh-plan-compare-column-content">{candidate.content}</pre>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}
