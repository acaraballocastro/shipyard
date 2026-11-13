import { useQuery } from "@tanstack/react-query";
import { api, type Sample } from "../api";

export type { Sample };

const MAX_SAMPLES = 120; // 10 minutes at the 5s poll
const MAX_GAP = 20_000; // ms; a longer gap starts a new series
const history = new Map<string, Sample[]>(); // "server/app" -> samples
const seeded = new Set<string>();

// Keep only the newest unbroken run: points are drawn evenly spaced, so a
// gap (nobody was watching) would otherwise look like a smooth line.
function contiguous(list: Sample[]): Sample[] {
  let start = list.length - 1;
  while (start > 0 && list[start].t - list[start - 1].t <= MAX_GAP) start--;
  return list.slice(Math.max(0, start), list.length).slice(-MAX_SAMPLES);
}

// Seeded samples carry the server's clock, live ones the browser's; a live
// sample is never placed before the newest one, whatever the clock skew.
function push(key: string, s: Sample, live = false) {
  const h = history.get(key) ?? [];
  const last = h[h.length - 1];
  if (live && last && s.t <= last.t) s = { ...s, t: last.t + 1 };
  else if (last && s.t <= last.t) return;
  h.push(s);
  history.set(key, contiguous(h));
}

// useStats polls container stats for a server (one docker call for all
// services) and keeps a rolling history for sparklines. The first fetch
// seeds it from the server, so a reload keeps the recent past. The query
// is shared, so every component on the page sees the same samples.
export function useStats(server: string) {
  return useQuery({
    queryKey: ["stats", server],
    queryFn: async () => {
      if (!seeded.has(server)) {
        seeded.add(server);
        try {
          const past = await api.statsHistory(server);
          for (const [app, list] of Object.entries(past)) {
            for (const s of list) push(`${server}/${app}`, s);
          }
        } catch {
          /* older server: start empty */
        }
      }
      const data = await api.stats(server);
      const t = Date.now();
      for (const [app, s] of Object.entries(data)) push(`${server}/${app}`, { ...s, t }, true);
      return data;
    },
    refetchInterval: 5000,
    retry: false,
  });
}

export function statsHistory(server: string, app: string): Sample[] {
  return history.get(`${server}/${app}`) ?? [];
}
