import React, { useState } from 'react';
import { Monitor, Globe, Loader, X } from 'lucide-react';
import { cn } from '../lib/utils';

interface SandboxModeButtonProps {
  sandboxId: string;
  mode: 'browser' | 'desktop';
  isActive: boolean;
  onActivate: () => void;
  onDeactivate: () => void;
  disabled?: boolean;
}

/**
 * Sandbox Mode Button
 * 
 * Allows user to enable browser mode (CDP) or desktop mode (VNC) for a sandbox.
 */
export function SandboxModeButton({
  sandboxId,
  mode,
  isActive,
  onActivate,
  onDeactivate,
  disabled = false,
}: SandboxModeButtonProps) {
  const [isActivating, setIsActivating] = useState(false);

  const handleClick = async () => {
    if (isActive) {
      onDeactivate();
      return;
    }

    setIsActivating(true);
    try {
      const endpoint = mode === 'browser' ? 'browser-mode' : 'desktop-mode';
      
      // Call backend to enable mode
      const response = await fetch(`/api/sandbox/${sandboxId}/${endpoint}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          reason: mode === 'browser' 
            ? 'User requested browser access for web automation' 
            : 'User requested full desktop access for GUI apps',
        }),
      });

      if (!response.ok) {
        throw new Error(`Failed to enable ${mode} mode`);
      }

      const data = await response.json();
      
      // Open viewer popup
      const popupUrl = `/sandbox/${sandboxId}/viewer`;
      const popup = window.open(
        popupUrl,
        `_sandbox_${mode}_${sandboxId}`,
        'width=1400,height=900,resizable=yes,scrollbars=yes'
      );

      if (popup) {
        popup.focus();
      }

      onActivate();
    } catch (error) {
      console.error(`Failed to enable ${mode} mode:`, error);
      alert(`Failed to enable ${mode} mode. Please try again.`);
    } finally {
      setIsActivating(false);
    }
  };

  const isBrowser = mode === 'browser';
  
  return (
    <button
      onClick={handleClick}
      disabled={disabled || isActivating}
      className={cn(
        'flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium transition-all',
        isActive
          ? 'bg-green-600 hover:bg-green-700 text-white'
          : 'bg-zinc-800 hover:bg-zinc-700 text-zinc-300',
        disabled && 'opacity-50 cursor-not-allowed'
      )}
      title={
        isActive 
          ? `${isBrowser ? 'Browser' : 'Desktop'} mode active - click to disable`
          : isBrowser
            ? 'Enable live browser mode (Xvfb + Chrome + VNC, ~300MB) - See actual browser window'
            : 'Enable full desktop mode (VNC + Xvfb, ~500MB) - For Telegram, etc.'
      }
    >
      {isActivating ? (
        <Loader size={14} className="animate-spin" />
      ) : isActive ? (
        <X size={14} />
      ) : isBrowser ? (
        <Globe size={14} />
      ) : (
        <Monitor size={14} />
      )}
      {isActive 
        ? (isBrowser ? 'Browser Active' : 'Desktop Active') 
        : (isBrowser ? '🌐 Browser Mode' : '💻 Desktop Mode')}
    </button>
  );
}

interface SandboxModeStatusProps {
  mode: 'browser' | 'desktop';
  isActive: boolean;
  viewerUrl?: string;
  onOpenViewer: () => void;
}

/**
 * Sandbox Mode Status Indicator
 */
export function SandboxModeStatus({
  mode,
  isActive,
  viewerUrl,
  onOpenViewer,
}: SandboxModeStatusProps) {
  if (!isActive) return null;

  const isBrowser = mode === 'browser';

  return (
    <div className="flex items-center gap-2 px-3 py-1.5 bg-green-900/30 border border-green-700/50 rounded-md">
      {isBrowser ? (
        <Globe size={14} className="text-green-400" />
      ) : (
        <Monitor size={14} className="text-green-400" />
      )}
      <span className="text-xs text-green-300">
        {isBrowser ? 'Browser Mode Active' : 'Desktop Mode Active'}
      </span>
      <button
        onClick={onOpenViewer}
        className="ml-2 px-2 py-0.5 bg-green-700 hover:bg-green-600 rounded text-xs text-white"
      >
        Open Viewer
      </button>
    </div>
  );
}
