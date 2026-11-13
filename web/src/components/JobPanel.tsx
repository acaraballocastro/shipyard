import { useEffect, useRef, useState } from "react";
import { api, type Job, type ProgressEvent } from "../api";

const MAX_OUTPUT = 3000;

// Follows a job over SSE: progress steps, raw docker/git output, final state.
// The server replays the whole history, so reconnecting loses nothing.
export function JobPanel({
  server,
  job: initial,
  onDone,
  onClose,
}: {
  server: string;
  job: Job;
  onDone?: (job: Job) => void;
  onClose: () => void;
}) {
  const [job, setJob] = useState(initial);
  const [steps, setSteps] = useState<ProgressEvent[]>([]);
  const [output, setOutput] = useState<string[]>([]);
  const [showOutput, setShowOutput] = useState(false);
  const [streamError, setStreamError] = useState("");
  const out = useRef<HTMLPreElement>(null);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;

  useEffect(() => {
    setSteps([]);
    setOutput([]);
    const es = new EventSource(api.jobEventsUrl(server, initial.id));
    es.addEventListener("progress", (e) => {
      const ev = JSON.parse((e as MessageEvent).data) as ProgressEvent;
      setSteps((s) => [...s, ev]);
    });
    es.addEventListener("output", (e) => {
      const line = (e as MessageEvent).data as string;
      setOutput((o) => (o.length >= MAX_OUTPUT ? [...o.slice(-MAX_OUTPUT + 1), line] : [...o, line]));
    });
    es.addEventListener("done", (e) => {
      const final = JSON.parse((e as MessageEvent).data) as Job;
      setJob(final);
      es.close();
      doneRef.current?.(final);
    });
    es.addEventListener("error", (e) => {
      const data = (e as MessageEvent).data;
      if (data) {
        setStreamError(data);
        es.close();
      }
      // otherwise: connection drop, EventSource reconnects and the server replays
    });
    return () => es.close();
  }, [server, initial.id]);

  useEffect(() => {
    if (out.current) out.current.scrollTop = out.current.scrollHeight;
  }, [output, showOutput]);

  const running = job.status === "running";

  return (
    <section className={`card job ${job.status}`}>
      <div className="job-head">
        <h2>
          {jobLabel(job.action)} <span className="muted">{job.app}</span>
        </h2>
        <span className={`badge ${badge(job.status)}`}>{running ? "running…" : job.status}</span>
        <div className="job-actions">
          {running ? (
            <button className="link small" onClick={() => api.cancelJob(server, job.id)}>
              Cancel
            </button>
          ) : (
            <button className="link small" onClick={onClose}>
              Close
            </button>
          )}
        </div>
      </div>

      <ol className="steps">
        {steps
          .filter((s) => s.level !== "detail")
          .map((s, i) => (
            <li key={i} className={s.level}>
              {s.stage && <span className="stage">{s.stage}</span>}
              {s.message}
            </li>
          ))}
        {running && <li className="pending">working…</li>}
      </ol>

      {job.error && job.status === "failed" && <div className="error">{job.error}</div>}
      {streamError && <div className="error">{streamError}</div>}

      {output.length > 0 && (
        <>
          <button className="link small" onClick={() => setShowOutput((v) => !v)}>
            {showOutput ? "Hide" : "Show"} output ({output.length} lines)
          </button>
          {showOutput && (
            <pre ref={out} className="logs-body job-output">
              {output.join("\n")}
            </pre>
          )}
        </>
      )}
    </section>
  );
}

export function jobLabel(a: Job["action"]) {
  return { upgrade: "Update", deploy: "Deploy", restart: "Restart", update: "Update", delete: "Delete" }[a] ?? a;
}

function badge(s: Job["status"]) {
  return s === "succeeded" ? "ok" : s === "running" ? "warn" : "bad";
}
