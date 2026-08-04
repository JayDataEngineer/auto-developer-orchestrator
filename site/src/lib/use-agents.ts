// Hook to fetch available agents via the CopilotKit BFF (single ingress point).
//
// The BFF (site/server) owns the org→graph map, derived from aegra.json at
// startup. By querying the BFF's own info endpoint instead of Aegra directly,
// the frontend has ONE backend to talk to — no cross-origin Aegra dependency,
// and the agent list is always in sync with what the BFF actually serves.
//
// Protocol: CopilotKit v2 single-route — POST /api/copilotkit { method: "info" }
// Response: { agents: { name: { name, description, className }, ... } }
// We flatten the object to the AgentInfo[] the UI expects and drop the
// synthetic "default" alias (it maps to "general" and would show as a dup).

import { useEffect, useState } from "react";

const SITE_URL = import.meta.env.VITE_PUX_SITE_URL ?? "http://127.0.0.1:3001";

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
        const resp = await fetch(`${SITE_URL}/api/copilotkit`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ method: "info" }),
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const data = await resp.json();
        if (!cancelled) {
          const agentsMap: Record<string, { name?: string; description?: string }> =
            data.agents ?? {};
          const list: AgentInfo[] = Object.entries(agentsMap)
            .filter(([name]) => name !== "default")
            .map(([name, info]) => ({
              agent_id: name,
              name: info.name ?? name,
              description: info.description ?? "",
            }))
            .sort((a, b) => a.name.localeCompare(b.name));
          setAgents(list);
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
