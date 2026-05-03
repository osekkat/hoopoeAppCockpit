import { Activity, Inbox, LayoutDashboard, Users } from "lucide-react";
import { useSwarmStageQuery } from "../../data/stage-data.ts";
import "./SwarmStage.css";

export function SwarmStage({ projectId }: { readonly projectId: string }) {
  const query = useSwarmStageQuery(projectId);

  if (query.isLoading) {
    return <div className="hh-live-stage hh-live-stage-loading">Loading Mock Flywheel swarm...</div>;
  }

  if (query.isError || !query.data) {
    return (
      <div className="hh-live-stage hh-live-stage-error" role="status">
        <Activity size={18} strokeWidth={2.1} />
        <span>Swarm data is not available for this project yet.</span>
      </div>
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
            {data.beadBoard.map((assignment) => (
              <article className="hh-swarm-bead-card" key={assignment.beadId}>
                <code>{assignment.beadId}</code>
                <span>{assignment.agents.join(", ")}</span>
              </article>
            ))}
          </div>
        </div>

        <div className="hh-swarm-panel">
          <div className="hh-stage-section-title">
            <Users size={17} strokeWidth={2.1} />
            <h2>Agent grid</h2>
          </div>
          <div className="hh-swarm-agent-grid">
            {data.sessions.map((session) => (
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
            ))}
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
            {data.mail.messages.map((message) => (
              <article className="hh-swarm-mail-item" key={message.id}>
                <code>{message.threadId}</code>
                <strong>{message.subject}</strong>
                <span>{message.from}</span>
              </article>
            ))}
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
