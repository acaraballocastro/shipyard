import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, memAvailable, type Container } from "../api";
import { useStats } from "../hooks/useStats";

// Containers that shipyard does not manage (cloudflared, traefik, things
// started by hand…). Hidden on servers too old to list them.
export function OtherContainers({ server, query }: { server: string; query: string }) {
  const [open, setOpen] = useState(true);
  const { data, error } = useQuery({
    queryKey: ["containers", server],
    queryFn: () => api.containers(server),
    refetchInterval: 10000,
    retry: false,
  });
  const stats = useStats(server);
  if (error || !data) return null;
  const q = query.trim().toLowerCase();
  const others = data.filter(
    (c) => !c.managed && (!q || [c.name, c.image, c.project ?? ""].some((v) => v.toLowerCase().includes(q))),
  );
  if (!others.length) return null;
  const running = others.filter((c) => c.state === "running").length;

  return (
    <section className="card table-wrap">
      <button className="section-toggle" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span>{open ? "▾" : "▸"}</span>
        <h2>Other containers</h2>
        <span className="muted small">
          {running}/{others.length} running · not managed by shipyard
        </span>
      </button>
      {open && (
        <table>
          <thead>
            <tr>
              <th>Container</th>
              <th>State</th>
              <th className="hide-sm">CPU</th>
              <th className="hide-sm">Memory</th>
              <th className="hide-md">Project</th>
              <th className="hide-md">Status</th>
            </tr>
          </thead>
          <tbody>
            {others.map((c) => {
              const st = stats.data?.[c.name];
              return (
                <tr key={c.name}>
                  <td>
                    <Link to={`/servers/${server}/containers/${encodeURIComponent(c.name)}`} className="strong">
                      {c.name}
                    </Link>
                    <div className="mono muted small trunc">{c.image}</div>
                  </td>
                  <td>
                    <ContainerBadge c={c} />
                  </td>
                  <td className="hide-sm">{st ? `${st.cpu.toFixed(1)}%` : "–"}</td>
                  <td className="hide-sm" title={st?.memUsage}>
                    {st ? (memAvailable(st) ? `${st.memPerc.toFixed(1)}%` : "n/a") : "–"}
                  </td>
                  <td className="hide-md mono">{c.project ?? "–"}</td>
                  <td className="hide-md muted small">{c.status}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}

export function ContainerBadge({ c }: { c: Container }) {
  const unhealthy = c.status.includes("(unhealthy)");
  const cls = c.state === "running" ? (unhealthy ? "bad" : "ok") : c.state === "restarting" ? "warn" : "bad";
  return <span className={`badge ${cls}`}>{unhealthy ? "unhealthy" : c.state}</span>;
}
