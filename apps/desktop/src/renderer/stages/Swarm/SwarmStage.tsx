import { Activity, Inbox, LayoutDashboard, Users } from "lucide-react";
import { useSwarmStageQuery } from "../../data/stage-data.ts";
import { StateSurface } from "../../state-view/index.ts";
import "./SwarmStage.css";

export function SwarmStage({ projectId }: { readonly projectId: string }) {
  const query = useSwarmStageQuery(projectId);

  if (query.isLoading) {
    return (
      <StateSurface
        variant="loading"
        eyebrow="Swarm"
        title="Loading swarm"
        description="Fetching NTM session state, bead assignments, and activity mail."
        testId="swarm-stage-loading"
      />
    );
  }

  if (query.isError || !query.data) {
    return (
      <StateSurface
        variant="error"
        eyebrow="Swarm"
        icon={<Activity size={18} strokeWidth={2.1} />}
        title="Swarm data unavailable"
        description="Reconnect the daemon before launching or tending agents."
        testId="swarm-stage-error"
      />
    );
  }

  const { data } = query;

  return (
    <div className="hh-live-stage hh-swarm-stage" data-testid="mock-swarm-stage">
      <section className="hh-fixture-strip" aria-label="Mock Flywheel source">
        <span>{data.source.scenarioId}</span>
        <strong>{data.source.fixturesVersion}</strong>
        <span>{data.source.transport}</span>
        <span>{data.counters.alive} active agents</span>
      </section>

      <section className="hh-swarm-metrics" aria-label="Swarm counters">
        <Metric label="Sessions" value={data.counters.sessions} />
        <Metric label="Panes" value={data.counters.panes} />
        <Metric label="Alive" value={data.counters.alive} />
        <Metric label="Wedged" value={data.counters.wedged} />
      </section>

      <section className="hh-swarm-layout">
        <div className="hh-swarm-panel">
          <div className="hh-stage-section-title">
            <LayoutDashboard size={17} strokeWidth={2.1} />
            <h2>Bead board</h2>
          </div>
          <div className="hh-swarm-bead-board">
            {data.beadBoard.length > 0 ? data.beadBoard.map((assignment) => (
              <article className="hh-swarm-bead-card" key={assignment.beadId}>
                <code>{assignment.beadId}</code>
                <span>{assignment.agents.join(", ")}</span>
              </article>
            )) : (
              <StateSurface
                variant="empty"
                density="compact"
                title="No bead claims"
                description="Launch or resume a swarm to populate assignments."
                testId="swarm-bead-board-empty"
              />
            )}
          </div>
        </div>

        <div className="hh-swarm-panel">
          <div className="hh-stage-section-title">
            <Users size={17} strokeWidth={2.1} />
            <h2>Agent grid</h2>
          </div>
          <div className="hh-swarm-agent-grid">
            {data.sessions.length > 0 ? data.sessions.map((session) => (
              <section className="hh-swarm-session" key={session.id} aria-label={session.id}>
                <header>{session.id}</header>
                {session.agents.map((agent) => (
                  <article className="hh-agent-tile" key={agent.id}>
                    <div>
                      <strong>{agent.agent}</strong>
                      <span>{agent.program}</span>
                    </div>
                    <div>
                      <code>{agent.bead ?? "unassigned"}</code>
                      <span>{agent.state}</span>
                    </div>
                    <small>{agent.model}</small>
                  </article>
                ))}
              </section>
            )) : (
              <StateSurface
                variant="empty"
                density="compact"
                title="No active agents"
                description="Start a swarm after beads are ready."
                testId="swarm-agent-grid-empty"
              />
            )}
          </div>
        </div>

        <div className="hh-swarm-panel hh-swarm-mail-panel">
          <div className="hh-stage-section-title">
            <Inbox size={17} strokeWidth={2.1} />
            <h2>Activity mail</h2>
          </div>
          <div className="hh-swarm-mail-summary">
            <span>{data.mail.unreadTotal} unread</span>
            <span>{data.mail.threads.join(", ")}</span>
          </div>
          <div className="hh-swarm-mail-list">
            {data.mail.messages.length > 0 ? data.mail.messages.map((message) => (
              <article className="hh-swarm-mail-item" key={message.id}>
                <code>{message.threadId}</code>
                <strong>{message.subject}</strong>
                <span>{message.from}</span>
              </article>
            )) : (
              <StateSurface
                variant="empty"
                density="compact"
                title="No activity mail"
                description="Agent Mail events appear here after the swarm starts."
                testId="swarm-mail-empty"
              />
            )}
          </div>
        </div>
      </section>
    </div>
  );
}

function Metric({ label, value }: { readonly label: string; readonly value: number }) {
  return (
    <article className="hh-swarm-metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}
