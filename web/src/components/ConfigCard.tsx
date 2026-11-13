import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, type AppStatus } from "../api";

// Every setting the service was deployed with (deployment.yaml), plus its
// env values on demand. Values are fetched only when asked for and not
// kept in the cache afterwards.
export function ConfigCard({ server, status, redeployTo }: { server: string; status: AppStatus; redeployTo: string }) {
  const c = status.config;
  const [reveal, setReveal] = useState(false);
  const env = useQuery({
    queryKey: ["env", server, status.name],
    queryFn: () => api.env(server, status.name),
    enabled: reveal,
    gcTime: 0,
    retry: false,
  });

  if (!c) {
    return (
      <section className="card config">
        <h2>Configuration</h2>
        <p className="muted small">No deployment.yaml found for this service (deployed with an older shipyard). Redeploy it to record its settings.</p>
      </section>
    );
  }

  const git = c.Source.Type === "git";
  const keys = c.Env.Keys ?? [];
  const buildArgs = [
    ...Object.entries(c.Build.Args ?? {}).map(([k, v]) => `${k}=${v}`),
    ...(c.Build.ArgsFromEnv ?? []).map((k) => `${k} (from environment)`),
  ];

  return (
    <section className="card config">
      <div className="config-head">
        <h2>Configuration</h2>
        <Link className="button secondary" to={redeployTo}>
          Edit and redeploy…
        </Link>
      </div>

      <Group title="Source">
        <Row k="Type">{git ? "GitHub repository (built on the server)" : "Docker image"}</Row>
        {git && <Row k="Repository" mono>{c.Source.Repository}</Row>}
        <Row k="Image" mono>{c.Image}</Row>
        {git && <Row k="Platform" mono>{c.Build.Platform || "linux/arm64"}</Row>}
        {git && buildArgs.length > 0 && <Row k="Build args" mono list={buildArgs} />}
      </Group>

      <Group title="Networking">
        <Row k="Mode">{c.Deploy.Mode === "traefik" ? "Traefik (domain)" : "Local ports"}</Row>
        {c.Deploy.Mode === "traefik" ? (
          <>
            <Row k="Domain">{c.Deploy.Domain}</Row>
            <Row k="Container port" mono>{c.Deploy.InternalPort}</Row>
            <Row k="Network" mono>{c.Deploy.Network}</Row>
            <Row k="Entrypoint" mono>{c.Deploy.Entrypoint}</Row>
            <Row k="TLS">{c.Deploy.TLS ? "on" : "off"}</Row>
            {c.Deploy.CertResolver && <Row k="Cert resolver" mono>{c.Deploy.CertResolver}</Row>}
            {c.Deploy.RouterName && <Row k="Router" mono>{c.Deploy.RouterName}</Row>}
            {!!c.Deploy.Middlewares?.length && <Row k="Middlewares" mono list={c.Deploy.Middlewares} />}
          </>
        ) : (
          <Row k="Ports" mono list={c.Deploy.Ports ?? []} />
        )}
      </Group>

      <Group title="Storage">
        <Row k="Volumes" mono list={c.Deploy.Volumes ?? []} />
        <Row k="Path" mono>{status.path}</Row>
      </Group>

      <Group
        title="Environment"
        action={
          keys.length > 0 && (
            <button className="link small" onClick={() => setReveal((r) => !r)}>
              {reveal ? "Hide values" : "Show values"}
            </button>
          )
        }
      >
        {keys.length === 0 && !(c.Env.Files ?? []).length && <div className="muted small">No variables.</div>}
        {keys.length > 0 && (
          <div className="env-table">
            {keys.map((k) => (
              <div key={k}>
                <span className="mono">{k}</span>
                <span className="mono env-value">
                  {!reveal ? "••••••••" : env.isLoading ? "…" : env.error ? "unavailable" : env.data?.[k] ?? ""}
                </span>
              </div>
            ))}
          </div>
        )}
        {env.error && reveal && <div className="small bad-text">{env.error.message}</div>}
        {!!c.Env.Files?.length && <Row k="Env files" mono list={c.Env.Files} />}
      </Group>

      <Group title="Healthcheck">
        {c.Healthcheck.Enabled ? (
          <>
            <Row k="Command" mono>{c.Healthcheck.Command}</Row>
            <Row k="Interval / timeout" mono>
              {(c.Healthcheck.Interval || "default") + " / " + (c.Healthcheck.Timeout || "default")}
            </Row>
            <Row k="Retries / start period" mono>
              {(c.Healthcheck.Retries || "default") + " / " + (c.Healthcheck.StartPeriod || "default")}
            </Row>
          </>
        ) : (
          <div className="muted small">Disabled.</div>
        )}
      </Group>
    </section>
  );
}

function Group({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="config-group">
      <div className="config-group-head">
        <h3>{title}</h3>
        {action}
      </div>
      <dl>{children}</dl>
    </div>
  );
}

function Row({ k, mono, list, children }: { k: string; mono?: boolean; list?: string[]; children?: ReactNode }) {
  const empty = list ? list.length === 0 : children === undefined || children === null || children === "";
  return (
    <div className="config-row">
      <dt>{k}</dt>
      <dd className={mono ? "mono" : ""}>
        {empty ? <span className="muted">–</span> : list ? list.map((x) => <div key={x}>{x}</div>) : children}
      </dd>
    </div>
  );
}
