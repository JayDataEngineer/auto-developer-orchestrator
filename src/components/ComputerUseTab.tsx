import React, { useState, useEffect, useCallback, useRef } from 'react';
import { cn } from '../lib/utils';
import { useComputerUse } from '../hooks/useComputerUse';
import { PiAgentView } from './PiAgentView';
import { RightPanel } from './RightPanel';
import { useArtifacts } from '../hooks/useArtifacts';
import {
  Monitor, Globe, ChevronLeft, ChevronRight, Maximize2, Minimize2,
  ExternalLink, Loader, AlertCircle, RefreshCw
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
  const [retryKey, setRetryKey] = useState(0);

  // Live screenshot state
  const [screenshotUrl, setScreenshotUrl] = useState<string | null>(null);
  const [screenshotInfo, setScreenshotInfo] = useState<{ url: string; title: string } | null>(null);
  const [screenshotPolling, setScreenshotPolling] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const sandboxId = selectedProject ? `sandbox-${selectedProject}` : null;
  const cu = useComputerUse();
  const artifactsHook = useArtifacts(selectedProject ? `${selectedProject}:${activeAgentId}` : null);

  // Single initialization flow: enable sandbox → fetch viewer info
  useEffect(() => {
    if (!sandboxId) return;

    let cancelled = false;
    setSessionLoading(true);
    setSessionError(null);
    setSession(null);

    const init = async () => {
      // Step 1: Ensure sandbox + browser mode is enabled
      if (!cu.enabled || cu.sandboxId !== sandboxId) {
        try {
          await cu.enableComputerUse(sandboxId);
        } catch (err) {
          if (!cancelled) {
            setSessionError(String(err));
            setSessionLoading(false);
          }
          return;
        }
      }

      // Step 2: Fetch viewer session info
      try {
        const res = await fetch(`/api/sandbox/${sandboxId}/viewer`);
        if (!res.ok) throw new Error('Desktop session not found');
        const data = await res.json();
        if (!cancelled) {
          setSession(data);
          setSessionLoading(false);
        }
      } catch (err: any) {
        if (!cancelled) {
          setSessionError(err.message);
          setSessionLoading(false);
        }
      }
    };

    init();
    return () => { cancelled = true; };
  }, [sandboxId, retryKey]);

  // Live screenshot polling
  const captureScreenshot = useCallback(async () => {
    if (!sandboxId) return;
    try {
      const res = await fetch(`/api/sandbox/${sandboxId}/computer-use/screenshot?describe=false&format=json`);
      if (!res.ok) return;
      const data = await res.json();
      if (data.image) {
        // Revoke previous blob URL
        if (screenshotUrl && screenshotUrl.startsWith('blob:')) {
          URL.revokeObjectURL(screenshotUrl);
        }
        const blob = await (await fetch(`data:image/png;base64,${data.image}`)).blob();
        const url = URL.createObjectURL(blob);
        setScreenshotUrl(url);
        setScreenshotInfo({ url: data.url || '', title: data.title || '' });
      }
    } catch {
      // Ignore polling errors
    }
  }, [sandboxId]);

  // Start/stop polling based on session state
  useEffect(() => {
    if (session && sandboxId && screenshotPolling) {
      captureScreenshot(); // Initial capture
      pollRef.current = setInterval(captureScreenshot, 2000);
      return () => {
        if (pollRef.current) clearInterval(pollRef.current);
      };
    } else {
      if (pollRef.current) clearInterval(pollRef.current);
    }
  }, [session, sandboxId, screenshotPolling, captureScreenshot]);

  // Auto-start polling when session is ready
  useEffect(() => {
    if (session && sandboxId && !screenshotPolling) {
      setScreenshotPolling(true);
    }
  }, [session, sandboxId]);

  // Cleanup blob URLs on unmount
  useEffect(() => {
    return () => {
      if (screenshotUrl && screenshotUrl.startsWith('blob:')) {
        URL.revokeObjectURL(screenshotUrl);
      }
    };
  }, []);

  const openDesktop = useCallback(() => {
    if (sandboxId) {
      window.open(`/api/sandbox/vnc/${sandboxId}/vnc.html?host=${window.location.hostname}&port=${window.location.port}&path=api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&resize=scale`, '_blank', 'width=1280,height=720');
    }
  }, [sandboxId]);

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
              {screenshotPolling && (
                <>
                  <div className="w-px h-3 bg-white/10" />
                  <button
                    onClick={captureScreenshot}
                    className="p-1 hover:bg-white/5 text-zinc-500 hover:text-zinc-300"
                    title="Refresh screenshot"
                  >
                    <RefreshCw size={10} />
                  </button>
                </>
              )}
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
                  onClick={() => setRetryKey(k => k + 1)}
                  className="mt-4 px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
                >
                  Start Desktop
                </button>
              </div>
            </div>
          )}
          {sandboxId && session && screenshotUrl && (
            <div className="absolute inset-0 flex flex-col">
              {/* Browser info bar */}
              {screenshotInfo && (screenshotInfo.url || screenshotInfo.title) && (
                <div className="h-7 bg-zinc-900 border-b border-white/5 flex items-center px-3 gap-2 shrink-0">
                  <Globe size={10} className="text-zinc-500 shrink-0" />
                  <span className="text-[9px] font-mono text-zinc-400 truncate">
                    {screenshotInfo.title || screenshotInfo.url}
                  </span>
                  {screenshotInfo.url && (
                    <span className="text-[8px] font-mono text-zinc-600 truncate">
                      {screenshotInfo.url}
                    </span>
                  )}
                </div>
              )}
              {/* Live screenshot */}
              <div className="flex-1 flex items-center justify-center p-2">
                <img
                  src={screenshotUrl}
                  alt="Desktop"
                  className="max-w-full max-h-full object-contain rounded shadow-lg"
                  style={{ imageRendering: 'auto' }}
                />
              </div>
            </div>
          )}
          {sandboxId && session && !screenshotUrl && (
            <div className="absolute inset-0 flex items-center justify-center">
              <div className="text-center">
                <Loader size={24} className="mx-auto mb-3 text-zinc-600 animate-spin" />
                <p className="text-xs font-mono text-zinc-500">Capturing desktop...</p>
              </div>
            </div>
          )}
          {sandboxId && !session?.novncUrl && !sessionLoading && !session && (
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
