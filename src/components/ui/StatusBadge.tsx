import React from 'react';
import { cn } from '../../lib/utils';

type StatusType = 'pending' | 'in_progress' | 'running' | 'completed' | 'complete' | 'failed' | 'error' | 'aborted' | 'idle' | 'disabled' | 'success' | 'ok';

interface StatusBadgeProps {
  status: StatusType;
  label?: string;
  size?: 'sm' | 'md';
}

const STATUS_COLORS: Record<string, string> = {
  pending: 'bg-muted-foreground/60',
  in_progress: 'bg-yellow-400 animate-pulse',
  running: 'bg-yellow-400 animate-pulse',
  completed: 'bg-emerald-400',
  complete: 'bg-emerald-400',
  success: 'bg-emerald-400',
  ok: 'bg-emerald-400',
  failed: 'bg-red-400',
  error: 'bg-red-400',
  aborted: 'bg-muted-foreground/50',
  idle: 'bg-muted-foreground/60',
  disabled: 'bg-muted-foreground/60',
};

const STATUS_TEXT_COLORS: Record<string, string> = {
  pending: 'text-muted-foreground/60',
  in_progress: 'text-yellow-400',
  running: 'text-yellow-400',
  completed: 'text-emerald-400',
  complete: 'text-emerald-400',
  success: 'text-emerald-400',
  ok: 'text-emerald-400',
  failed: 'text-red-400',
  error: 'text-red-400',
  aborted: 'text-muted-foreground/60',
  idle: 'text-muted-foreground/60',
  disabled: 'text-muted-foreground/60',
};

export function StatusBadge({ status, label, size = 'sm' }: StatusBadgeProps) {
  const dotSize = size === 'sm' ? 'w-1.5 h-1.5' : 'w-2 h-2';
  const textSize = size === 'sm' ? 'text-xs' : 'text-xs';

  return (
    <div className="flex items-center gap-1.5">
      <div className={cn('rounded-full shrink-0', dotSize, STATUS_COLORS[status] || 'bg-muted-foreground/60')} />
      {(label || status) && (
        <span className={cn('font-medium', textSize, STATUS_TEXT_COLORS[status] || 'text-muted-foreground/60')}>
          {label || status}
        </span>
      )}
    </div>
  );
}
