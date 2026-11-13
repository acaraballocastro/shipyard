import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

type Kind = "ok" | "bad" | "info";
interface Toast {
  id: number;
  kind: Kind;
  title: string;
  body?: string;
}

const Ctx = createContext<(kind: Kind, title: string, body?: string) => void>(() => {});

// useToast returns notify(kind, title, body?): a dismissible notice that
// fades after a few seconds (errors stay twice as long).
export const useToast = () => useContext(Ctx);

let seq = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), []);
  const notify = useCallback(
    (kind: Kind, title: string, body?: string) => {
      const id = ++seq;
      setToasts((t) => [...t.slice(-3), { id, kind, title, body }]);
      setTimeout(() => dismiss(id), kind === "bad" ? 10000 : 5000);
    },
    [dismiss],
  );
  return (
    <Ctx.Provider value={notify}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind}`}>
            <div>
              <div className="strong">{t.title}</div>
              {t.body && <div className="small muted">{t.body}</div>}
            </div>
            <button className="link" aria-label="Dismiss" onClick={() => dismiss(t.id)}>
              ×
            </button>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  );
}
