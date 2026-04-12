import React, { useState, useEffect, useCallback } from 'react';
import { cn } from '../lib/utils';
import {
  Monitor, Maximize2, Minimize2,
  ExternalLink, Loader, AlertCircle
} from 'lucide-react';

interface ComputerUseTabProps {
  selectedProject: string | null;
  sandboxId: string | null;
  cu: {
    enabled: boolean;
    loading: boolean;
    error: string | null;
    sandboxId: string | null;
  };
}

export function ComputerUseTab({ selectedProject, sandboxId, cu }: ComputerUseTabProps) {
  const [session, setSession] = useState<{
    mode?: string;
    cdpUrl?: string;
    vncUrl?: string;
    novncUrl?: string;
  } | null>(null);
  const [sessionLoading, setSessionLoading] = useState(false);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [desktopFull, setDesktopFull] = useState(false);

  // Fetch viewer info once computer use is enabled
  useEffect(() => {
    if (!sandboxId || !cu.enabled) return;

    let cancelled = false;
    setSessionLoading(true);
    setSessionError(null);

    const fetchViewer = async () => {
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

    fetchViewer();
    return () => { cancelled = true; };
  }, [sandboxId, cu.enabled]);

  // Retry / explicit start
  const startDesktop = useCallback(async () => {
    if (!sandboxId) return;
    setSessionLoading(true);
    setSessionError(null);
    try {
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
  }, [sandboxId]);

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
        <span className="text-sm font-mono text-zinc-400 truncate">
          {sandboxId ? sandboxId : 'Select a project to start'}
        </span>
        <div className="flex-1" />
        {cu.loading && !sessionLoading && (
          <span className="text-xs font-mono text-zinc-500 flex items-center gap-1">
            <Loader size={8} className="animate-spin" /> Enabling...
          </span>
        )}
        {sessionLoading && (
          <span className="text-xs font-mono text-zinc-500 flex items-center gap-1">
            <Loader size={8} className="animate-spin" /> Starting desktop...
          </span>
        )}
        {cu.error && (
          <span className="text-xs font-mono text-red-400 flex items-center gap-1">
            <AlertCircle size={8} /> {cu.error}
          </span>
        )}
        {sessionError && (
          <span className="text-xs font-mono text-red-400 flex items-center gap-1">
            <AlertCircle size={8} /> {sessionError}
          </span>
        )}
        {session && (
          <>
            <span className="text-xs font-mono text-zinc-600">
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
        ) : cu.loading || sessionLoading ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-4" />
              <p className="text-sm font-mono text-zinc-500">
                {cu.loading ? 'Enabling computer use...' : 'Starting desktop...'}
              </p>
            </div>
          </div>
        ) : cu.error || sessionError ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center max-w-md">
              <AlertCircle size={48} className="mx-auto mb-4 text-red-400/50" />
              <p className="text-sm font-mono text-zinc-400 mb-2">Desktop not available</p>
              <p className="text-xs font-mono text-zinc-600">{cu.error || sessionError}</p>
              <button
                onClick={startDesktop}
                className="mt-4 px-4 py-2 bg-primary text-black text-xs font-black uppercase tracking-widest hover:bg-primary/80 transition-colors"
              >
                Retry
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
        ) : cu.enabled && !session ? (
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
              <p className="text-sm font-mono text-zinc-500">Initializing...</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
