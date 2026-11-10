import { useState, type FormEvent } from "react";
import { api } from "../api";

export function Login({ onLogin }: { onLogin: () => void }) {
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.login(token.trim());
      onLogin();
    } catch {
      setError("Invalid token");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="center">
      <form className="card login" onSubmit={submit}>
        <h1>
          <span aria-hidden>⚓</span> Shipyard
        </h1>
        <label>
          Access token
          <input
            type="password"
            autoFocus
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="shipyard serve token"
          />
        </label>
        {error && <div className="error">{error}</div>}
        <button className="primary" disabled={busy || !token}>
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <p className="muted small">
          On the server, run <code>shipyard serve token</code>. On your Mac, <code>shipyard ui</code> signs you in
          automatically.
        </p>
      </form>
    </div>
  );
}
