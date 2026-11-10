import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { ErrorBox } from "../App";
import { StatusBadge, uptime } from "../components/StatusBadge";
import { Logs } from "../components/Logs";

export function AppPage() {
  const { server = "local", app = "" } = useParams();
  const { data: s, error } = useQuery({
    queryKey: ["status", server, app],
    queryFn: () => api.status(server, app),
    refetchInterval: 5000,
  });

  const rows: [string, React.ReactNode][] = s
    ? [
        ["Image", <span className="mono">{s.image}</span>],
        ["Source", s.config?.Source.Repository ?? s.config?.Source.Type ?? "–"],
        ["Mode", s.mode],
        ...(s.domain ? [["Domain", s.domain] as [string, React.ReactNode]] : []),
        ...(s.ports?.length ? [["Ports", <span className="mono">{s.ports.join(", ")}</span>] as [string, React.ReactNode]] : []),
        ["Uptime", uptime(s.startedAt)],
        ["CPU", s.cpu ?? "–"],
        ["Memory", s.memory ?? "–"],
        ["Env keys", s.config?.Env.Keys?.join(", ") || "–"],
        ["Path", <span className="mono">{s.path}</span>],
      ]
    : [];

  return (
    <>
      <header className="page-head">
        <div>
          <div className="crumbs">
            <Link to={`/servers/${server}`}>{server === "local" ? "This machine" : server}</Link> /
          </div>
          <h1>
            {app} <StatusBadge status={s} />
          </h1>
        </div>
      </header>
      {error && <ErrorBox error={error} />}
      {s && (
        <div className="card">
          <dl className="details">
            {rows.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      )}
      <Logs server={server} app={app} />
    </>
  );
}
