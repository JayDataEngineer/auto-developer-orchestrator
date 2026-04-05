import React, { useEffect, useState } from 'react';
import { Monitor, Globe, Terminal, ExternalLink, Maximize, Minimize } from 'lucide-react';
import { cn } from '../lib/utils';

interface DesktopViewerPageProps {
  sandboxId: string;
}

/**
 * Desktop Viewer Popup
 * 
 * Browser Mode: Shows live Chrome browser via VNC (single pane)
 * Desktop Mode: Shows dual-pane - CDP viewer + Full desktop VNC
 */
export function DesktopViewerPage({ sandboxId }: DesktopViewerPageProps) {
  const [session, setSession] = useState<{
    cdpUrl?: string;
    vncUrl?: string;
    novncUrl?: string;
    viewerUrl?: string;
    mode?: 'browser' | 'desktop';
  } | null>(null);
  
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [leftPaneMaximized, setLeftPaneMaximized] = useState(false);
  const [rightPaneMaximized, setRightPaneMaximized] = useState(false);

  useEffect(() => {
    if (!sandboxId) return;

    // Fetch desktop session info
    fetch(`/api/sandbox/${sandboxId}/viewer`)
      .then((res) => {
        if (!res.ok) throw new Error('Desktop session not found');
        return res.json();
      })
      .then((data) => {
        setSession(data);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message);
        setLoading(false);
      });
  }, [sandboxId]);

  // Proxy noVNC through backend since the container isn't directly reachable
  const novncProxyUrl = (sandboxId: string) =>
    `/api/sandbox/vnc/${sandboxId}/vnc.html?path=/api/sandbox/vnc/${sandboxId}/websockify&autoconnect=true&resize=scale`;

  const openInNewWindow = (url: string) => {
    window.open(url, '_blank');
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-white">
        <div className="text-center">
          <div className="animate-spin w-8 h-8 border-2 border-primary border-t-transparent rounded-full mx-auto mb-4" />
          <p className="text-sm text-zinc-400">
            {session?.mode === 'browser' 
              ? 'Starting live browser...' 
              : 'Loading desktop environment...'}
          </p>
        </div>
      </div>
    );
  }

  if (error || !session) {
    return (
      <div className="flex items-center justify-center h-screen bg-zinc-950 text-white">
        <div className="text-center max-w-md">
          <h2 className="text-lg font-bold mb-2">Session Not Found</h2>
          <p className="text-sm text-zinc-400 mb-4">
            {error || 'No active session for this sandbox.'}
          </p>
          <p className="text-xs text-zinc-500">
            Enable Browser Mode or Desktop Mode from the dashboard to start a session.
          </p>
        </div>
      </div>
    );
  }

  // Browser Mode: Single pane VNC viewer (live Chrome)
  if (session.mode === 'browser') {
    return (
      <div className="h-screen bg-zinc-950 flex flex-col">
        {/* Header */}
        <header className="h-12 bg-zinc-900 border-b border-zinc-800 flex items-center px-4 gap-4">
          <Globe size={18} className="text-blue-400" />
          <h1 className="text-sm font-bold text-white">
            Browser Viewer - Sandbox {sandboxId}
          </h1>
          <div className="flex-1" />
          <div className="flex items-center gap-2 text-xs text-zinc-400">
            <Globe size={12} />
            <span>Live Browser (VNC)</span>
            <span className="mx-2">|</span>
            <span>CDP: {session.cdpUrl || 'N/A'}</span>
          </div>
        </header>

        {/* VNC Viewer - Live Chrome */}
        <div className="flex-1 flex flex-col">
          <div className="h-8 bg-zinc-900/50 border-b border-zinc-800 flex items-center justify-between px-3">
            <div className="flex items-center gap-2">
              <Globe size={14} className="text-blue-400" />
              <span className="text-xs font-medium text-zinc-300">Live Browser (VNC)</span>
              <span className="text-[10px] text-zinc-500">- Watch and interact with Chrome</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => openInNewWindow(session.novncUrl || '')}
                className="p-1 hover:bg-zinc-800 rounded"
                title="Open in new window"
              >
                <ExternalLink size={12} />
              </button>
            </div>
          </div>
          <div className="flex-1 bg-zinc-950">
            {session?.novncUrl ? (
              <iframe
                src={novncProxyUrl(sandboxId)}
                className="w-full h-full border-0"
                title="Browser VNC Viewer"
              />
            ) : (
              <div className="flex items-center justify-center h-full text-zinc-500 text-sm">
                Browser viewer not available
              </div>
            )}
          </div>
        </div>

        {/* Footer Status */}
        <footer className="h-6 bg-zinc-900 border-t border-zinc-800 flex items-center px-4 text-[10px] text-zinc-500">
          <span>Browser Mode Active (Live Chrome via VNC)</span>
          <span className="mx-2">•</span>
          <span>Sandbox: {sandboxId}</span>
          <span className="flex-1" />
          <span>Close this window to return to dashboard</span>
        </footer>
      </div>
    );
  }

  // Desktop Mode: Dual-pane (CDP + Full Desktop VNC)
  return (
    <div className="h-screen bg-zinc-950 flex flex-col">
      {/* Header */}
      <header className="h-12 bg-zinc-900 border-b border-zinc-800 flex items-center px-4 gap-4">
        <Monitor size={18} className="text-green-400" />
        <h1 className="text-sm font-bold text-white">
          Desktop Viewer - Sandbox {sandboxId}
        </h1>
        <div className="flex-1" />
        <div className="flex items-center gap-2 text-xs text-zinc-400">
          <Terminal size={12} />
          <span>Full Desktop (VNC)</span>
          <span className="mx-2">|</span>
          <span>VNC: {session.vncUrl || 'N/A'}</span>
        </div>
      </header>

      {/* Main Content - Dual Pane */}
      <div className="flex-1 flex overflow-hidden">
        {/* Left: Chrome CDP Viewer */}
        <div
          className={cn(
            'flex-1 border-r border-zinc-800 flex flex-col',
            leftPaneMaximized && 'flex-[2]',
            rightPaneMaximized && 'hidden'
          )}
        >
          <div className="h-8 bg-zinc-900/50 border-b border-zinc-800 flex items-center justify-between px-3">
            <div className="flex items-center gap-2">
              <Globe size={14} className="text-blue-400" />
              <span className="text-xs font-medium text-zinc-300">Chrome Browser (CDP)</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setLeftPaneMaximized(!leftPaneMaximized)}
                className="p-1 hover:bg-zinc-800 rounded"
                title={leftPaneMaximized ? 'Restore' : 'Maximize'}
              >
                {leftPaneMaximized ? <Minimize size={12} /> : <Maximize size={12} />}
              </button>
              <button
                onClick={() => openInNewWindow(session.cdpUrl || '')}
                className="p-1 hover:bg-zinc-800 rounded"
                title="Open in new window"
              >
                <ExternalLink size={12} />
              </button>
            </div>
          </div>
          <div className="flex-1 bg-zinc-950">
            {session?.cdpUrl ? (
              <iframe
                src={session.cdpUrl}
                className="w-full h-full border-0"
                title="Chrome CDP Viewer"
              />
            ) : (
              <div className="flex items-center justify-center h-full text-zinc-500 text-sm">
                Chrome CDP not available
              </div>
            )}
          </div>
        </div>

        {/* Right: Full Desktop VNC Viewer */}
        <div
          className={cn(
            'flex-1 flex flex-col',
            rightPaneMaximized && 'flex-[2]',
            leftPaneMaximized && 'hidden'
          )}
        >
          <div className="h-8 bg-zinc-900/50 border-b border-zinc-800 flex items-center justify-between px-3">
            <div className="flex items-center gap-2">
              <Terminal size={14} className="text-green-400" />
              <span className="text-xs font-medium text-zinc-300">Full Desktop (VNC)</span>
              <span className="text-[10px] text-zinc-500">(Telegram, etc.)</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setRightPaneMaximized(!rightPaneMaximized)}
                className="p-1 hover:bg-zinc-800 rounded"
                title={rightPaneMaximized ? 'Restore' : 'Maximize'}
              >
                {rightPaneMaximized ? <Minimize size={12} /> : <Maximize size={12} />}
              </button>
              <button
                onClick={() => openInNewWindow(session.novncUrl || '')}
                className="p-1 hover:bg-zinc-800 rounded"
                title="Open in new window"
              >
                <ExternalLink size={12} />
              </button>
            </div>
          </div>
          <div className="flex-1 bg-zinc-950">
            {session?.novncUrl ? (
              <iframe
                src={novncProxyUrl(sandboxId)}
                className="w-full h-full border-0"
                title="VNC Desktop Viewer"
              />
            ) : (
              <div className="flex items-center justify-center h-full text-zinc-500 text-sm">
                VNC desktop not available
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Footer Status */}
      <footer className="h-6 bg-zinc-900 border-t border-zinc-800 flex items-center px-4 text-[10px] text-zinc-500">
        <span>Desktop Mode Active (Full XFCE4 Environment)</span>
        <span className="mx-2">•</span>
        <span>Sandbox: {sandboxId}</span>
        <span className="flex-1" />
        <span>Close this window to return to dashboard</span>
      </footer>
    </div>
  );
}
