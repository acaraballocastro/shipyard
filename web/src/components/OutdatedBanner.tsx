import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, REQUIRED_API, type Job } from "../api";
import { JobPanel } from "./JobPanel";
import { useToast } from "./Toast";

// Shown when a server runs a shipyard older than this UI. Servers added
// over SSH can be updated in place; others get the command to run.
export function OutdatedBanner({ server }: { server: string }) {
  const qc = useQueryClient();
  const notify = useToast();
  const [job, setJob] = useState<Job | null>(null);
  const [error, setError] = useState("");
  const info = useQuery({ queryKey: ["info", server], queryFn: () => api.info(server) });
  const servers = useQuery({ queryKey: ["servers"], queryFn: api.servers });
  const upgradable = servers.data?.find((s) => s.name === server)?.upgradable;

  const outdated = info.data && (info.data.api ?? 0) < REQUIRED_API;
  if (!outdated && !job) return null;

  async function start() {
    setError("");
    try {
      setJob(await api.upgrade(server));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  function onDone(final: Job) {
    if (final.status === "succeeded") {
      notify("ok", `${server} updated`, "Metrics, history and deploys are now available.");
      qc.invalidateQueries({ queryKey: ["info", server] });
      qc.invalidateQueries({ queryKey: ["stats", server] });
      qc.invalidateQueries({ queryKey: ["jobs", server] });
      qc.invalidateQueries({ queryKey: ["apps", server] });
    } else if (final.status === "failed") {
      notify("bad", `Updating ${server} failed`, final.error);
    }
  }

  if (job) {
    return <JobPanel key={job.id} server={server} job={job} onDone={onDone} onClose={() => setJob(null)} />;
  }

  return (
    <div className="card banner warn">
      <div>
        <div className="strong">
          {server} runs an older shipyard{info.data?.version ? ` (${info.data.version})` : ""}
        </div>
        <div className="small muted">
          Deploying from the UI, live metrics and activity history need an update.
          {!upgradable && (
            <>
              {" "}
              From your workstation, run <code>shipyard server upgrade {server}</code>.
            </>
          )}
        </div>
        {error && <div className="error small">{error}</div>}
      </div>
      {upgradable && (
        <button className="primary" onClick={start} title="Installs the latest release over SSH and restarts shipyard serve">
          Update {server}
        </button>
      )}
    </div>
  );
}
