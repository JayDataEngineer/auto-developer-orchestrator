import React, { useState, useCallback } from 'react';
import { Monitor, Maximize2, Minimize2, ExternalLink, Loader } from 'lucide-react';
import { Button } from './ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip';

interface ComputerViewerProps {
  sandboxId: string | null;
  enabled: boolean;
  loading: boolean;
  error: string | null;
}

export function ComputerViewer({ sandboxId, enabled, loading, error }: ComputerViewerProps) {
  const [fullscreen, setFullscreen] = useState(false);

  const novncUrl = sandboxId
    ? `/api/sandbox/vnc/${sandboxId}/vnc.html?path=api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&reconnect=true&reconnect_delay=3000&resize=scale`
    : null;

  const openExternal = useCallback(() => {
    if (novncUrl) window.open(novncUrl, '_blank', 'width=1280,height=720');
  }, [novncUrl]);

  if (!sandboxId) {
    return (
      <div className="h-full flex items-center justify-center bg-background">
        <div className="text-center">
          <Monitor size={40} className="mx-auto mb-3 text-muted-foreground/30" />
          <p className="text-sm text-muted-foreground">Select a project to start</p>
        </div>
      </div>
    );
  }

  if (loading || !enabled) {
    return (
      <div className="h-full flex items-center justify-center bg-background">
        <div className="text-center">
          <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-3" />
          <p className="text-sm text-muted-foreground">Starting computer...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="h-full flex items-center justify-center bg-background">
        <div className="text-center">
          <Monitor size={40} className="mx-auto mb-3 text-red-400/40" />
          <p className="text-sm text-muted-foreground">Computer not available</p>
          <p className="text-xs text-muted-foreground/60 mt-1">{error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className={fullscreen ? 'fixed inset-0 z-50 bg-background flex flex-col' : 'h-full flex flex-col'}>
      {/* Thin header */}
      <div className="h-7 bg-muted/30 border-b border-border flex items-center px-2 gap-2 shrink-0">
        <Monitor size={10} className="text-green-500" />
        <span className="text-[10px] font-mono text-muted-foreground truncate">{sandboxId}</span>
        <div className="flex-1" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-xs" onClick={() => setFullscreen(!fullscreen)}>
              {fullscreen ? <Minimize2 size={10} /> : <Maximize2 size={10} />}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{fullscreen ? 'Exit fullscreen' : 'Fullscreen'}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-xs" onClick={openExternal}>
              <ExternalLink size={10} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Open in new window</TooltipContent>
        </Tooltip>
      </div>

      {/* VNC iframe */}
      {novncUrl && (
        <iframe
          src={novncUrl}
          className="flex-1 w-full border-0"
          title="Computer"
          allow="clipboard-read; clipboard-write"
        />
      )}
    </div>
  );
}
