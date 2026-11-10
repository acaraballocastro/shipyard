import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api";
import { ErrorBox } from "../App";
import { StatusBadge, uptime } from "../components/StatusBadge";

export function ServerPage() {
  const { server = "local" } = useParams();
  const info = useQuery({ queryKey: ["info", server], queryFn: () => api.info(server) });
  const apps = useQuery({ queryKey: ["apps", server], queryFn: () => api.apps(server), refetchInterval: 15000 });
  const statuses = useQueries({
    queries: (apps.data ?? []).map((a) => ({
      queryKey: ["status", server, a.name],
      queryFn: () => api.status(server, a.name),
      refetchInterval: 10000,
    })),
  });

  return (
    <>
      <header className="page-head">
        <div>
          <h1>{server === "local" ? "This machine" : server}</h1>
          {info.data && (
            <div className="muted small">
              {info.data.hostname} · {info.data.os}/{info.data.arch} · shipyard {info.data.version}
            </div>
          )}
        </div>
      </header>

      {info.error && <ErrorBox error={info.error} />}
      {apps.error && <ErrorBox error={apps.error} />}
      {apps.data?.length === 0 && (
        <div className="card empty">
          No services yet. Deploy one with <code>shipyard deploy</code>.
        </div>
      )}

      {!!apps.data?.length && (
        <div className="card table-wrap">
          <table>
            <thead>
              <tr>
                <th>Service</th>
                <th>Status</th>
                <th className="hide-sm">Image</th>
                <th className="hide-sm">Access</th>
                <th className="hide-sm">Uptime</th>
              </tr>
            </thead>
            <tbody>
              {apps.data.map((a, i) => {
                const st = statuses[i]?.data;
                return (
                  <tr key={a.name}>
                    <td>
                      <Link to={`/servers/${server}/apps/${a.name}`} className="strong">
                        {a.name}
                      </Link>
                    </td>
                    <td>
                      <StatusBadge status={st} />
                    </td>
                    <td className="hide-sm mono">{a.image}</td>
                    <td className="hide-sm">
                      {a.domain ? (
                        <a href={`http${st?.config?.Deploy.TLS ? "s" : ""}://${a.domain}`} target="_blank" rel="noreferrer">
                          {a.domain}
                        </a>
                      ) : (
                        <span className="mono">{st?.ports?.join(", ") || a.mode}</span>
                      )}
                    </td>
                    <td className="hide-sm">{uptime(st?.startedAt)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
