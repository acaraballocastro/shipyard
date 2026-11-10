// Typed client for shipyard's /api/v1. Mirrors internal/api and internal/core.

export interface ServerInfo {
  name: string;
  kind: "local" | "remote";
  url?: string;
  default: boolean;
}

export interface HostInfo {
  hostname: string;
  version: string;
  os: string;
  arch: string;
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
export interface DeploymentConfig {
  Source: { Type: string; Repository?: string };
  Build: { Platform?: string };
  Deploy: { Mode: string; Entrypoint?: string; TLS?: boolean };
  Env: { Files?: string[]; Keys?: string[] };
  Healthcheck: { Enabled: boolean; Command?: string };
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
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (res.status === 204) return undefined as T;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, body.error ?? res.statusText);
  return body as T;
}

const enc = encodeURIComponent;

export const api = {
  session: () => request<{ authenticated: boolean }>("/session"),
  login: (token: string) =>
    request<{ authenticated: boolean }>("/session", { method: "POST", body: JSON.stringify({ token }) }),
  logout: () => request<void>("/session", { method: "DELETE" }),

  servers: () => request<ServerInfo[]>("/servers"),
  info: (server: string) => request<HostInfo>(`/servers/${enc(server)}/info`),
  apps: (server: string) => request<App[]>(`/servers/${enc(server)}/apps`),
  status: (server: string, app: string) => request<AppStatus>(`/servers/${enc(server)}/apps/${enc(app)}`),
  logsUrl: (server: string, app: string, tail: number) =>
    `/api/v1/servers/${enc(server)}/apps/${enc(app)}/logs?follow=1&tail=${tail}`,
};
