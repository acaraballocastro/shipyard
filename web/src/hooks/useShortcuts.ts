import { useEffect } from "react";

// useShortcuts binds single-key shortcuts, ignored while typing in a field
// or with a modifier held. Keys are KeyboardEvent.key values ("/", "n", "?").
export function useShortcuts(map: Record<string, () => void>) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) {
        if (e.key === "Escape") t.blur();
        return;
      }
      const fn = map[e.key];
      if (fn) {
        e.preventDefault();
        fn();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [map]);
}
