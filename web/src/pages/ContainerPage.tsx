import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api, memAvailable, type ContainerAction } from "../api";
import { ErrorBox } from "../App";
import { Logs } from "../components/Logs";
import { ContainerBadge } from "../components/OtherContainers";
import { Sparkline } from "../components/Sparkline";
import { useToast } from "../components/Toast";
import { statsHistory, useStats } from "../hooks/useStats";

// A container shipyard does not manage: state, resources, logs and
// start/stop/restart. Its configuration stays wherever it was created.
export function ContainerPage() {
  const { server = "local", name = "" } = useParams();
  const qc = useQueryClient();
  const notify = useToast();
  const list = useQuery({
    queryKey: ["containers", server],
    queryFn: () => api.containers(server),
    refetchInterval: 5000,
  });
  const stats = useStats(server);
  const c = list.data?.find((x) => x.name === name);
  const cur = stats.data?.[name];
  const hist = statsHistory(server, name);

  const act = useMutation({
    mutationFn: (a: ContainerAction) => api.containerAction(server, name, a),
    onSuccess: (_, a) => {
      notify("ok", `${name}: ${a} done`);
      qc.invalidateQueries({ queryKey: ["containers", server] });
    },
    onError: (e, a) => notify("bad", `${name}: ${a} failed`, e.message),
  });
  const running = c?.state === "running";

  return (
    <>
      <header className="page-head">
        <div>
          <div className="crumbs">
            <Link to={`/servers/${server}`}>{server === "local" ? "This machine" : server}</Link> / containers /
          </div>
          <h1>
            {name} {c && <ContainerBadge c={c} />}
          </h1>
        </div>
        {c && (
          <div className="actions">
            {!c.managed && (
              <Link className="button secondary" to={`/servers/${server}/deploy?adopt=${encodeURIComponent(name)}`} title="Manage it with shipyard from now on">
                Adopt into shipyard…
              </Link>
            )}
            {running ? (
              <>
                <button className="secondary" disabled={act.isPending} onClick={() => act.mutate("restart")}>
                  Restart
                </button>
                <button className="danger" disabled={act.isPending} onClick={() => act.mutate("stop")}>
                  Stop
                </button>
              </>
            ) : (
              <button className="secondary" disabled={act.isPending} onClick={() => act.mutate("start")}>
                Start
              </button>
            )}
          </div>
        )}
      </header>

      {list.error && <ErrorBox error={list.error} />}
      {list.data && !c && <div className="error">Container {name} not found.</div>}
      {c?.managed && (
        <div className="card banner">
          <div>
            This container belongs to the shipyard service <Link to={`/servers/${server}/apps/${c.app}`}>{c.app}</Link>.
          </div>
        </div>
      )}

      {c && running && (
        <div className="metrics">
          <div className="card metric-card">
            <div className="tile-label">CPU</div>
            <div className="metric-big">{cur ? `${cur.cpu.toFixed(1)}%` : "…"}</div>
            <Sparkline values={hist.map((h) => h.cpu)} floor={5} height={44} label="CPU" />
          </div>
          <div className="card metric-card">
            <div className="tile-label">Memory</div>
            <div className="metric-big">{cur ? (memAvailable(cur) ? `${cur.memPerc.toFixed(1)}%` : "n/a") : "…"}</div>
            <div className="small muted trunc">{cur && memAvailable(cur) ? cur.memUsage : "not reported by Docker"}</div>
          </div>
          <div className="card metric-card">
            <div className="tile-label">Network rx / tx</div>
            <div className="metric-big">{cur?.netIO ?? "…"}</div>
          </div>
          <div className="card metric-card">
            <div className="tile-label">Disk read / write</div>
            <div className="metric-big">{cur?.blockIO ?? "…"}</div>
          </div>
        </div>
      )}

      {c && (
        <div className="card">
          <dl className="details">
            <div>
              <dt>Image</dt>
              <dd className="mono">{c.image}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{c.status}</dd>
            </div>
            <div>
              <dt>Compose project</dt>
              <dd className="mono">{c.project ?? "–"}</dd>
            </div>
            <div>
              <dt>Ports</dt>
              <dd className="mono">{c.ports || "–"}</dd>
            </div>
          </dl>
        </div>
      )}

      {c && <Logs server={server} app={name} url={api.containerLogsUrl(server, name, 300)} />}
    </>
  );
}
