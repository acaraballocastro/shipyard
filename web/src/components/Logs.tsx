import { useEffect, useRef, useState } from "react";
import { api } from "../api";

const MAX_LINES = 5000;

// Live logs over Server-Sent Events; reconnects automatically.
export function Logs({ server, app }: { server: string; app: string }) {
  const [lines, setLines] = useState<string[]>([]);
  const [state, setState] = useState<"connecting" | "live" | "ended" | "error">("connecting");
  const [error, setError] = useState("");
  const [follow, setFollow] = useState(true);
  const box = useRef<HTMLPreElement>(null);

  useEffect(() => {
    setLines([]);
    setState("connecting");
    const es = new EventSource(api.logsUrl(server, app, 300));
    let buf: string[] = [];
    // Batch updates: busy containers can emit hundreds of lines per second.
    const flush = setInterval(() => {
      if (!buf.length) return;
      const chunk = buf;
      buf = [];
      setLines((prev) => {
        const next = prev.concat(chunk);
        return next.length > MAX_LINES ? next.slice(-MAX_LINES) : next;
      });
    }, 150);
    es.onopen = () => setState("live");
    es.onmessage = (e) => buf.push(e.data);
    es.addEventListener("error", (e) => {
      const data = (e as MessageEvent).data;
      if (data) {
        setError(data);
        setState("error");
        es.close();
      } else {
        setState("connecting"); // network drop: EventSource retries
      }
    });
    es.addEventListener("end", () => {
      setState("ended");
      es.close();
    });
    return () => {
      clearInterval(flush);
      es.close();
    };
  }, [server, app]);

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [lines, follow]);

  return (
    <section className="card logs">
      <div className="logs-head">
        <h2>Logs</h2>
        <span className={`live ${state}`}>{state}</span>
        <label className="small">
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
        </label>
        <button className="link small" onClick={() => setLines([])}>
          Clear
        </button>
      </div>
      {error && <div className="error">{error}</div>}
      <pre
        ref={box}
        className="logs-body"
        onScroll={(e) => {
          const el = e.currentTarget;
          setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
        }}
      >
        {lines.length ? lines.join("\n") : <span className="muted">Waiting for output…</span>}
      </pre>
    </section>
  );
}
