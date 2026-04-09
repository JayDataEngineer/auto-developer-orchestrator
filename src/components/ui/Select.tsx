import React from 'react';
import { cn } from '../../lib/utils';

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {}

export function Select({ className, ...props }: SelectProps) {
  return (
    <select
      className={cn(
        'bg-zinc-900 border border-white/5 px-2 py-1.5 text-[11px] text-white outline-none focus:border-primary/40 transition-colors font-mono',
        className,
      )}
      {...props}
    />
  );
}
