// Minimal workbench UI state. Pi-mono owns thread/message/tool state via
// react-pi; this store only covers things react-pi doesn't: sidebar collapse,
// active workbench tab, terminal drawer visibility.

import { create } from "zustand";

export type WorkbenchTab = "vnc" | "editor" | "files" | "settings";

interface SiteState {
  sidebarCollapsed: boolean;
  activeWorkbenchTab: WorkbenchTab;
  terminalOpen: boolean;
  activeThreadId: string | null;

  setSidebarCollapsed: (v: boolean) => void;
  toggleSidebar: () => void;
  setActiveWorkbenchTab: (t: WorkbenchTab) => void;
  setTerminalOpen: (v: boolean) => void;
  toggleTerminal: () => void;
  setActiveThreadId: (id: string | null) => void;
}

export const useSiteStore = create<SiteState>((set) => ({
  sidebarCollapsed: false,
  activeWorkbenchTab: "editor",
  terminalOpen: false,
  activeThreadId: null,

  setSidebarCollapsed: (sidebarCollapsed) => set({ sidebarCollapsed }),
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
  setActiveWorkbenchTab: (activeWorkbenchTab) => set({ activeWorkbenchTab }),
  setTerminalOpen: (terminalOpen) => set({ terminalOpen }),
  toggleTerminal: () => set((s) => ({ terminalOpen: !s.terminalOpen })),
  setActiveThreadId: (activeThreadId) => set({ activeThreadId }),
}));
