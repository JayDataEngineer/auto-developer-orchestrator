// Settings panel — org picker for the Agent Protocol harness.
// Uses CopilotKit's agent selection.

import { useCallback, useEffect, useState, type FC } from "react";
import { Loader2Icon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAgents } from "@/lib/use-agents.ts";

interface SettingsPanelProps {
  threadId: string | null;
}

export const SettingsPanel: FC<SettingsPanelProps> = ({ threadId }) => {
  const { agents, loading: agentsLoading } = useAgents();
  const [selectedAgent, setSelectedAgent] = useState("general");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (agents.length > 0 && !selectedAgent) {
      setSelectedAgent(agents[0].agent_id);
    }
  }, [agents, selectedAgent]);

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          settings
        </span>
        {!threadId && (
          <span className="text-muted-foreground">— no thread selected</span>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-4 text-xs">
        {error && (
          <div className="mb-3 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1 text-destructive">
            {error}
          </div>
        )}

        <div className="space-y-6">
          <section>
            <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
              org (agent)
            </div>
            <div className="flex items-center gap-2">
              <select
                value={selectedAgent}
                onChange={(e) => setSelectedAgent(e.target.value)}
                className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs outline-none focus:border-ring"
              >
                {agents.map((a) => (
                  <option key={a.agent_id} value={a.agent_id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="mt-1 text-[11px] text-muted-foreground/70">
              current org: <code className="font-mono">{selectedAgent ?? "—"}</code>
            </div>
            <div className="mt-3 space-y-1">
              {agents.map((a) => (
                <div key={a.agent_id} className="rounded-md border border-border/50 p-2">
                  <div className="font-medium">{a.name}</div>
                  <div className="text-muted-foreground/70">{a.description}</div>
                </div>
              ))}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
};
