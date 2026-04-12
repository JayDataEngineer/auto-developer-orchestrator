import React from 'react';
import { cn } from '../../lib/utils';

type ButtonVariant = 'primary' | 'ghost' | 'danger' | 'muted';
type ButtonSize = 'sm' | 'xs';

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
}

const VARIANT_STYLES: Record<ButtonVariant, string> = {
  primary:
    'bg-primary text-black hover:bg-primary/80 disabled:opacity-30',
  ghost:
    'bg-white/5 text-muted hover:text-zinc-300 hover:bg-white/10 disabled:opacity-30',
  danger:
    'text-red-400/50 hover:text-red-400 hover:bg-red-400/5 disabled:opacity-30',
  muted:
    'text-muted hover:text-muted-foreground disabled:opacity-30',
};

const SIZE_STYLES: Record<ButtonSize, string> = {
  sm: 'px-3 py-1.5 text-xs',
  xs: 'px-2 py-1 text-xs',
};

export function Button({
  variant = 'ghost',
  size = 'sm',
  className,
  children,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cn(
        'font-mono uppercase tracking-widest transition-colors inline-flex items-center gap-1',
        VARIANT_STYLES[variant],
        SIZE_STYLES[size],
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}
