// Hook to fetch available agents from the harness.

import { useEffect, useState } from "react";

const HARNES_URL = import.meta.env.VITE_PUX_HARNESS_URL ?? "http://127.0.0.1:9988";

export interface AgentInfo {
  agent_id: string;
  name: string;
  description: string;
}

export function useAgents() {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function fetchAgents() {
      try {
        const resp = await fetch(`${HARNES_URL}/agents/search`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({}),
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const data = await resp.json();
        if (!cancelled) {
          setAgents(data.agents ?? []);
          setLoading(false);
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      }
    }
    fetchAgents();
    return () => { cancelled = true; };
  }, []);

  return { agents, loading, error };
}
