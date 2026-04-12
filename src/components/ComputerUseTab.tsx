import React, { useState, useEffect, useCallback } from 'react';
import { cn } from '../lib/utils';
import { useComputerUse } from '../hooks/useComputerUse';
import {
  Monitor, Maximize2, Minimize2,
  ExternalLink, Loader, AlertCircle
} from 'lucide-react';

interface ComputerUseTabProps {
  selectedProject: string | null;
}

export function ComputerUseTab({ selectedProject }: ComputerUseTabProps) {
  const [session, setSession] = useState<{
    mode?: string;
    cdpUrl?: string;
    vncUrl?: string;
    novncUrl?: string;
  } | null>(null);
  const [sessionLoading, setSessionLoading] = useState(false);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [desktopFull, setDesktopFull] = useState(false);
  const [resolvedSandboxId, setResolvedSandboxId] = useState<string | null>(null);

  const cu = useComputerUse();

  // Resolve the real sandbox ID from the API
  useEffect(() => {
    if (!selectedProject) {
      setResolvedSandboxId(null);
      return;
    }

    let cancelled = false;
    const resolveSandboxId = async () => {
      try {
        const res = await fetch('/api/sandbox/');
        if (!res.ok || cancelled) return;
        const data = await res.json();
        const sandboxes = Array.isArray(data) ? data : (data.sandboxes || []);

        if (sandboxes.length === 0 || cancelled) return;

        const projectName = selectedProject;
        const match = sandboxes.find((s: any) =>
          s.id === projectName ||
          s.id === `sandbox-${projectName}` ||
          s.projectPath?.includes(projectName)
        );

        if (!cancelled) {
          setResolvedSandboxId(match ? match.id : sandboxes[0].id);
        }
      } catch {
        if (!cancelled) {
          setResolvedSandboxId(`sandbox-${selectedProject}`);
        }
      }
    };

    resolveSandboxId();
    return () => { cancelled = true; };
  }, [selectedProject]);

  const sandboxId = resolvedSandboxId;

  // Auto-enable computer use and fetch viewer info on mount
  useEffect(() => {
    if (!sandboxId) return;

    let cancelled = false;
    setSessionLoading(true);
    setSessionError(null);

    const init = async () => {
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
  }, [sandboxId]);

  // Retry / explicit start
  const startDesktop = useCallback(async () => {
    if (!sandboxId) return;
    setSessionLoading(true);
    setSessionError(null);
    try {
      await cu.enableComputerUse(sandboxId);
      const res = await fetch(`/api/sandbox/${sandboxId}/viewer`);
      if (!res.ok) throw new Error('Failed to start desktop');
      const data = await res.json();
      setSession(data);
    } catch (err: any) {
      setSessionError(err.message);
      setSession(null);
    } finally {
      setSessionLoading(false);
    }
  }, [sandboxId, cu]);

  // Build noVNC iframe URL
  const novncUrl = sandboxId
    ? `/api/sandbox/vnc/${sandboxId}/vnc.html?host=${window.location.hostname}&port=${window.location.port}&path=api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&resize=scale`
    : null;

  const openDesktop = useCallback(() => {
    if (novncUrl) {
      window.open(novncUrl, '_blank', 'width=1280,height=720');
    }
  }, [novncUrl]);

  return (
    <div className={cn(
      'flex flex-col h-full',
      desktopFull && 'fixed inset-0 z-50 bg-black'
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
              Desktop
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
        {!sandboxId ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <Monitor size={48} className="mx-auto mb-4 text-zinc-700" />
              <p className="text-sm font-mono text-zinc-500">Select a project above to start your desktop</p>
              <p className="text-xs font-mono text-zinc-700 mt-2">Your agent will control this environment</p>
            </div>
          </div>
        ) : sessionLoading ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-4" />
              <p className="text-sm font-mono text-zinc-500">Starting desktop...</p>
            </div>
          </div>
        ) : sessionError ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center max-w-md">
              <AlertCircle size={48} className="mx-auto mb-4 text-red-400/50" />
              <p className="text-sm font-mono text-zinc-400 mb-2">Desktop not available</p>
              <p className="text-xs font-mono text-zinc-600">{sessionError}</p>
              <button
                onClick={startDesktop}
                className="mt-4 px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                Start Desktop
              </button>
            </div>
          </div>
        ) : session && novncUrl ? (
          <iframe
            src={novncUrl}
            className="absolute inset-0 w-full h-full border-0"
            title="Desktop"
            allow="clipboard-read; clipboard-write"
          />
        ) : session ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <Loader size={24} className="mx-auto mb-3 text-zinc-600 animate-spin" />
              <p className="text-xs font-mono text-zinc-500">Connecting to desktop...</p>
            </div>
          </div>
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <Monitor size={32} className="mx-auto mb-3 text-zinc-600" />
              <p className="text-sm font-mono text-zinc-500">Desktop not available</p>
              <p className="text-xs font-mono text-zinc-700 mt-1">The sandbox may not be running</p>
              <button
                onClick={startDesktop}
                className="mt-4 px-4 py-2 bg-primary text-black text-[9px] font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                Start Desktop
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
