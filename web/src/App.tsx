import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, NavLink, Route, Routes } from "react-router-dom";
import { api, ApiError, type ServerInfo, type Session } from "./api";
import { Login } from "./pages/Login";
import { ServerPage } from "./pages/ServerPage";
import { AppPage } from "./pages/AppPage";
import { DeployPage } from "./pages/DeployPage";
import { ToastProvider } from "./components/Toast";
import { useTheme } from "./hooks/useTheme";
import { useShortcuts } from "./hooks/useShortcuts";

// Session bootstrap: `shipyard ui` opens /#token=…, which is exchanged for
// an HttpOnly cookie and removed from the URL.
function useSession() {
  const [state, setState] = useState<"checking" | "in" | "out">("checking");
  const [info, setInfo] = useState<Session | null>(null);
  useEffect(() => {
    (async () => {
      const m = window.location.hash.match(/token=([A-Za-z0-9]+)/);
      if (m) {
        history.replaceState(null, "", window.location.pathname + window.location.search);
        try {
          await api.login(m[1]);
          return setState("in");
        } catch {
          /* fall through to session check */
        }
      }
      try {
        setInfo(await api.session());
        setState("in");
      } catch {
        setState("out");
      }
    })();
  }, []);
  return [state, setState, info] as const;
}

export function App() {
  const [session, setSession, sessionInfo] = useSession();
  const qc = useQueryClient();
  const theme = useTheme();
  const [help, setHelp] = useState(false);
  const shortcuts = useMemo(() => ({ "?": () => setHelp((h) => !h), Escape: () => setHelp(false) }), []);
  useShortcuts(shortcuts);

  if (session === "checking") return <div className="center muted">Loading…</div>;
  if (session === "out") return <Login onLogin={() => setSession("in")} />;

  return (
    <ToastProvider>
      <div className="layout">
        <Sidebar
          theme={theme.theme}
          onTheme={theme.cycle}
          onHelp={() => setHelp(true)}
          email={sessionInfo?.email}
          onLogout={async () => {
            await api.logout();
            qc.clear();
            if (sessionInfo?.access) {
              // End the Cloudflare Access session too, or it signs us right back in.
              window.location.href = "/cdn-cgi/access/logout";
              return;
            }
            setSession("out");
          }}
        />
        <main className="main">
          <Routes>
            <Route path="/" element={<DefaultServer />} />
            <Route path="/servers/:server" element={<ServerPage />} />
            <Route path="/servers/:server/deploy" element={<DeployPage />} />
            <Route path="/servers/:server/apps/:app" element={<AppPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
      {help && <ShortcutHelp onClose={() => setHelp(false)} />}
    </ToastProvider>
  );
}

// Polled so the active route (failover) stays current in the sidebar.
export function useServers() {
  return useQuery({ queryKey: ["servers"], queryFn: api.servers, refetchInterval: 15000 });
}

function DefaultServer() {
  const { data, error } = useServers();
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  const s = data.find((x) => x.default) ?? data[0];
  return <Navigate to={`/servers/${s.name}`} replace />;
}

function Sidebar({
  theme,
  onTheme,
  onHelp,
  onLogout,
  email,
}: {
  email?: string;
  theme: string;
  onTheme: () => void;
  onHelp: () => void;
  onLogout: () => void;
}) {
  const { data: servers } = useServers();
  return (
    <aside className="sidebar">
      <div className="brand">
        <span aria-hidden>⚓</span> Shipyard
      </div>
      <nav>
        <div className="nav-label">Servers</div>
        {servers?.map((s) => (
          <NavLink key={s.name} to={`/servers/${s.name}`} className="nav-item">
            <span className={`dot ${s.kind}`} />
            {s.name === "local" ? "This machine" : s.name}
            <RouteTag routes={s.routes} />
            {s.default && <span className="tag">default</span>}
          </NavLink>
        ))}
      </nav>
      <div className="sidebar-foot">
        {email && (
          <span className="small muted trunc hide-sm" title="Signed in with Cloudflare Access">
            {email}
          </span>
        )}
        <button className="link" onClick={onTheme} title="Switch theme">
          {theme === "dark" ? "☾ Dark" : theme === "light" ? "☀ Light" : "◐ System"}
        </button>
        <button className="link hide-sm" onClick={onHelp} title="Keyboard shortcuts (?)">
          ⌨ Shortcuts
        </button>
        <button className="link" onClick={onLogout}>
          Sign out
        </button>
      </div>
    </aside>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  const msg = error instanceof ApiError || error instanceof Error ? error.message : String(error);
  return <div className="error">{msg}</div>;
}

function ShortcutHelp({ onClose }: { onClose: () => void }) {
  const keys: [string, string][] = [
    ["/", "Search services"],
    ["n", "New deployment"],
    ["?", "Show or hide this help"],
    ["Esc", "Close dialog or leave a field"],
  ];
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="card modal" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts" onClick={(e) => e.stopPropagation()}>
        <h2>Keyboard shortcuts</h2>
        <dl className="shortcuts">
          {keys.map(([k, d]) => (
            <div key={k}>
              <dt>
                <kbd>{k}</kbd>
              </dt>
              <dd>{d}</dd>
            </div>
          ))}
        </dl>
        <div className="actions end">
          <button className="secondary" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

// RouteTag shows the route in use when it is not the preferred one.
export function RouteTag({ routes, always }: { routes?: ServerInfo["routes"]; always?: boolean }) {
  if (!routes?.length) return null;
  const i = routes.findIndex((r) => r.active);
  if (i < 0 || (i === 0 && !always)) return null;
  const why = routes
    .slice(0, i)
    .map((r) => `${r.name}: ${r.error ?? "unreachable"}`)
    .join("\n");
  return (
    <span className={`tag route ${i > 0 ? "fallback" : ""}`} title={i > 0 ? `Fallback route. Preferred routes failed:\n${why}` : "Preferred route"}>
      via {routes[i].name}
    </span>
  );
}
