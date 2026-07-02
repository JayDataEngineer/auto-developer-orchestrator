// Settings panel — Phase 4 stub. Phase 6 wires the model picker via
// `piClient.setModel` and a few per-thread config knobs.

import type { FC } from "react";

export const SettingsPanel: FC = () => {
  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          settings
        </span>
      </div>
      <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-xs text-muted-foreground">
        <div className="max-w-sm space-y-2 text-center">
          <div className="text-sm font-medium text-foreground">Coming in Phase 6</div>
          <div>
            Per-thread model + thinking-level picker (drives{" "}
            <code className="font-mono">piClient.setModel</code>), default
            workspace, and theme. Today these are config-file only.
          </div>
        </div>
      </div>
    </div>
  );
};
