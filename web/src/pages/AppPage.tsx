import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, memAvailable, type AppStatus, type Job } from "../api";
import { Actions } from "../components/Actions";
import { JobPanel, jobLabel } from "../components/JobPanel";
import { ErrorBox } from "../App";
import { StatusBadge, uptime } from "../components/StatusBadge";
import { Logs } from "../components/Logs";
import { Sparkline } from "../components/Sparkline";
import { useToast } from "../components/Toast";
import { statsHistory, useStats } from "../hooks/useStats";

export function AppPage() {
  const { server = "local", app = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const notify = useToast();
  const [job, setJob] = useState<Job | null>(null);
  const [deleted, setDeleted] = useState(false);
  const { data: s, error } = useQuery({
    queryKey: ["status", server, app],
    queryFn: () => api.status(server, app),
    refetchInterval: 5000,
    enabled: !deleted,
  });

  // Reattach to a job already running for this app (page reload, or one
  // started from another tab or the deploy page).
  const jobs = useQuery({
    queryKey: ["jobs", server, app],
    queryFn: () => api.jobs(server, app),
    refetchInterval: 15000,
    retry: false,
    enabled: !deleted,
  });
  const runningJob = jobs.data?.find((j) => j.status === "running");
  useEffect(() => {
    if (runningJob && !job) setJob(runningJob);
  }, [runningJob, job]);

  function onJobDone(final: Job) {
    qc.invalidateQueries({ queryKey: ["apps", server] });
    qc.invalidateQueries({ queryKey: ["jobs", server, app] });
    const what = `${jobLabel(final.action)} ${final.app}`;
    if (final.status === "succeeded") notify("ok", `${what}: done`);
    else if (final.status === "failed") notify("bad", `${what}: failed`, final.error);
    else if (final.status === "canceled") notify("info", `${what}: canceled`);
    if (final.action === "delete" && final.status === "succeeded") {
      setDeleted(true);
      setTimeout(() => navigate(`/servers/${server}`), 1200);
      return;
    }
    qc.invalidateQueries({ queryKey: ["status", server, app] });
  }

  const rows: [string, React.ReactNode][] = s
    ? [
        ["Image", <span className="mono">{s.image}</span>],
        ["Source", s.config?.Source.Repository ?? s.config?.Source.Type ?? "–"],
        ["Mode", s.mode],
        ...(s.domain ? [["Domain", s.domain] as [string, React.ReactNode]] : []),
        ...(s.ports?.length ? [["Ports", <span className="mono">{s.ports.join(", ")}</span>] as [string, React.ReactNode]] : []),
        ["Uptime", uptime(s.startedAt)],
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
            {app} {!deleted && <StatusBadge status={s} />}
          </h1>
        </div>
        {s && !deleted && (
          <div className="actions">
            <Link
              className={`button secondary ${job?.status === "running" ? "disabled" : ""}`}
              to={redeployUrl(server, s)}
              title="Change settings and redeploy"
            >
              Redeploy…
            </Link>
            <Actions server={server} app={app} busy={job?.status === "running"} onStarted={setJob} />
          </div>
        )}
      </header>
      {job && <JobPanel key={job.id} server={server} job={job} onDone={onJobDone} onClose={() => setJob(null)} />}
      {error && !deleted && <ErrorBox error={error} />}
      {s && !deleted && <Metrics server={server} app={app} running={s.running} />}
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
      {!deleted && <Activity server={server} app={app} current={job?.id} onOpen={setJob} />}
      {!deleted && <Logs server={server} app={app} />}
    </>
  );
}

function redeployUrl(server: string, s: AppStatus) {
  const q = new URLSearchParams({ name: s.name });
  if (s.repo) q.set("repo", s.repo);
  else q.set("image", s.image);
  return `/servers/${server}/deploy?${q}`;
}

function Metrics({ server, app, running }: { server: string; app: string; running: boolean }) {
  const { data, error } = useStats(server);
  const cur = data?.[app];
  const hist = statsHistory(server, app);
  if (error) return null; // older servers have no /stats; the details card still shows the basics
  if (!running) return <div className="card muted small">Not running: no live metrics.</div>;
  return (
    <div className="metrics">
      <MetricCard label="CPU" value={cur ? `${cur.cpu.toFixed(1)}%` : "…"} sub="of one core">
        <Sparkline values={hist.map((h) => h.cpu)} floor={5} height={44} label="CPU over the last minutes" />
      </MetricCard>
      <MetricCard
        label="Memory"
        value={cur ? (memAvailable(cur) ? `${cur.memPerc.toFixed(1)}%` : "n/a") : "…"}
        sub={cur ? (memAvailable(cur) ? cur.memUsage : "not reported by Docker (cgroup memory disabled?)") : ""}
      >
        <Sparkline values={hist.map((h) => h.memPerc)} floor={5} height={44} label="Memory over the last minutes" />
      </MetricCard>
      <MetricCard label="Network rx / tx" value={cur?.netIO ?? "…"} sub="since start" />
      <MetricCard label="Disk read / write" value={cur?.blockIO ?? "…"} sub={cur ? `${cur.pids} processes` : ""} />
    </div>
  );
}

function MetricCard({ label, value, sub, children }: { label: string; value: string; sub: string; children?: React.ReactNode }) {
  return (
    <div className="card metric-card">
      <div className="tile-label">{label}</div>
      <div className="metric-big">{value}</div>
      <div className="small muted trunc">{sub || " "}</div>
      {children}
    </div>
  );
}

function Activity({
  server,
  app,
  current,
  onOpen,
}: {
  server: string;
  app: string;
  current?: string;
  onOpen: (j: Job) => void;
}) {
  const { data, error } = useQuery({
    queryKey: ["jobs", server, app],
    queryFn: () => api.jobs(server, app),
    refetchInterval: 15000,
    retry: false,
  });
  if (error || !data) return null;
  return (
    <section className="card activity">
      <h2>Activity</h2>
      {data.length === 0 ? (
        <p className="muted small">No deploys or actions yet.</p>
      ) : (
        <ol className="timeline">
          {data.slice(0, 10).map((j) => (
            <li key={j.id} className={j.status}>
              <button className="link timeline-item" onClick={() => onOpen(j)} disabled={j.id === current}>
                <span className="strong">{jobLabel(j.action)}</span>
                <span className={`badge ${j.status === "succeeded" ? "ok" : j.status === "running" ? "warn" : "bad"}`}>
                  {j.status}
                </span>
                <span className="muted small" title={new Date(j.startedAt).toLocaleString()}>
                  {ago(j.startedAt)}
                  {j.finishedAt && ` · ${duration(j.startedAt, j.finishedAt)}`}
                </span>
              </button>
              {j.error && j.status === "failed" && <div className="small bad-text trunc">{j.error}</div>}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function ago(iso: string) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

function duration(a: string, b: string) {
  const s = Math.round((new Date(b).getTime() - new Date(a).getTime()) / 1000);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}
