import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api, type DeployRequest, type Job } from "../api";
import { JobPanel } from "../components/JobPanel";
import { useToast } from "../components/Toast";
import { OutdatedBanner } from "../components/OutdatedBanner";

// Mirrors internal/deployment and core.DeployName.
const NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,62}$/;
const REPO_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]*\/[A-Za-z0-9_.-]+$/;

function deriveName(source: "image" | "repo", image: string, repo: string): string {
  if (source === "image") {
    let n = image.trim();
    n = n.split("/").pop() ?? n;
    return n.split(":")[0].toLowerCase();
  }
  return (repo.trim().split("/").pop() ?? "").toLowerCase();
}

const lines = (s: string) =>
  s
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);

interface EnvRow {
  key: string;
  value: string;
}

// Deploy wizard. With ?name= it redeploys an existing service: every field
// left empty keeps its saved value, so only what you change is sent.
export function DeployPage() {
  const { server = "local" } = useParams();
  const [params] = useSearchParams();
  const redeploy = params.get("name") ?? "";
  const qc = useQueryClient();
  const notify = useToast();

  const [source, setSource] = useState<"image" | "repo">(params.get("repo") ? "repo" : "image");
  const [image, setImage] = useState(params.get("image") ?? "");
  const [repo, setRepo] = useState(params.get("repo") ?? "");
  const [name, setName] = useState(redeploy);
  const [mode, setMode] = useState<"" | "local" | "traefik">(redeploy ? "" : "local");
  const [ports, setPorts] = useState("");
  const [domain, setDomain] = useState("");
  const [network, setNetwork] = useState("");
  const [createNetwork, setCreateNetwork] = useState(false);
  const [internalPort, setInternalPort] = useState("");
  const [tls, setTls] = useState<"" | "on" | "off">("");
  const [env, setEnv] = useState<EnvRow[]>([]);
  const [volumes, setVolumes] = useState("");
  const [health, setHealth] = useState("");
  const [platform, setPlatform] = useState("");
  const [buildArgs, setBuildArgs] = useState("");
  const [job, setJob] = useState<Job | null>(null);

  const derived = deriveName(source, image, repo);
  const effectiveName = name.trim() || derived;

  const problems = useMemo(() => {
    const p: string[] = [];
    if (source === "image" && !image.trim()) p.push("Enter an image");
    if (source === "repo" && !REPO_RE.test(repo.trim())) p.push("Repository must be owner/name");
    if (effectiveName && !NAME_RE.test(effectiveName))
      p.push("Name: lowercase letters, digits, '.', '_' or '-' (max 63)");
    if (mode === "traefik" && !domain.trim() && !redeploy) p.push("Traefik mode needs a domain");
    if (env.some((r) => r.value && !r.key.trim())) p.push("Every env value needs a key");
    return p;
  }, [source, image, repo, effectiveName, mode, domain, env, redeploy]);

  const deploy = useMutation({
    mutationFn: (req: DeployRequest) => api.deploy(server, req),
    onSuccess: (j) => setJob(j),
  });

  function build(): DeployRequest {
    const req: DeployRequest = {};
    if (source === "image") req.image = image.trim();
    else req.repo = repo.trim();
    if (name.trim()) req.name = name.trim();
    if (mode) req.mode = mode;
    if (mode !== "traefik" && ports.trim()) req.ports = lines(ports);
    if (mode !== "local") {
      if (domain.trim()) req.domain = domain.trim();
      if (network.trim()) req.network = network.trim();
      if (createNetwork) req.createNetwork = true;
      if (internalPort.trim()) req.internalPort = internalPort.trim();
      if (tls) req.tls = tls === "on";
    }
    const envMap = Object.fromEntries(env.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.value]));
    if (Object.keys(envMap).length) req.env = envMap;
    if (volumes.trim()) req.volumes = lines(volumes);
    if (health.trim()) req.healthcheckCmd = health.trim();
    if (source === "repo") {
      if (platform) req.platform = platform;
      if (buildArgs.trim()) req.buildArgs = lines(buildArgs);
    }
    return req;
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    if (problems.length) return;
    deploy.mutate(build());
  }

  function onDone(final: Job) {
    qc.invalidateQueries({ queryKey: ["apps", server] });
    qc.invalidateQueries({ queryKey: ["jobs", server] });
    if (final.status === "succeeded") notify("ok", `${final.app} deployed`, "The service is up.");
    else if (final.status === "failed") notify("bad", `Deploy of ${final.app} failed`, final.error);
  }

  const serverLabel = server === "local" ? "This machine" : server;

  return (
    <>
      <header className="page-head">
        <div>
          <div className="crumbs">
            <Link to={`/servers/${server}`}>{serverLabel}</Link> /
          </div>
          <h1>{redeploy ? `Redeploy ${redeploy}` : "New deployment"}</h1>
          {redeploy && <div className="muted small">Empty fields keep their current value.</div>}
        </div>
      </header>

      {!job && <OutdatedBanner server={server} />}

      {job && (
        <>
          <JobPanel key={job.id} server={server} job={job} onDone={onDone} onClose={() => setJob(null)} />
          <div className="actions">
            <Link className="button secondary" to={`/servers/${server}/apps/${job.app}`}>
              Open {job.app}
            </Link>
          </div>
        </>
      )}

      {!job && (
        <form className="deploy" onSubmit={submit}>
          <Section n={1} title="Source">
            <div className="segmented" role="tablist">
              {(["image", "repo"] as const).map((s) => (
                <button
                  key={s}
                  type="button"
                  role="tab"
                  aria-selected={source === s}
                  className={source === s ? "on" : ""}
                  onClick={() => setSource(s)}
                >
                  {s === "image" ? "Docker image" : "GitHub repository"}
                </button>
              ))}
            </div>
            {source === "image" ? (
              <Field label="Image" hint="From any registry, e.g. nginx:1.27 or ghcr.io/me/api:latest">
                <input type="text" autoFocus value={image} onChange={(e) => setImage(e.target.value)} placeholder="nginx:1.27" />
              </Field>
            ) : (
              <Field label="Repository" hint="Cloned over SSH, built with buildx and pushed to ghcr.io">
                <input type="text" autoFocus value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="owner/name" />
              </Field>
            )}
            <Field label="Service name" hint={derived && !name ? `Defaults to “${derived}”` : undefined}>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={derived || "my-service"}
                readOnly={!!redeploy}
              />
            </Field>
          </Section>

          <Section n={2} title="Networking">
            <div className="segmented">
              {redeploy && (
                <button type="button" className={mode === "" ? "on" : ""} onClick={() => setMode("")}>
                  Keep current
                </button>
              )}
              <button type="button" className={mode === "local" ? "on" : ""} onClick={() => setMode("local")}>
                Local ports
              </button>
              <button type="button" className={mode === "traefik" ? "on" : ""} onClick={() => setMode("traefik")}>
                Traefik domain
              </button>
            </div>
            {mode !== "traefik" && (
              <Field label="Ports" hint="One per line, host:container">
                <textarea rows={2} value={ports} onChange={(e) => setPorts(e.target.value)} placeholder="8080:80" />
              </Field>
            )}
            {mode !== "local" && (
              <div className="grid2">
                <Field label="Domain">
                  <input type="text" value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="app.example.com" />
                </Field>
                <Field label="Container port" hint="Where the app listens">
                  <input type="text" value={internalPort} onChange={(e) => setInternalPort(e.target.value)} placeholder="80" />
                </Field>
                <Field label="Network">
                  <input type="text" value={network} onChange={(e) => setNetwork(e.target.value)} placeholder="traefik" />
                </Field>
                <Field label="TLS">
                  <select value={tls} onChange={(e) => setTls(e.target.value as typeof tls)}>
                    <option value="">{redeploy ? "Keep current" : "Default"}</option>
                    <option value="on">On</option>
                    <option value="off">Off</option>
                  </select>
                </Field>
                <label className="check">
                  <input type="checkbox" checked={createNetwork} onChange={(e) => setCreateNetwork(e.target.checked)} /> Create
                  the network if missing
                </label>
              </div>
            )}
          </Section>

          <Section n={3} title="Environment">
            {env.map((row, i) => (
              <div className="env-row" key={i}>
                <input
                  type="text"
                  className="mono"
                  value={row.key}
                  placeholder="KEY"
                  aria-label="Variable name"
                  onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, key: e.target.value } : r)))}
                />
                <input
                  type="password"
                  className="mono"
                  value={row.value}
                  placeholder="value"
                  aria-label="Variable value"
                  autoComplete="off"
                  onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                />
                <button type="button" className="link" aria-label="Remove variable" onClick={() => setEnv(env.filter((_, j) => j !== i))}>
                  ×
                </button>
              </div>
            ))}
            <button type="button" className="secondary small-btn" onClick={() => setEnv([...env, { key: "", value: "" }])}>
              + Add variable
            </button>
            <p className="muted small">
              Written to the service's <code>.env</code> (mode 0600) and merged with existing values. Never stored in
              deployment.yaml.
            </p>
          </Section>

          <details className="card advanced">
            <summary>Advanced</summary>
            <div className="advanced-body">
              <Field label="Volumes" hint="One per line, e.g. data:/var/lib/app or /srv/data:/data">
                <textarea rows={2} value={volumes} onChange={(e) => setVolumes(e.target.value)} />
              </Field>
              <Field label="Healthcheck command" hint="Run inside the container; deploy waits until it passes">
                <input type="text" className="mono" value={health} onChange={(e) => setHealth(e.target.value)} placeholder="curl -f http://localhost/ || exit 1" />
              </Field>
              {source === "repo" && (
                <div className="grid2">
                  <Field label="Build platform">
                    <select value={platform} onChange={(e) => setPlatform(e.target.value)}>
                      <option value="">{redeploy ? "Keep current" : "linux/arm64 (default)"}</option>
                      <option value="linux/arm64">linux/arm64</option>
                      <option value="linux/amd64">linux/amd64</option>
                      <option value="linux/arm/v7">linux/arm/v7</option>
                    </select>
                  </Field>
                  <Field label="Build args" hint="KEY=VALUE, one per line">
                    <textarea rows={2} className="mono" value={buildArgs} onChange={(e) => setBuildArgs(e.target.value)} />
                  </Field>
                </div>
              )}
            </div>
          </details>

          <div className="deploy-foot">
            <div className="small muted">
              {problems.length ? (
                problems[0]
              ) : (
                <>
                  Deploys <strong>{effectiveName}</strong> to {serverLabel}.
                </>
              )}
            </div>
            {deploy.error && <div className="error">{deploy.error.message}</div>}
            <button className="primary" disabled={problems.length > 0 || deploy.isPending}>
              {deploy.isPending ? "Starting…" : redeploy ? "Redeploy" : "Deploy"}
            </button>
          </div>
        </form>
      )}
    </>
  );
}

function Section({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <section className="card step">
      <h2>
        <span className="step-n">{n}</span>
        {title}
      </h2>
      {children}
    </section>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label>
      {label}
      {children}
      {hint && <span className="hint">{hint}</span>}
    </label>
  );
}
