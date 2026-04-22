import React, { useState, useEffect, useCallback } from 'react';
import { cn } from '../lib/utils';
import {
  Monitor, Maximize2, Minimize2,
  ExternalLink, Loader, AlertCircle
} from 'lucide-react';
import { usePuxAgentContext } from '../contexts/PuxAgentContext';
import { DesktopConsolePanel } from './DesktopConsolePanel';
import { api } from '../lib/api';
import { Button } from './ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

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
  const { state: puxState } = usePuxAgentContext();
  const [session, setSession] = useState<{
    mode?: string;
    cdpUrl?: string;
    vncUrl?: string;
    novncUrl?: string;
  } | null>(null);
  const [sessionLoading, setSessionLoading] = useState(false);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [desktopFull, setDesktopFull] = useState(false);

  // Fetch viewer info once computer use is enabled — polls until background setup completes
  useEffect(() => {
    if (!sandboxId || !cu.enabled) return;

    let cancelled = false;
    setSessionLoading(true);
    setSessionError(null);

    const pollViewer = async () => {
      const deadline = Date.now() + 60_000; // 60s — Docker pull + container start can be slow
      while (Date.now() < deadline && !cancelled) {
        try {
          const data = await api.sandbox.getViewer(sandboxId);
          if (!cancelled) {
            setSession(data);
            setSessionLoading(false);
          }
          return;
        } catch {
          // 404 or network error = background setup not done yet — keep polling
        }
        await new Promise(r => setTimeout(r, 2000)); // 2s between attempts
      }
      if (!cancelled) {
        setSessionError('Desktop session not found — background setup did not complete');
        setSessionLoading(false);
      }
    };

    pollViewer();
    return () => { cancelled = true; };
  }, [sandboxId, cu.enabled]);

  // Retry / explicit start — polls until background setup completes
  const startDesktop = useCallback(async () => {
    if (!sandboxId) return;
    setSessionLoading(true);
    setSessionError(null);
    const deadline = Date.now() + 60_000;
    while (Date.now() < deadline) {
      try {
        const data = await api.sandbox.getViewer(sandboxId);
        setSession(data);
        setSessionLoading(false);
        return;
      } catch { /* keep polling */ }
      await new Promise(r => setTimeout(r, 2000));
    }
    setSessionError('Failed to start desktop — timed out');
    setSession(null);
    setSessionLoading(false);
  }, [sandboxId]);

  // Build noVNC iframe URL — uses relative path so it inherits the page's origin.
  // noVNC defaults to connecting to the iframe's own host:port, which is correct
  // since the Vite proxy (or production server) forwards /api/ to the Go backend.
  const novncUrl = sandboxId
    ? `/api/sandbox/vnc/${sandboxId}/vnc.html?path=/api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&reconnect=true&reconnect_delay=3000&resize=scale`
    : null;

  const openDesktop = useCallback(() => {
    if (novncUrl) {
      window.open(novncUrl, '_blank', 'width=1280,height=720');
    }
  }, [novncUrl]);

  return (
    <div className={cn(
      'flex flex-col h-full',
      desktopFull && 'fixed inset-0 z-50 bg-background'
    )}>
      {/* Desktop header */}
      <div className="h-8 bg-muted/50 border-b border-border flex items-center px-3 gap-2 shrink-0">
        <Monitor size={12} className="text-green-500" />
        <span className="text-sm text-muted-foreground truncate">
          {sandboxId ? sandboxId : 'Select a project to start'}
        </span>
        <div className="flex-1" />
        {cu.loading && !sessionLoading && (
          <span className="text-xs font-mono text-muted-foreground flex items-center gap-1">
            <Loader size={8} className="animate-spin" /> Enabling...
          </span>
        )}
        {sessionLoading && (
          <span className="text-xs font-mono text-muted-foreground flex items-center gap-1">
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
            <span className="text-xs font-mono text-muted-foreground">
              Desktop
            </span>
            <div className="w-px h-3 bg-white/10" />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => setDesktopFull(!desktopFull)}
                >
                  {desktopFull ? <Minimize2 size={10} /> : <Maximize2 size={10} />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{desktopFull ? 'Exit fullscreen' : 'Fullscreen'}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={openDesktop}
                >
                  <ExternalLink size={10} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Open in new window</TooltipContent>
            </Tooltip>
          </>
        )}
      </div>

      {/* Desktop content */}
      <div className="flex-1 bg-background relative">
        {!sandboxId ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <Monitor size={48} className="mx-auto mb-4 text-muted-foreground/50" />
              <p className="text-sm font-mono text-muted-foreground">Select a project above to start your desktop</p>
              <p className="text-xs font-mono text-muted-foreground/50 mt-2">Your agent will control this environment</p>
            </div>
          </div>
        ) : cu.loading || sessionLoading ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-4" />
              <p className="text-sm font-mono text-muted-foreground">
                {cu.loading ? 'Enabling computer use...' : 'Starting desktop...'}
              </p>
            </div>
          </div>
        ) : cu.error || sessionError ? (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center max-w-md">
              <AlertCircle size={48} className="mx-auto mb-4 text-red-400/50" />
              <p className="text-sm font-mono text-foreground mb-2">Desktop not available</p>
              <p className="text-xs font-mono text-muted-foreground">{cu.error || sessionError}</p>
              <Button
                variant="default"
                size="xs"
                onClick={startDesktop}
                className="mt-4"
              >
                Retry
              </Button>
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
              <Loader size={24} className="mx-auto mb-3 text-muted-foreground animate-spin" />
              <p className="text-xs font-mono text-muted-foreground">Connecting to desktop...</p>
            </div>
          </div>
        ) : (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="text-center">
              <Monitor size={32} className="mx-auto mb-3 text-muted-foreground" />
              <p className="text-sm font-mono text-muted-foreground">Initializing...</p>
            </div>
          </div>
        )}
      </div>

      {/* Console panel showing desktop tool call history */}
      <DesktopConsolePanel toolCalls={puxState.toolCalls} />
    </div>
  );
}
