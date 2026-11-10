import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, NavLink, Route, Routes } from "react-router-dom";
import { api, ApiError } from "./api";
import { Login } from "./pages/Login";
import { ServerPage } from "./pages/ServerPage";
import { AppPage } from "./pages/AppPage";

// Session bootstrap: `shipyard ui` opens /#token=…, which is exchanged for
// an HttpOnly cookie and removed from the URL.
function useSession() {
  const [state, setState] = useState<"checking" | "in" | "out">("checking");
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
        await api.session();
        setState("in");
      } catch {
        setState("out");
      }
    })();
  }, []);
  return [state, setState] as const;
}

export function App() {
  const [session, setSession] = useSession();
  const qc = useQueryClient();

  if (session === "checking") return <div className="center muted">Loading…</div>;
  if (session === "out") return <Login onLogin={() => setSession("in")} />;

  return (
    <div className="layout">
      <Sidebar
        onLogout={async () => {
          await api.logout();
          qc.clear();
          setSession("out");
        }}
      />
      <main className="main">
        <Routes>
          <Route path="/" element={<DefaultServer />} />
          <Route path="/servers/:server" element={<ServerPage />} />
          <Route path="/servers/:server/apps/:app" element={<AppPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}

function useServers() {
  return useQuery({ queryKey: ["servers"], queryFn: api.servers });
}

function DefaultServer() {
  const { data, error } = useServers();
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  const s = data.find((x) => x.default) ?? data[0];
  return <Navigate to={`/servers/${s.name}`} replace />;
}

function Sidebar({ onLogout }: { onLogout: () => void }) {
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
            {s.default && <span className="tag">default</span>}
          </NavLink>
        ))}
      </nav>
      <button className="link sidebar-foot" onClick={onLogout}>
        Sign out
      </button>
    </aside>
  );
}

export function ErrorBox({ error }: { error: unknown }) {
  const msg = error instanceof ApiError || error instanceof Error ? error.message : String(error);
  return <div className="error">{msg}</div>;
}
