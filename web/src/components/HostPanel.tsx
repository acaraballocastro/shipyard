import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, bytes, type Job } from "../api";
import { JobPanel } from "./JobPanel";
import { useToast } from "./Toast";

// Machine overview: CPU, memory, disk, temperature, uptime and Docker disk
// usage with a clean-up action. Hidden on servers too old to report it.
export function HostPanel({ server }: { server: string }) {
  const qc = useQueryClient();
  const notify = useToast();
  const [confirm, setConfirm] = useState(false);
  const [job, setJob] = useState<Job | null>(null);
  const { data: h, error } = useQuery({
    queryKey: ["host", server],
    queryFn: () => api.host(server),
    refetchInterval: 10000,
    retry: false,
  });
  if (error || !h) return null;

  const memPct = h.memTotal ? (100 * h.memUsed) / h.memTotal : 0;
  const disk = [...(h.disks ?? [])].sort((a, b) => b.used / b.total - a.used / a.total)[0];
  const diskPct = disk ? (100 * disk.used) / disk.total : 0;
  const reclaim = (h.docker?.imagesReclaim ?? 0) + (h.docker?.buildCacheReclaim ?? 0);

  return (
    <>
      <div className="host">
        <Gauge label="CPU" value={`${h.cpu.toFixed(0)}%`} pct={h.cpu} sub={`${h.cores} cores · load ${h.load[0].toFixed(2)}`} />
        <Gauge label="Memory" value={`${memPct.toFixed(0)}%`} pct={memPct} sub={`${bytes(h.memUsed)} / ${bytes(h.memTotal)}`} />
        {disk && (
          <Gauge
            label={`Disk ${disk.path}`}
            value={`${diskPct.toFixed(0)}%`}
            pct={diskPct}
            sub={`${bytes(disk.total - disk.used)} free`}
            title={(h.disks ?? []).map((d) => `${d.path}: ${bytes(d.used)} / ${bytes(d.total)}`).join("\n")}
          />
        )}
        {!!h.temp && (
          <Gauge label="Temperature" value={`${h.temp.toFixed(0)}°C`} pct={(h.temp / 85) * 100} sub={h.temp >= 80 ? "throttling likely" : h.temp >= 70 ? "warm" : "ok"} />
        )}
        <div className="card tile">
          <div className="tile-label">Uptime</div>
          <div className="tile-value small-value">{formatUptime(h.uptime)}</div>
          {h.docker && (
            <div className="small muted">
              Docker: {bytes(h.docker.images + h.docker.buildCache + h.docker.containers)}{" "}
              <button className="link small" onClick={() => setConfirm(true)} disabled={job?.status === "running"}>
                Clean up…
              </button>
            </div>
          )}
        </div>
      </div>

      {!h.memoryCgroup && (
        <div className="card banner warn">
          <div>
            <div className="strong">Container memory is not being measured</div>
            <div className="small muted">
              The kernel has the memory cgroup disabled (common on Raspberry Pi), so containers show memory as n/a.
              Add <code>cgroup_enable=memory cgroup_memory=1</code> to the end of the single line in{" "}
              <code>/boot/firmware/cmdline.txt</code> and reboot.
            </div>
          </div>
        </div>
      )}

      {job && (
        <JobPanel
          key={job.id}
          server={server}
          job={job}
          onClose={() => setJob(null)}
          onDone={(f) => {
            qc.invalidateQueries({ queryKey: ["host", server] });
            if (f.status === "succeeded") notify("ok", "Docker disk cleaned up");
            else if (f.status === "failed") notify("bad", "Clean up failed", f.error);
          }}
        />
      )}
      {confirm && h.docker && (
        <PruneDialog
          reclaim={reclaim}
          onCancel={() => setConfirm(false)}
          onConfirm={async (all) => {
            setConfirm(false);
            try {
              setJob(await api.prune(server, all));
            } catch (e) {
              notify("bad", "Clean up failed", e instanceof Error ? e.message : String(e));
            }
          }}
        />
      )}
    </>
  );
}

function PruneDialog({ reclaim, onCancel, onConfirm }: { reclaim: number; onCancel: () => void; onConfirm: (all: boolean) => void }) {
  const [all, setAll] = useState(false);
  return (
    <div className="modal-backdrop" onClick={onCancel}>
      <div className="card modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <h2>Clean up Docker disk</h2>
        <p className="muted">
          Removes dangling images and the build cache (about {bytes(reclaim)} reclaimable). Containers, volumes and
          networks are never touched.
        </p>
        <label className="check">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> Also remove every image no
          container uses (they will be downloaded again when needed)
        </label>
        <div className="actions end">
          <button className="secondary" onClick={onCancel}>
            Cancel
          </button>
          <button className="primary" onClick={() => onConfirm(all)}>
            Clean up
          </button>
        </div>
      </div>
    </div>
  );
}

function Gauge({ label, value, pct, sub, title }: { label: string; value: string; pct: number; sub: string; title?: string }) {
  const tone = pct >= 90 ? "bad" : pct >= 75 ? "warn" : "ok";
  return (
    <div className="card tile" title={title}>
      <div className="tile-label">{label}</div>
      <div className="tile-value small-value">{value}</div>
      <div className="bar" role="meter" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className={`bar-fill ${tone}`} style={{ width: `${Math.min(100, Math.max(0, pct))}%` }} />
      </div>
      <div className="small muted trunc">{sub}</div>
    </div>
  );
}

function formatUptime(s: number) {
  if (!s) return "–";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}
