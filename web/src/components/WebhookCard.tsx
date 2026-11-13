import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type WebhookInfo } from "../api";
import { useToast } from "./Toast";

// Auto-deploy on git push (repository services). The secret is shown once,
// right after enabling or rotating; GitHub needs it to sign deliveries.
export function WebhookCard({ server, app }: { server: string; app: string }) {
  const qc = useQueryClient();
  const notify = useToast();
  const key = ["webhook", server, app];
  const { data, error } = useQuery({ queryKey: key, queryFn: () => api.webhook(server, app), retry: false, refetchInterval: 30000 });
  const [branch, setBranch] = useState("");
  const [fresh, setFresh] = useState<WebhookInfo | null>(null); // carries the secret

  const save = useMutation({
    mutationFn: (v: { branch: string; rotate?: boolean }) => api.setWebhook(server, app, v.branch, v.rotate),
    onSuccess: (w) => {
      if (w.secret) setFresh(w);
      qc.setQueryData(key, { ...w, secret: undefined });
    },
    onError: (e) => notify("bad", "Webhook not saved", e.message),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteWebhook(server, app),
    onSuccess: () => {
      setFresh(null);
      qc.setQueryData(key, { enabled: false });
      notify("info", "Auto-deploy disabled");
    },
  });

  if (error) {
    // Older server, or `shipyard ui` serving this machine (no webhooks there).
    return null;
  }
  if (!data) return null;
  const url = (fresh ?? data).url;
  const path = (fresh ?? data).path;

  return (
    <section className="card webhook">
      <div className="config-head">
        <h2>Auto-deploy on push</h2>
        {data.enabled && <span className="badge ok">on · {data.branch}</span>}
      </div>

      {!data.enabled && (
        <>
          <p className="muted small">
            Redeploys this service when you push to a branch on GitHub. Shipyard verifies GitHub's signature on every
            delivery.
          </p>
          <div className="inline-form">
            <input type="text" value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="main" aria-label="Branch" />
            <button className="primary" disabled={save.isPending} onClick={() => save.mutate({ branch: branch || "main" })}>
              Enable
            </button>
          </div>
        </>
      )}

      {fresh?.secret && (
        <div className="secret-box">
          <div className="strong">Add this webhook on GitHub (the secret is shown only now)</div>
          <ol className="small">
            <li>Repository → Settings → Webhooks → Add webhook</li>
            <li>
              Payload URL: <Copy text={url ?? `https://<your-hostname>${path}`} />
            </li>
            <li>
              Content type: <code>application/json</code>
            </li>
            <li>
              Secret: <Copy text={fresh.secret} />
            </li>
            <li>Events: “Just the push event”</li>
          </ol>
          {!url && (
            <p className="small muted">
              Use the public hostname that reaches shipyard serve (e.g. your Cloudflare Tunnel hostname). Set it with{" "}
              <code>shipyard serve cloudflare --hostname …</code> to see the full URL here.
            </p>
          )}
          <p className="small muted">
            Behind Cloudflare Access, add a <strong>Bypass</strong> policy for the path <code>/hooks/</code> so GitHub
            can reach it (the signature protects it).
          </p>
          <button className="link small" onClick={() => setFresh(null)}>
            I saved it
          </button>
        </div>
      )}

      {data.enabled && (
        <>
          <dl className="config-group plain">
            <div className="config-row">
              <dt>Payload URL</dt>
              <dd className="mono">{url ?? path}</dd>
            </div>
            <div className="config-row">
              <dt>Last delivery</dt>
              <dd>
                {data.last ? (
                  <>
                    <span className={`badge ${data.last.result === "deployed" ? "ok" : data.last.result === "ignored" ? "" : "bad"}`}>
                      {data.last.result}
                    </span>{" "}
                    <span className="small muted">
                      {data.last.event}
                      {data.last.commit && ` ${data.last.commit}`} · {new Date(data.last.at).toLocaleString()}
                      {data.last.detail && ` · ${data.last.detail}`}
                    </span>
                  </>
                ) : (
                  <span className="muted small">none yet (GitHub sends a ping when you add it)</span>
                )}
              </dd>
            </div>
          </dl>
          <div className="actions">
            <input
              type="text"
              className="branch-input"
              defaultValue={data.branch}
              aria-label="Branch"
              onBlur={(e) => e.target.value && e.target.value !== data.branch && save.mutate({ branch: e.target.value })}
            />
            <button className="secondary" disabled={save.isPending} onClick={() => save.mutate({ branch: data.branch ?? "main", rotate: true })}>
              Rotate secret
            </button>
            <button className="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
              Disable
            </button>
          </div>
        </>
      )}
    </section>
  );
}

function Copy({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <span className="copy">
      <code>{text}</code>
      <button
        className="link small"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setDone(true);
            setTimeout(() => setDone(false), 1500);
          } catch {
            /* clipboard blocked: the text is selectable */
          }
        }}
      >
        {done ? "copied" : "copy"}
      </button>
    </span>
  );
}
