import type { AppStatus } from "../api";

export function StatusBadge({ status }: { status?: AppStatus }) {
  if (!status) return <span className="badge muted">…</span>;
  if (!status.container) return <span className="badge bad">no container</span>;
  if (status.health === "unhealthy") return <span className="badge bad">unhealthy</span>;
  if (status.health === "starting") return <span className="badge warn">starting</span>;
  if (status.running) return <span className="badge ok">{status.health === "healthy" ? "healthy" : "running"}</span>;
  return <span className="badge bad">{status.state ?? "stopped"}</span>;
}

export function uptime(startedAt?: string): string {
  if (!startedAt) return "–";
  const s = Math.max(0, Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000));
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m`;
  return "just started";
}
