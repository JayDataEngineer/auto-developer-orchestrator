// Pux site shell — CopilotKit sidebar + workbench (editor/terminal/sandbox/vnc).

import { useCallback, useEffect, useState } from "react";
import { Group, Panel, Separator } from "react-resizable-panels";
import {
  Code2Icon,
  TerminalIcon,
  MonitorIcon,
  ContainerIcon,
  SettingsIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { PuxRuntimeProvider } from "./lib/runtime.tsx";
import { CopilotSidebar } from "@copilotkit/react-core/v2";
import "@copilotkit/react-core/v2/styles.css";
import { EditorPanel } from "./components/workbench/editor-panel.tsx";
import { TerminalPanel } from "./components/workbench/terminal-panel.tsx";
import { SandboxPanel } from "./components/workbench/sandbox-panel.tsx";
import { SettingsPanel } from "./components/workbench/settings-panel.tsx";
import { VncViewer } from "./components/workbench/vnc-viewer.tsx";
import { cn } from "./lib/utils";

type WorkbenchTab = "files" | "terminal" | "sandbox" | "vnc" | "settings";

function readThreadFromUrl(): string | null {
  if (typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.hash.slice(1));
  return params.get("thread");
}

function writeThreadToUrl(id: string | null) {
  if (typeof window === "undefined") return;
  const params = new URLSearchParams(window.location.hash.slice(1));
  if (id) params.set("thread", id);
  else params.delete("thread");
  const next = params.toString();
  const hash = next ? `#${next}` : "";
  if (window.location.hash !== hash) {
    window.history.replaceState(null, "", `${window.location.pathname}${hash}`);
  }
}

export function App() {
  const [activeThreadId, setActiveThreadId] = useState<string | null>(
    readThreadFromUrl,
  );
  const [workbenchOpen, setWorkbenchOpen] = useState(false);
  const [workbenchTab, setWorkbenchTab] = useState<WorkbenchTab>("files");

  useEffect(() => {
    writeThreadToUrl(activeThreadId);
  }, [activeThreadId]);

  const openTab = useCallback((tab: WorkbenchTab) => {
    setWorkbenchTab(tab);
    setWorkbenchOpen(true);
  }, []);

  return (
    <PuxRuntimeProvider>
      <div className="flex h-full flex-col bg-background text-foreground">
        <header className="flex h-10 items-center gap-2 border-b border-border px-3">
          <div className="text-sm font-semibold tracking-tight">
            <span className="text-muted-foreground">π</span> pux site
          </div>
          <div className="ml-auto flex items-center gap-1">
            <button
              onClick={() => openTab("files")}
              className={cn(
                "flex size-7 items-center justify-center rounded-md hover:bg-accent",
                workbenchOpen && workbenchTab === "files" && "bg-accent",
              )}
              title="Files"
            >
              <Code2Icon className="size-4" />
            </button>
            <button
              onClick={() => openTab("terminal")}
              className={cn(
                "flex size-7 items-center justify-center rounded-md hover:bg-accent",
                workbenchOpen && workbenchTab === "terminal" && "bg-accent",
              )}
              title="Terminal"
            >
              <TerminalIcon className="size-4" />
            </button>
            <button
              onClick={() => openTab("sandbox")}
              className={cn(
                "flex size-7 items-center justify-center rounded-md hover:bg-accent",
                workbenchOpen && workbenchTab === "sandbox" && "bg-accent",
              )}
              title="Sandbox"
            >
              <ContainerIcon className="size-4" />
            </button>
            <button
              onClick={() => openTab("vnc")}
              className={cn(
                "flex size-7 items-center justify-center rounded-md hover:bg-accent",
                workbenchOpen && workbenchTab === "vnc" && "bg-accent",
              )}
              title="VNC"
            >
              <MonitorIcon className="size-4" />
            </button>
            <button
              onClick={() => openTab("settings")}
              className={cn(
                "flex size-7 items-center justify-center rounded-md hover:bg-accent",
                workbenchOpen && workbenchTab === "settings" && "bg-accent",
              )}
              title="Settings"
            >
              <SettingsIcon className="size-4" />
            </button>
          </div>
        </header>

        <div className="flex-1 overflow-hidden">
          <Group
            orientation="horizontal"
            id="pux-site-shell"
            key={`w${workbenchOpen ? 1 : 0}`}
            className="h-full"
          >
            <Panel id="chat">
              <CopilotSidebar
                agentId="general"
                defaultOpen={true}
                labels={{
                  modalHeaderTitle: "Pux",
                }}
                className="h-full"
              >
                {/* CopilotKit renders the chat UI here */}
              </CopilotSidebar>
            </Panel>
            {workbenchOpen && (
              <>
                <Separator className="w-px bg-border" />
                <Panel
                  id="workbench"
                  defaultSize="45%"
                  minSize="20%"
                  maxSize="70%"
                >
                  <section className="flex h-full flex-col bg-background">
                    <div className="flex h-9 items-center border-b border-border px-2 text-xs">
                      <button
                        onClick={() => setWorkbenchOpen(false)}
                        className="rounded-md px-2 py-1 text-muted-foreground hover:bg-accent hover:text-foreground"
                        title="Hide workbench"
                      >
                        ×
                      </button>
                      <div className="ml-2 text-muted-foreground/70">
                        {workbenchTab}
                      </div>
                    </div>
                    <div className="min-h-0 flex-1">
                      {workbenchTab === "files" && <EditorPanel />}
                      {workbenchTab === "terminal" && <TerminalPanel />}
                      {workbenchTab === "sandbox" && <SandboxPanel />}
                      {workbenchTab === "vnc" && <VncViewer />}
                      {workbenchTab === "settings" && (
                        <SettingsPanel threadId={activeThreadId} />
                      )}
                    </div>
                  </section>
                </Panel>
              </>
            )}
          </Group>
        </div>
      </div>
    </PuxRuntimeProvider>
  );
}
