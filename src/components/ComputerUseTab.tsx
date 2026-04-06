import React, { useState, useEffect, useCallback } from 'react';
import { cn } from '../lib/utils';
import { useComputerUse } from '../hooks/useComputerUse';
import { PiAgentView } from './PiAgentView';
import { RightPanel } from './RightPanel';
import { useArtifacts } from '../hooks/useArtifacts';
import {
  Monitor, Globe, ChevronLeft, ChevronRight, Maximize2, Minimize2,
  ExternalLink, Loader, AlertCircle
} from 'lucide-react';

interface ComputerUseTabProps {
  selectedProject: string | null;
  projects: string[];
  refreshProjectData: () => void;
}

export function ComputerUseTab({ selectedProject, projects }: ComputerUseTabProps) {
  const [activeAgentId, setActiveAgentId] = useState('default');
  const [chatCollapsed, setChatCollapsed] = useState(false);
  const [rightPanelCollapsed, setRightPanelCollapsed] = useState(false);
  const [session, setSession] = useState<{
    mode?: string;
    cdpUrl?: string;
    vncUrl?: string;
    novncUrl?: string;
  } | null>(null);
  const [sessionLoading, setSessionLoading] = useState(false);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [desktopFull, setDesktopFull] = useState(false);

  const sandboxId = selectedProject ? `sandbox-${selectedProject}-${activeAgentId}` : null;
  const cu = useComputerUse();
  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);

  // Enable computer use when a project is selected
  useEffect(() => {
    if (sandboxId && !cu.enabled) {
      cu.enableComputerUse(sandboxId);
    }
  }, [sandboxId]);

  // Fetch session info for embedding noVNC
  useEffect(() => {
    console.log('[ComputerUseTab] Session fetch effect triggered', { sandboxId, cuEnabled: cu.enabled });
    if (!sandboxId) return;
    setSessionLoading(true);
    setSessionError(null);
    fetch(`/api/sandbox/${sandboxId}/viewer`)
      .then(res => {
        console.log('[ComputerUseTab] Fetch response status:', res.status);
        if (!res.ok) throw new Error('Desktop session not found');
        return res.json();
      })
      .then(data => {
        console.log('[ComputerUseTab] Session data received:', data);
        setSession(data);
        setSessionLoading(false);
      })
      .catch(err => {
        console.log('[ComputerUseTab] Fetch error:', err.message);
        setSessionError(err.message);
        setSessionLoading(false);
      });
  }, [sandboxId, cu.enabled]); // refetch when computer use becomes enabled

  // Try to enable computer use if no session exists
  useEffect(() => {
    console.log('[ComputerUseTab] Enable effect check', { sandboxId, cuEnabled: cu.enabled, sessionLoading, hasSession: !!session, hasError: !!sessionError, error: sessionError });
    if (!sandboxId || cu.enabled || sessionLoading) {
      console.log('[ComputerUseTab] Enable effect skipped - early return');
      return;
    }
    if (session) {
      console.log('[ComputerUseTab] Enable effect skipped - has session');
      return;
    }
    if (sessionError && sessionError !== 'Desktop session not found') {
      console.log('[ComputerUseTab] Enable effect skipped - real error:', sessionError);
      return;
    }
    // Session doesn't exist yet (no error or "not found") — create it
    if (!sessionError || sessionError === 'Desktop session not found') {
      console.log('[ComputerUseTab] Enabling computer use for:', sandboxId);
      cu.enableComputerUse(sandboxId).then(() => {
        console.log('[ComputerUseTab] Computer use enabled successfully');
      }).catch(err => {
        console.error('[ComputerUseTab] enableComputerUse failed:', err);
      });
    }
  }, [sandboxId, session, sessionError, sessionLoading, cu.enabled]);

  const openDesktop = useCallback(() => {
    if (sandboxId) {
      window.open(`/api/sandbox/vnc/${sandboxId}/vnc.html`, '_blank', 'width=1280,height=720');
    }
  }, [sandboxId]);

  // Proxy the noVNC URL through the backend so the browser can reach the container
  // noVNC reads path from URL params; we route everything through our proxy
  const novncProxyUrl = sandboxId ? `/api/sandbox/vnc/${sandboxId}/vnc.html?path=api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&resize=scale` : null;

  return (
    <div className="flex h-full bg-black text-slate-100 overflow-hidden">
      {/* Left: Narrow agent chat (collapsible) */}
      {!chatCollapsed && !desktopFull && (
        <div className="w-80 border-r border-white/5 flex flex-col shrink-0">
          <div className="p-2 border-b border-white/5 flex items-center justify-between">
            <span className="text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground">
              Agent Chat
            </span>
            <button onClick={() => setChatCollapsed(true)} className="p-1 hover:bg-white/5 text-zinc-500">
              <ChevronLeft size={10} />
            </button>
          </div>
          <div className="flex-1 overflow-hidden">
            <PiAgentView
              selectedProject={selectedProject || undefined}
              selectedAgentId={activeAgentId}
              projects={projects}
              isZenMode={false}
              onZenToggle={() => {}}
            />
          </div>
        </div>
      )}

      {/* Chat collapse toggle */}
      {!desktopFull && (
        <button
          onClick={() => setChatCollapsed(!chatCollapsed)}
          className={cn(
            'absolute z-20 flex items-center justify-center w-4 h-12 bg-zinc-900 border border-white/5 text-zinc-500 hover:text-zinc-300 transition-colors',
            chatCollapsed ? 'left-0' : 'left-80'
          )}
          style={{ top: 'calc(2.5rem + 0.5rem)' }}
        >
          {chatCollapsed ? <ChevronRight size={10} /> : <ChevronLeft size={10} />}
        </button>
      )}

      {/* Center: Full desktop / noVNC */}
      <div className={cn(
        'flex-1 flex flex-col min-w-0',
        desktopFull && 'absolute inset-0 z-10 bg-black'
      )}>
        {/* Desktop header */}
        <div className="h-8 bg-zinc-900/50 border-b border-white/5 flex items-center px-3 gap-2 shrink-0">
          <Monitor size={12} className="text-green-400" />
          <span className="text-[10px] font-mono text-zinc-400 truncate">
            {sandboxId ? sandboxId : 'Select a project to start'}
          </span>
          <div className="flex-1" />
          {/* Debug info */}
          <span className="text-[8px] font-mono text-zinc-700">
            sid={sandboxId} loaded={sessionLoading} session={!!session} err={!!sessionError} cu={cu.enabled}
          </span>
          {sessionLoading && (
            <span className="text-[8px] font-mono text-zinc-500 flex items-center gap-1">
              <Loader size={8} className="animate-spin" /> Starting desktop...
            </span>
          )}
          {sessionError && (
            <span className="text-[8px] font-mono text-red-400 flex items-center gap-1">
              <AlertCircle size={8} /> {sessionError}
            </span>
          )}
          {session && (
            <>
              <span className="text-[8px] font-mono text-zinc-600">
                {session.mode === 'browser' ? 'Browser Mode' : 'Desktop Mode'}
              </span>
              <div className="w-px h-3 bg-white/10" />
              <button
                onClick={() => setDesktopFull(!desktopFull)}
                className="p-1 hover:bg-white/5 text-zinc-500 hover:text-zinc-300"
                title={desktopFull ? 'Exit full screen' : 'Full screen'}
              >
                {desktopFull ? <Minimize2 size={10} /> : <Maximize2 size={10} />}
              </button>
              <button
                onClick={openDesktop}
                className="p-1 hover:bg-white/5 text-zinc-500 hover:text-zinc-300"
                title="Open in new window"
              >
                <ExternalLink size={10} />
              </button>
            </>
          )}
        </div>

        {/* Desktop content */}
        <div className="flex-1 bg-zinc-950 relative">
          {!sandboxId && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="text-center">
                <Monitor size={48} className="mx-auto mb-4 text-zinc-700" />
                <p className="text-sm font-mono text-zinc-500">Select a project above to start your desktop</p>
                <p className="text-xs font-mono text-zinc-700 mt-2">Your agent will control this environment</p>
              </div>
            </div>
          )}
          {sandboxId && sessionLoading && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="text-center">
                <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-4" />
                <p className="text-sm font-mono text-zinc-500">Starting desktop...</p>
              </div>
            </div>
          )}
          {sandboxId && sessionError && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="text-center max-w-md">
                <AlertCircle size={48} className="mx-auto mb-4 text-red-400/50" />
                <p className="text-sm font-mono text-zinc-400 mb-2">Desktop not available</p>
                <p className="text-xs font-mono text-zinc-600">{sessionError}</p>
                <button
                  onClick={() => {
                    if (sandboxId) cu.enableComputerUse(sandboxId);
                  }}
                  className="mt-4 px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
                >
                  Start Desktop
                </button>
              </div>
            </div>
          )}
          {sandboxId && session && novncProxyUrl && (
            <iframe
              src={novncProxyUrl}
              className="w-full h-full border-0"
              title="Desktop VNC Viewer"
            />
          )}
          {sandboxId && !session?.novncUrl && !sessionLoading && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="text-center">
                <Globe size={32} className="mx-auto mb-3 text-zinc-600" />
                <p className="text-sm font-mono text-zinc-500">VNC viewer not available</p>
                <p className="text-xs font-mono text-zinc-700 mt-1">Try opening in a new window</p>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Right: Browser / Artifacts Panel (collapsible) */}
      {!rightPanelCollapsed && !desktopFull && (
        <div className="w-80 border-l border-white/5 flex flex-col shrink-0">
          <div className="p-2 border-b border-white/5 flex items-center justify-between">
            <span className="text-[8px] font-black uppercase tracking-[0.2em] text-muted-foreground">
              Controls
            </span>
            <button onClick={() => setRightPanelCollapsed(true)} className="p-1 hover:bg-white/5 text-zinc-500">
              <ChevronLeft size={10} />
            </button>
          </div>
          <div className="flex-1 overflow-hidden">
            <RightPanel
              agentId={selectedProject ? `${selectedProject}:${activeAgentId}` : null}
              sandboxId={sandboxId}
              artifacts={artifactsHook.artifacts}
              artifactsLoading={artifactsHook.loading}
            />
          </div>
        </div>
      )}

      {/* Right panel collapse toggle */}
      {!desktopFull && (
        <button
          onClick={() => setRightPanelCollapsed(!rightPanelCollapsed)}
          className={cn(
            'absolute z-20 flex items-center justify-center w-4 h-12 bg-zinc-900 border border-white/5 text-zinc-500 hover:text-zinc-300 transition-colors',
            rightPanelCollapsed ? 'right-0' : 'right-80'
          )}
          style={{ top: 'calc(2.5rem + 0.5rem)' }}
        >
          {rightPanelCollapsed ? <ChevronLeft size={10} /> : <ChevronRight size={10} />}
        </button>
      )}
    </div>
  );
}
