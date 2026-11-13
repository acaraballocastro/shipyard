import { useMemo, useRef, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, type AppStatus } from "../api";
import { ErrorBox, RouteTag, useServers } from "../App";
import { StatusBadge, uptime } from "../components/StatusBadge";
import { Sparkline } from "../components/Sparkline";
import { statsHistory, useStats } from "../hooks/useStats";
import { useShortcuts } from "../hooks/useShortcuts";
import { OutdatedBanner } from "../components/OutdatedBanner";
import { OtherContainers } from "../components/OtherContainers";
import { HostPanel } from "../components/HostPanel";
import { memAvailable } from "../api";

type Filter = "all" | "running" | "issues";

// Running and not unhealthy; everything else (stopped, missing, unhealthy) is an issue.
function healthy(s?: AppStatus) {
  return !!s && s.running && s.health !== "unhealthy";
}

export function ServerPage() {
  const { server = "local" } = useParams();
  const navigate = useNavigate();
  const search = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");

  const servers = useServers();
  const me = servers.data?.find((s) => s.name === server);
  const info = useQuery({ queryKey: ["info", server], queryFn: () => api.info(server) });
  const apps = useQuery({ queryKey: ["apps", server], queryFn: () => api.apps(server), refetchInterval: 15000 });
  const stats = useStats(server);
  const statuses = useQueries({
    queries: (apps.data ?? []).map((a) => ({
      queryKey: ["status", server, a.name],
      queryFn: () => api.status(server, a.name),
      refetchInterval: 10000,
    })),
  });

  const shortcuts = useMemo(
    () => ({
      "/": () => search.current?.focus(),
      n: () => navigate(`/servers/${server}/deploy`),
    }),
    [navigate, server],
  );
  useShortcuts(shortcuts);

  const rows = (apps.data ?? []).map((a, i) => ({ app: a, st: statuses[i]?.data }));
  const loaded = rows.filter((r) => r.st);
  const running = loaded.filter((r) => healthy(r.st)).length;
  const issues = loaded.length - running;
  const totalCpu = Object.values(stats.data ?? {}).reduce((sum, s) => sum + s.cpu, 0);

  const q = query.trim().toLowerCase();
  const visible = rows.filter(({ app, st }) => {
    if (q && ![app.name, app.image, app.domain ?? "", app.repo ?? ""].some((v) => v.toLowerCase().includes(q))) return false;
    if (filter === "running") return healthy(st);
    if (filter === "issues") return !!st && !healthy(st);
    return true;
  });

  return (
    <>
      <header className="page-head">
        <div>
          <h1>{server === "local" ? "This machine" : server}</h1>
          {info.data && (
            <div className="muted small">
              {info.data.hostname} · {info.data.os}/{info.data.arch} · shipyard {info.data.version}{" "}
              <RouteTag routes={me?.routes} always />
            </div>
          )}
        </div>
        <Link className="button primary" to={`/servers/${server}/deploy`} title="New deployment (n)">
          + Deploy
        </Link>
      </header>

      {info.error && <ErrorBox error={info.error} />}
      <OutdatedBanner server={server} />
      <HostPanel server={server} />
      {apps.error && <ErrorBox error={apps.error} />}

      {!!apps.data?.length && (
        <div className="tiles">
          <Tile label="Services" value={String(apps.data.length)} />
          <Tile label="Healthy" value={loaded.length ? String(running) : "…"} tone="ok" />
          <Tile label="Need attention" value={loaded.length ? String(issues) : "…"} tone={issues ? "bad" : undefined} />
          <Tile label="Total CPU" value={stats.data ? `${totalCpu.toFixed(1)}%` : "…"} hint="Sum across containers" />
        </div>
      )}

      {apps.data?.length === 0 && (
        <div className="card empty-state">
          <div className="empty-icon" aria-hidden>
            ⚓
          </div>
          <h2>No services on this server yet</h2>
          <p className="muted">Deploy a Docker image or a GitHub repository. You can follow the build live.</p>
          <Link className="button primary" to={`/servers/${server}/deploy`}>
            Deploy your first service
          </Link>
        </div>
      )}

      {!!apps.data?.length && (
        <div className="card table-wrap">
          <div className="toolbar">
            <input
              ref={search}
              type="search"
              className="search"
              placeholder="Search services…  ( / )"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Search services"
            />
            <div className="segmented">
              {(["all", "running", "issues"] as const).map((f) => (
                <button key={f} type="button" className={filter === f ? "on" : ""} onClick={() => setFilter(f)}>
                  {f === "all" ? "All" : f === "running" ? "Healthy" : `Issues${issues ? ` (${issues})` : ""}`}
                </button>
              ))}
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>Service</th>
                <th>Status</th>
                <th className="hide-sm">CPU</th>
                <th className="hide-sm">Memory</th>
                <th className="hide-md">Access</th>
                <th className="hide-md">Uptime</th>
              </tr>
            </thead>
            <tbody>
              {visible.map(({ app: a, st }) => {
                const cur = stats.data?.[a.name];
                const hist = statsHistory(server, a.name);
                return (
                  <tr key={a.name} className="clickable" onClick={() => navigate(`/servers/${server}/apps/${a.name}`)}>
                    <td>
                      <Link to={`/servers/${server}/apps/${a.name}`} className="strong" onClick={(e) => e.stopPropagation()}>
                        {a.name}
                      </Link>
                      <div className="mono muted small trunc">{a.image}</div>
                    </td>
                    <td>
                      <StatusBadge status={st} />
                    </td>
                    <td className="hide-sm">
                      <Metric value={cur ? `${cur.cpu.toFixed(1)}%` : "–"}>
                        <Sparkline values={hist.map((h) => h.cpu)} floor={5} label={`${a.name} CPU`} />
                      </Metric>
                    </td>
                    <td className="hide-sm">
                      <Metric
                        value={cur ? (memAvailable(cur) ? `${cur.memPerc.toFixed(1)}%` : "n/a") : "–"}
                        title={cur && !memAvailable(cur) ? "Docker reports no memory usage (on a Raspberry Pi: enable cgroup_enable=memory)" : cur?.memUsage}
                      >
                        <Sparkline values={hist.map((h) => h.memPerc)} floor={5} label={`${a.name} memory`} />
                      </Metric>
                    </td>
                    <td className="hide-md">
                      {a.domain ? (
                        <a
                          href={`http${st?.config?.Deploy.TLS ? "s" : ""}://${a.domain}`}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                        >
                          {a.domain}
                        </a>
                      ) : (
                        <span className="mono">{st?.ports?.join(", ") || a.mode}</span>
                      )}
                    </td>
                    <td className="hide-md">{uptime(st?.startedAt)}</td>
                  </tr>
                );
              })}
              {visible.length === 0 && (
                <tr>
                  <td colSpan={6} className="muted center-cell">
                    No services match.{" "}
                    <button
                      className="link"
                      onClick={() => {
                        setQuery("");
                        setFilter("all");
                      }}
                    >
                      Clear filters
                    </button>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
      <OtherContainers server={server} query={query} />
    </>
  );
}

function Tile({ label, value, tone, hint }: { label: string; value: string; tone?: "ok" | "bad"; hint?: string }) {
  return (
    <div className="card tile" title={hint}>
      <div className="tile-label">{label}</div>
      <div className={`tile-value ${tone ?? ""}`}>{value}</div>
    </div>
  );
}

function Metric({ value, title, children }: { value: string; title?: string; children: React.ReactNode }) {
  return (
    <div className="metric" title={title}>
      <span className="metric-value">{value}</span>
      {children}
    </div>
  );
}
