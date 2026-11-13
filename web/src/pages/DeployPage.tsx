import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { api, type DeployRequest, type DeploymentConfig, type Job } from "../api";
import { JobPanel } from "../components/JobPanel";
import { useToast } from "../components/Toast";
import { OutdatedBanner } from "../components/OutdatedBanner";
import { ErrorBox } from "../App";

// Mirrors internal/deployment and core.DeployName.
const NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,62}$/;
const REPO_RE = /^[A-Za-z0-9][A-Za-z0-9_.-]*\/[A-Za-z0-9_.-]+$/;
const KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

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
const join = (xs?: string[] | null) => (xs ?? []).join("\n");

interface EnvRow {
  key: string;
  value: string;
}

// Form state; strings so inputs stay controlled.
interface Form {
  source: "image" | "repo";
  image: string;
  repo: string;
  name: string;
  mode: "local" | "traefik";
  ports: string;
  domain: string;
  network: string;
  createNetwork: boolean;
  internalPort: string;
  entrypoint: string;
  tls: boolean;
  certResolver: string;
  routerName: string;
  middlewares: string;
  volumes: string;
  command: string;
  envFiles: string;
  health: string;
  healthInterval: string;
  healthTimeout: string;
  healthRetries: string;
  healthStart: string;
  platform: string;
  buildArgs: string;
}

const EMPTY: Form = {
  source: "image",
  image: "",
  repo: "",
  name: "",
  mode: "local",
  ports: "",
  domain: "",
  network: "",
  createNetwork: false,
  internalPort: "",
  entrypoint: "",
  tls: false,
  certResolver: "",
  routerName: "",
  middlewares: "",
  volumes: "",
  command: "",
  envFiles: "",
  health: "",
  healthInterval: "",
  healthTimeout: "",
  healthRetries: "",
  healthStart: "",
  platform: "",
  buildArgs: "",
};

// fromConfig turns a saved deployment.yaml into form values.
function fromConfig(name: string, image: string, c: DeploymentConfig): Form {
  const git = c.Source.Type === "git";
  const args = [
    ...Object.entries(c.Build.Args ?? {}).map(([k, v]) => `${k}=${v}`),
    ...(c.Build.ArgsFromEnv ?? []),
  ];
  return {
    ...EMPTY,
    source: git ? "repo" : "image",
    image: git ? "" : c.Image || image,
    repo: c.Source.Repository ?? "",
    name,
    mode: c.Deploy.Mode === "traefik" ? "traefik" : "local",
    ports: join(c.Deploy.Ports),
    domain: c.Deploy.Domain ?? "",
    network: c.Deploy.Network ?? "",
    internalPort: c.Deploy.InternalPort ?? "",
    entrypoint: c.Deploy.Entrypoint ?? "",
    tls: !!c.Deploy.TLS,
    certResolver: c.Deploy.CertResolver ?? "",
    routerName: c.Deploy.RouterName ?? "",
    middlewares: join(c.Deploy.Middlewares),
    volumes: join(c.Deploy.Volumes),
    command: join(c.Deploy.Command),
    envFiles: join(c.Env.Files),
    health: c.Healthcheck.Enabled ? c.Healthcheck.Command ?? "" : "",
    healthInterval: c.Healthcheck.Interval ?? "",
    healthTimeout: c.Healthcheck.Timeout ?? "",
    healthRetries: c.Healthcheck.Retries ? String(c.Healthcheck.Retries) : "",
    healthStart: c.Healthcheck.StartPeriod ?? "",
    platform: c.Build.Platform ?? "",
    buildArgs: args.join("\n"),
  };
}

// fromRequest turns a deploy request (an adoption plan) into form values.
function fromRequest(r: DeployRequest): Form {
  return {
    ...EMPTY,
    source: r.repo ? "repo" : "image",
    image: r.image ?? "",
    repo: r.repo ?? "",
    name: r.name ?? "",
    mode: r.mode === "traefik" ? "traefik" : "local",
    ports: join(r.ports),
    domain: r.domain ?? "",
    network: r.network ?? "",
    internalPort: r.internalPort ?? "",
    entrypoint: r.entrypoint ?? "",
    tls: !!r.tls,
    certResolver: r.certResolver ?? "",
    volumes: join(r.volumes),
    command: join(r.command),
    health: r.healthcheckCmd ?? "",
    healthInterval: r.healthcheckInterval ?? "",
    healthTimeout: r.healthcheckTimeout ?? "",
    healthRetries: r.healthcheckRetries ? String(r.healthcheckRetries) : "",
    healthStart: r.healthcheckStartPeriod ?? "",
  };
}

// Deploy wizard. With ?adopt=<container> it turns an existing container
// into a service, prefilled from its current settings. With ?name= it redeploys an existing service, starting
// from its saved settings and env values: what you see is what is applied.
export function DeployPage() {
  const { server = "local" } = useParams();
  const [params] = useSearchParams();
  const redeploy = params.get("name") ?? "";
  const adopt = params.get("adopt") ?? "";
  const qc = useQueryClient();
  const notify = useToast();

  const [f, setF] = useState<Form>(() => ({
    ...EMPTY,
    source: params.get("repo") ? "repo" : "image",
    image: params.get("image") ?? "",
    repo: params.get("repo") ?? "",
    name: redeploy,
  }));
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((x) => ({ ...x, [k]: v }));
  const [env, setEnv] = useState<EnvRow[]>([]);
  const [initialEnv, setInitialEnv] = useState<Record<string, string>>({});
  const [reveal, setReveal] = useState(false);
  const [job, setJob] = useState<Job | null>(null);

  // Redeploy: load the current configuration and env values once.
  const status = useQuery({
    queryKey: ["status", server, redeploy],
    queryFn: () => api.status(server, redeploy),
    enabled: !!redeploy,
    staleTime: Infinity,
  });
  const envQ = useQuery({
    queryKey: ["env", server, redeploy],
    queryFn: () => api.env(server, redeploy),
    enabled: !!redeploy,
    staleTime: Infinity,
    gcTime: 0, // secrets: don't keep them cached after leaving
    retry: false,
  });
  const plan = useQuery({
    queryKey: ["adopt", server, adopt],
    queryFn: () => api.adoptPlan(server, adopt),
    enabled: !!adopt,
    staleTime: Infinity,
    gcTime: 0, // carries env values
    retry: false,
  });
  const [loaded, setLoaded] = useState(!redeploy && !adopt);
  useEffect(() => {
    if (loaded || !plan.data) return;
    setF(fromRequest(plan.data.request));
    setEnv(Object.entries(plan.data.request.env ?? {}).map(([key, value]) => ({ key, value })));
    setLoaded(true);
  }, [loaded, plan.data]);
  useEffect(() => {
    if (loaded || !status.data || (!envQ.data && !envQ.error)) return;
    if (status.data.config) setF(fromConfig(redeploy, status.data.image, status.data.config));
    const vars = envQ.data ?? {};
    setInitialEnv(vars);
    setEnv(Object.entries(vars).map(([key, value]) => ({ key, value })));
    setLoaded(true);
  }, [loaded, status.data, envQ.data, envQ.error, redeploy]);

  const derived = deriveName(f.source, f.image, f.repo);
  const effectiveName = f.name.trim() || derived;

  const problems = useMemo(() => {
    const p: string[] = [];
    if (f.source === "image" && !f.image.trim()) p.push("Enter an image");
    if (f.source === "repo" && !REPO_RE.test(f.repo.trim())) p.push("Repository must be owner/name");
    if (effectiveName && !NAME_RE.test(effectiveName)) p.push("Name: lowercase letters, digits, '.', '_' or '-' (max 63)");
    if (f.mode === "traefik" && !f.domain.trim()) p.push("Traefik mode needs a domain");
    if (env.some((r) => r.key.trim() && !KEY_RE.test(r.key.trim()))) p.push("Env names: letters, digits and _ (not starting with a digit)");
    if (env.some((r) => r.value && !r.key.trim())) p.push("Every env value needs a key");
    if (f.healthRetries && !/^\d+$/.test(f.healthRetries)) p.push("Healthcheck retries must be a number");
    if (adopt && f.source !== "image") p.push("Adoption keeps the container's image");
    for (const b of plan.data?.blockers ?? []) p.push(`Cannot adopt: ${b}`);
    return p;
  }, [f, effectiveName, env, adopt, plan.data]);

  const deploy = useMutation({
    mutationFn: (req: DeployRequest) => (adopt ? api.adopt(server, adopt, req) : api.deploy(server, req)),
    onSuccess: (j) => setJob(j),
  });

  function build(): DeployRequest {
    // A redeploy sends every field (they were prefilled), so clearing one
    // clears it. A new deployment omits empty fields to get the defaults.
    const full = !!redeploy;
    const req: DeployRequest = {};
    const str = (k: keyof DeployRequest, v: string) => {
      if (full || v.trim()) (req as Record<string, unknown>)[k] = v.trim();
    };
    const list = (k: keyof DeployRequest, v: string) => {
      if (full || v.trim()) (req as Record<string, unknown>)[k] = lines(v);
    };

    if (f.source === "image") req.image = f.image.trim();
    else req.repo = f.repo.trim();
    if (f.name.trim()) req.name = f.name.trim();
    req.mode = f.mode;
    if (f.mode === "local") {
      list("ports", f.ports);
    } else {
      str("domain", f.domain);
      str("network", f.network);
      str("internalPort", f.internalPort);
      str("entrypoint", f.entrypoint);
      str("certResolver", f.certResolver);
      str("routerName", f.routerName);
      list("middlewares", f.middlewares);
      if (full || f.tls) req.tls = f.tls;
      if (f.createNetwork) req.createNetwork = true;
    }
    list("volumes", f.volumes);
    list("command", f.command);
    list("envFiles", f.envFiles);

    if (f.health.trim()) {
      req.healthcheckCmd = f.health.trim();
      str("healthcheckInterval", f.healthInterval);
      str("healthcheckTimeout", f.healthTimeout);
      str("healthcheckStartPeriod", f.healthStart);
      if (f.healthRetries) req.healthcheckRetries = Number(f.healthRetries);
    } else if (full) {
      req.noHealthcheck = true;
    }

    if (f.source === "repo") {
      str("platform", f.platform);
      list("buildArgs", f.buildArgs);
    }

    // Env: send what changed; removed rows are unset.
    const now: Record<string, string> = {};
    for (const r of env) if (r.key.trim()) now[r.key.trim()] = r.value;
    const changed = Object.fromEntries(Object.entries(now).filter(([k, v]) => initialEnv[k] !== v));
    const removed = Object.keys(initialEnv).filter((k) => !(k in now));
    if (Object.keys(changed).length) req.env = changed;
    if (removed.length) req.unsetEnv = removed;
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
    qc.invalidateQueries({ queryKey: ["status", server, final.app] });
    qc.removeQueries({ queryKey: ["env", server, final.app] });
    qc.invalidateQueries({ queryKey: ["containers", server] });
    if (final.status === "succeeded")
      notify("ok", adopt ? `${adopt} adopted` : `${final.app} deployed`, adopt ? `Now managed as ${final.app}.` : "The service is up.");
    else if (final.status === "failed") notify("bad", `Deploy of ${final.app} failed`, final.error);
  }

  const serverLabel = server === "local" ? "This machine" : server;
  const isRepo = f.source === "repo";

  return (
    <>
      <header className="page-head">
        <div>
          <div className="crumbs">
            <Link to={`/servers/${server}`}>{serverLabel}</Link> /
            {redeploy && (
              <>
                {" "}
                <Link to={`/servers/${server}/apps/${redeploy}`}>{redeploy}</Link> /
              </>
            )}
          </div>
          <h1>{adopt ? `Adopt ${adopt}` : redeploy ? `Redeploy ${redeploy}` : "New deployment"}</h1>
          {redeploy && <div className="muted small">Loaded from the current deployment. Change what you need.</div>}
          {adopt && (
            <div className="muted small">
              Prefilled from the running container. The original is stopped and kept until the new service is up; if
              anything fails it is restored as it was.
            </div>
          )}
        </div>
      </header>

      {!job && <OutdatedBanner server={server} />}
      {status.error && <ErrorBox error={status.error} />}
      {plan.error && <ErrorBox error={plan.error} />}
      {!job && !!plan.data?.warnings.length && (
        <div className="card banner warn">
          <div>
            <div className="strong">Differences after adoption</div>
            <ul className="small warn-list">
              {plan.data.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </div>
        </div>
      )}
      {envQ.error && (
        <div className="card banner warn">
          <div>
            <div className="strong">Could not load the current env values</div>
            <div className="small muted">
              {envQ.error.message}. Existing variables are kept unless you add one with the same name.
            </div>
          </div>
        </div>
      )}

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

      {!job && !loaded && <div className="card muted">Loading current settings…</div>}

      {!job && loaded && (
        <form className="deploy" onSubmit={submit}>
          <Section n={1} title="Source">
            <div className="segmented" role="tablist">
              {(["image", "repo"] as const).map((s) => (
                <button
                  key={s}
                  type="button"
                  role="tab"
                  aria-selected={f.source === s}
                  className={f.source === s ? "on" : ""}
                  onClick={() => set("source", s)}
                >
                  {s === "image" ? "Docker image" : "GitHub repository"}
                </button>
              ))}
            </div>
            {f.source === "image" ? (
              <Field label="Image" hint="From any registry, e.g. nginx:1.27 or ghcr.io/me/api:latest">
                <input type="text" autoFocus value={f.image} onChange={(e) => set("image", e.target.value)} placeholder="nginx:1.27" />
              </Field>
            ) : (
              <Field label="Repository" hint="Cloned over SSH, built with buildx and pushed to ghcr.io">
                <input type="text" autoFocus value={f.repo} onChange={(e) => set("repo", e.target.value)} placeholder="owner/name" />
              </Field>
            )}
            <Field label="Service name" hint={derived && !f.name ? `Defaults to “${derived}”` : undefined}>
              <input
                type="text"
                value={f.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder={derived || "my-service"}
                readOnly={!!redeploy}
              />
            </Field>
          </Section>

          <Section n={2} title="Networking">
            <div className="segmented">
              <button type="button" className={f.mode === "local" ? "on" : ""} onClick={() => set("mode", "local")}>
                Local ports
              </button>
              <button type="button" className={f.mode === "traefik" ? "on" : ""} onClick={() => set("mode", "traefik")}>
                Traefik domain
              </button>
            </div>
            {f.mode === "local" ? (
              <Field label="Ports" hint="One per line, host:container">
                <textarea rows={2} className="mono" value={f.ports} onChange={(e) => set("ports", e.target.value)} placeholder="8080:80" />
              </Field>
            ) : (
              <div className="grid2">
                <Field label="Domain">
                  <input type="text" value={f.domain} onChange={(e) => set("domain", e.target.value)} placeholder="app.example.com" />
                </Field>
                <Field label="Container port" hint="Where the app listens">
                  <input type="text" value={f.internalPort} onChange={(e) => set("internalPort", e.target.value)} placeholder="80" />
                </Field>
                <Field label="Network">
                  <input type="text" value={f.network} onChange={(e) => set("network", e.target.value)} placeholder="edge" />
                </Field>
                <Field label="Entrypoint">
                  <input type="text" value={f.entrypoint} onChange={(e) => set("entrypoint", e.target.value)} placeholder="web" />
                </Field>
                <label className="check">
                  <input type="checkbox" checked={f.tls} onChange={(e) => set("tls", e.target.checked)} /> TLS
                </label>
                <label className="check">
                  <input type="checkbox" checked={f.createNetwork} onChange={(e) => set("createNetwork", e.target.checked)} />{" "}
                  Create the network if missing
                </label>
              </div>
            )}
          </Section>

          <Section n={3} title="Environment">
            {env.length > 0 && (
              <button type="button" className="link small reveal" onClick={() => setReveal((r) => !r)}>
                {reveal ? "Hide values" : "Show values"}
              </button>
            )}
            {env.map((row, i) => {
              const isNew = !(row.key in initialEnv);
              const edited = !isNew && initialEnv[row.key] !== row.value;
              return (
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
                    type={reveal ? "text" : "password"}
                    className="mono"
                    value={row.value}
                    placeholder="value"
                    aria-label={`Value of ${row.key || "variable"}`}
                    autoComplete="off"
                    onChange={(e) => setEnv(env.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)))}
                  />
                  <span className="env-state small muted">{redeploy && row.key ? (isNew ? "new" : edited ? "edited" : "") : ""}</span>
                  <button type="button" className="link" aria-label="Remove variable" onClick={() => setEnv(env.filter((_, j) => j !== i))}>
                    ×
                  </button>
                </div>
              );
            })}
            {redeploy &&
              Object.keys(initialEnv)
                .filter((k) => !env.some((r) => r.key.trim() === k))
                .map((k) => (
                  <div key={k} className="small muted">
                    <span className="mono strike">{k}</span> will be removed{" "}
                    <button type="button" className="link small" onClick={() => setEnv([...env, { key: k, value: initialEnv[k] }])}>
                      undo
                    </button>
                  </div>
                ))}
            <button type="button" className="secondary small-btn" onClick={() => setEnv([...env, { key: "", value: "" }])}>
              + Add variable
            </button>
            <Field label="Env files" hint="Paths on the server, one per line (their values are not shown here)">
              <textarea rows={2} className="mono" value={f.envFiles} onChange={(e) => set("envFiles", e.target.value)} />
            </Field>
            <p className="muted small">
              Variables are written to the service's <code>.env</code> (mode 0600), never to deployment.yaml.
            </p>
          </Section>

          <details className="card advanced" open={(!!redeploy || !!adopt) && !!(f.volumes || f.health || f.buildArgs || f.middlewares || f.command)}>
            <summary>Advanced</summary>
            <div className="advanced-body">
              <Field label="Volumes" hint="One per line, e.g. data:/var/lib/app or /srv/data:/data">
                <textarea rows={2} className="mono" value={f.volumes} onChange={(e) => set("volumes", e.target.value)} />
              </Field>
              <Field label="Command" hint="Overrides the image's command; one argument per line (e.g. tunnel / run)">
                <textarea rows={2} className="mono" value={f.command} onChange={(e) => set("command", e.target.value)} />
              </Field>
              <Field label="Healthcheck command" hint="Run inside the container; deploy waits until it passes. Empty: no healthcheck">
                <input type="text" className="mono" value={f.health} onChange={(e) => set("health", e.target.value)} placeholder="curl -f http://localhost/ || exit 1" />
              </Field>
              {f.health.trim() && (
                <div className="grid4">
                  <Field label="Interval">
                    <input type="text" value={f.healthInterval} onChange={(e) => set("healthInterval", e.target.value)} placeholder="30s" />
                  </Field>
                  <Field label="Timeout">
                    <input type="text" value={f.healthTimeout} onChange={(e) => set("healthTimeout", e.target.value)} placeholder="5s" />
                  </Field>
                  <Field label="Retries">
                    <input type="text" value={f.healthRetries} onChange={(e) => set("healthRetries", e.target.value)} placeholder="3" />
                  </Field>
                  <Field label="Start period">
                    <input type="text" value={f.healthStart} onChange={(e) => set("healthStart", e.target.value)} placeholder="10s" />
                  </Field>
                </div>
              )}
              {f.mode === "traefik" && (
                <div className="grid2">
                  <Field label="Cert resolver">
                    <input type="text" value={f.certResolver} onChange={(e) => set("certResolver", e.target.value)} placeholder="letsencrypt" />
                  </Field>
                  <Field label="Router name" hint="Defaults to the service name">
                    <input type="text" value={f.routerName} onChange={(e) => set("routerName", e.target.value)} />
                  </Field>
                  <Field label="Middlewares" hint="One per line">
                    <textarea rows={2} className="mono" value={f.middlewares} onChange={(e) => set("middlewares", e.target.value)} />
                  </Field>
                </div>
              )}
              {isRepo && (
                <div className="grid2">
                  <Field label="Build platform">
                    <select value={f.platform} onChange={(e) => set("platform", e.target.value)}>
                      <option value="">linux/arm64 (default)</option>
                      <option value="linux/arm64">linux/arm64</option>
                      <option value="linux/amd64">linux/amd64</option>
                      <option value="linux/arm/v7">linux/arm/v7</option>
                    </select>
                  </Field>
                  <Field label="Build args" hint="KEY=VALUE, or KEY to read it from the server's environment">
                    <textarea rows={2} className="mono" value={f.buildArgs} onChange={(e) => set("buildArgs", e.target.value)} />
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
              {deploy.isPending ? "Starting…" : adopt ? "Adopt" : redeploy ? "Redeploy" : "Deploy"}
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
