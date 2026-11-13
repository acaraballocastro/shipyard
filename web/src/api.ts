// Typed client for shipyard's /api/v1. Mirrors internal/api and internal/core.

export interface ServerInfo {
  name: string;
  kind: "local" | "remote";
  url?: string;
  default: boolean;
  upgradable?: boolean;
  routes?: RouteStatus[]; // remote servers, once contacted
}

// One way to reach a server (internal/api.RouteStatus), in preference order.
export interface RouteStatus {
  name: string;
  active: boolean;
  error?: string;
}

// The API level this UI needs (internal/api.APILevel). The UI ships inside
// the same binary, so a server reporting less runs an older shipyard.
export const REQUIRED_API = 5;

export interface HostInfo {
  hostname: string;
  version: string;
  os: string;
  arch: string;
  api?: number; // absent on servers older than level 2
}

export interface App {
  name: string;
  repo?: string;
  image: string;
  mode: string;
  domain?: string;
  network?: string;
  path: string;
}

export interface AppStatus extends App {
  container: boolean;
  state?: string;
  running: boolean;
  health?: string;
  failingStreak?: number;
  startedAt?: string;
  ports?: string[];
  cpu?: string;
  memory?: string;
  config?: DeploymentConfig;
}

// deployment.yaml as JSON (Go field names).
// deployment.yaml as JSON (Go field names, no json tags: every field is
// present and empty lists/maps come as null).
export interface DeploymentConfig {
  Version: number;
  Name: string;
  Image: string;
  Source: { Type: "git" | "image" | string; Repository?: string };
  Build: { Platform?: string; Args?: Record<string, string> | null; ArgsFromEnv?: string[] | null };
  Deploy: {
    Mode: string;
    Domain?: string;
    Network?: string;
    InternalPort?: string;
    Entrypoint?: string;
    TLS?: boolean;
    CertResolver?: string;
    RouterName?: string;
    Middlewares?: string[] | null;
    Ports?: string[] | null;
    Volumes?: string[] | null;
    Command?: string[] | null;
  };
  Env: { Files?: string[] | null; Keys?: string[] | null };
  Healthcheck: {
    Enabled: boolean;
    Command?: string;
    Interval?: string;
    Timeout?: string;
    Retries?: number;
    StartPeriod?: string;
  };
}

export type JobStatus = "running" | "succeeded" | "failed" | "canceled";
export type Action = "restart" | "update" | "delete";

export interface Job {
  id: string;
  app: string;
  action: Action | "deploy" | "upgrade" | "prune" | "adopt";
  status: JobStatus;
  error?: string;
  startedAt: string;
  finishedAt?: string;
}

// A progress event from the server (internal/events).
export interface ProgressEvent {
  time: string;
  level: "step" | "info" | "warn" | "detail" | "ok";
  stage?: string;
  key?: string;
  message: string;
}

// A resource sample of one container (core.Stats).
export interface Stats {
  cpu: number;
  memPerc: number;
  memUsage: string;
  netIO: string;
  blockIO: string;
  pids: number;
}

// The machine running the services (core.Host). Zero means unknown.
export interface Host {
  cpu: number;
  cores: number;
  load: [number, number, number];
  memTotal: number;
  memUsed: number;
  swapUsed: number;
  temp?: number;
  uptime: number;
  disks: { path: string; total: number; used: number }[] | null;
  docker?: { images: number; imagesReclaim: number; containers: number; volumes: number; buildCache: number; buildCacheReclaim: number };
  memoryCgroup: boolean;
}

export interface WebhookInfo {
  enabled: boolean;
  branch?: string;
  path?: string;
  url?: string;
  secret?: string; // only right after enabling or rotating
  last?: { at: string; event: string; ref?: string; commit?: string; result: string; detail?: string; jobId?: string };
}

export interface AdoptPlan {
  container: string;
  request: DeployRequest;
  warnings: string[];
  blockers: string[];
}

// Any Docker container on the server (core.Container).
export interface Container {
  name: string;
  image: string;
  state: string;
  status: string;
  ports?: string;
  project?: string;
  managed: boolean;
  app?: string;
}
export type ContainerAction = "start" | "stop" | "restart";

// "n/a" when Docker reports no memory accounting (common on Raspberry Pi
// without cgroup_enable=memory), instead of a misleading 0%.
export function memAvailable(s?: Stats) {
  return !!s && !/^0B\s*\/\s*0B/.test(s.memUsage ?? "");
}

export interface Sample extends Stats {
  t: number; // unix ms
}

// core.DeployRequest. Omitted fields keep the saved value of an existing
// service, or the default for a new one.
export interface DeployRequest {
  repo?: string;
  image?: string;
  name?: string;
  mode?: "local" | "traefik";
  domain?: string;
  network?: string;
  createNetwork?: boolean;
  internalPort?: string;
  ports?: string[];
  volumes?: string[];
  command?: string[];
  envFiles?: string[];
  env?: Record<string, string>;
  unsetEnv?: string[];
  buildArgs?: string[];
  platform?: string;
  entrypoint?: string;
  tls?: boolean;
  certResolver?: string;
  routerName?: string;
  middlewares?: string[];
  healthcheckCmd?: string;
  healthcheckInterval?: string;
  healthcheckTimeout?: string;
  healthcheckRetries?: number;
  healthcheckStartPeriod?: string;
  noHealthcheck?: boolean;
}

export interface Session {
  authenticated: boolean;
  access?: boolean; // reached through Cloudflare Access
  email?: string; // Access identity, when signed in that way
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    credentials: "same-origin",
    ...init,
    // X-Shipyard: required by the server on state-changing requests (CSRF).
    headers: { "Content-Type": "application/json", "X-Shipyard": "1", ...init?.headers },
  });
  if (res.status === 204) return undefined as T;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, body.error ?? res.statusText);
  return body as T;
}

const enc = encodeURIComponent;

export const api = {
  session: () => request<Session>("/session"),
  login: (token: string) =>
    request<{ authenticated: boolean }>("/session", { method: "POST", body: JSON.stringify({ token }) }),
  logout: () => request<void>("/session", { method: "DELETE" }),

  servers: () => request<ServerInfo[]>("/servers"),
  info: (server: string) => request<HostInfo>(`/servers/${enc(server)}/info`),
  apps: (server: string) => request<App[]>(`/servers/${enc(server)}/apps`),
  status: (server: string, app: string) => request<AppStatus>(`/servers/${enc(server)}/apps/${enc(app)}`),
  startAction: (server: string, app: string, action: Action) =>
    request<Job>(`/servers/${enc(server)}/apps/${enc(app)}/actions/${action}`, { method: "POST" }),
  stats: (server: string) => request<Record<string, Stats>>(`/servers/${enc(server)}/stats`),
  host: (server: string) => request<Host>(`/servers/${enc(server)}/host`),
  prune: (server: string, all: boolean) =>
    request<Job>(`/servers/${enc(server)}/prune`, { method: "POST", body: JSON.stringify({ all }) }),
  webhook: (server: string, app: string) => request<WebhookInfo>(`/servers/${enc(server)}/apps/${enc(app)}/webhook`),
  setWebhook: (server: string, app: string, branch: string, rotate = false) =>
    request<WebhookInfo>(`/servers/${enc(server)}/apps/${enc(app)}/webhook`, {
      method: "PUT",
      body: JSON.stringify({ branch, rotate }),
    }),
  deleteWebhook: (server: string, app: string) =>
    request<void>(`/servers/${enc(server)}/apps/${enc(app)}/webhook`, { method: "DELETE" }),
  adoptPlan: (server: string, name: string) => request<AdoptPlan>(`/servers/${enc(server)}/containers/${enc(name)}/adopt`),
  adopt: (server: string, name: string, req: DeployRequest) =>
    request<Job>(`/servers/${enc(server)}/containers/${enc(name)}/adopt`, { method: "POST", body: JSON.stringify(req) }),
  env: (server: string, app: string) => request<Record<string, string>>(`/servers/${enc(server)}/apps/${enc(app)}/env`),
  statsHistory: (server: string) => request<Record<string, Sample[]>>(`/servers/${enc(server)}/stats/history`),
  upgrade: (server: string) => request<Job>(`/servers/${enc(server)}/upgrade`, { method: "POST" }),
  deploy: (server: string, req: DeployRequest) =>
    request<Job>(`/servers/${enc(server)}/deploy`, { method: "POST", body: JSON.stringify(req) }),
  jobs: (server: string, app?: string) =>
    request<Job[]>(`/servers/${enc(server)}/jobs${app ? `?app=${enc(app)}` : ""}`),
  job: (server: string, id: string) => request<Job>(`/servers/${enc(server)}/jobs/${enc(id)}`),
  cancelJob: (server: string, id: string) =>
    request<void>(`/servers/${enc(server)}/jobs/${enc(id)}/cancel`, { method: "POST" }),
  jobEventsUrl: (server: string, id: string) => `/api/v1/servers/${enc(server)}/jobs/${enc(id)}/events`,

  containers: (server: string) => request<Container[]>(`/servers/${enc(server)}/containers`),
  containerAction: (server: string, name: string, action: ContainerAction) =>
    request<void>(`/servers/${enc(server)}/containers/${enc(name)}/actions/${action}`, { method: "POST" }),
  containerLogsUrl: (server: string, name: string, tail: number) =>
    `/api/v1/servers/${enc(server)}/containers/${enc(name)}/logs?follow=1&tail=${tail}`,
  logsUrl: (server: string, app: string, tail: number) =>
    `/api/v1/servers/${enc(server)}/apps/${enc(app)}/logs?follow=1&tail=${tail}`,
};

// bytes renders a size: "1.2 GB", "512 MB".
export function bytes(n?: number): string {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1000 && i < u.length - 1) {
    n /= 1000;
    i++;
  }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${u[i]}`;
}
