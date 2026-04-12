import React from 'react';
import { cn } from '../../lib/utils';

interface SectionHeaderProps {
  icon?: React.ReactNode;
  label: string;
  action?: React.ReactNode;
  className?: string;
}

export function SectionHeader({ icon, label, action, className }: SectionHeaderProps) {
  return (
    <>
      <div className={cn('flex items-center gap-2 px-3 py-1.5 bg-zinc-950/30', className)}>
        {icon && <span className="text-muted-foreground">{icon}</span>}
        <span className="text-xs font-black uppercase tracking-widest text-muted-foreground">
          {label}
        </span>
        <div className="flex-1" />
        {action}
      </div>
    </>
  );
}
