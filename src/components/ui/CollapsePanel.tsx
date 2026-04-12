import React, { useState } from 'react';
import { cn } from '../../lib/utils';
import { ChevronLeft, ChevronRight } from 'lucide-react';

interface CollapsePanelProps {
  /** Side of the screen the panel is on */
  side: 'left' | 'right';
  /** Whether the panel is currently collapsed */
  collapsed: boolean;
  /** Called when the user toggles collapse */
  onToggle: () => void;
  /** Panel width class when expanded (e.g. 'w-80', 'w-96') */
  widthClass: string;
  /** Content rendered inside the panel */
  children: React.ReactNode;
  /** Header label shown at the top */
  label?: string;
  /** z-index offset from top for the toggle button */
  topOffset?: string;
  /** Whether to hide entirely (e.g. fullscreen mode) */
  hidden?: boolean;
}

export function CollapsePanel({
  side,
  collapsed,
  onToggle,
  widthClass,
  children,
  label,
  topOffset = 'calc(2.5rem + 0.5rem)',
  hidden = false,
}: CollapsePanelProps) {
  if (hidden) return null;

  const isLeft = side === 'left';

  return (
    <>
      {/* Panel */}
      {!collapsed && (
        <div className={cn(
          `${widthClass} flex flex-col shrink-0`,
          isLeft ? 'border-r border-white/5' : 'border-l border-white/5',
        )}>
          {label && (
            <div className="p-2 border-b border-white/5 flex items-center justify-between">
              <span className="text-xs font-black uppercase tracking-[0.2em] text-muted-foreground">
                {label}
              </span>
              <button onClick={onToggle} className="p-1 hover:bg-white/5 text-zinc-500">
                {isLeft ? <ChevronLeft size={10} /> : <ChevronLeft size={10} />}
              </button>
            </div>
          )}
          {children}
        </div>
      )}

      {/* Toggle button */}
      <button
        onClick={onToggle}
        className={cn(
          'absolute z-20 flex items-center justify-center w-4 h-12 bg-zinc-900 border border-white/5 text-zinc-500 hover:text-zinc-300 transition-colors',
          collapsed
            ? (isLeft ? 'left-0' : 'right-0')
            : (isLeft ? `left-[${widthClass.replace('w-', '')}]` : `right-[${widthClass.replace('w-', '')}]`),
        )}
        style={{ top: topOffset, ...(collapsed ? {} : isLeft ? { left: `var(--panel-width, 20rem)` } : { right: `var(--panel-width, 20rem)` }) }}
      >
        {isLeft
          ? (collapsed ? <ChevronRight size={10} /> : <ChevronLeft size={10} />)
          : (collapsed ? <ChevronLeft size={10} /> : <ChevronRight size={10} />)
        }
      </button>
    </>
  );
}
