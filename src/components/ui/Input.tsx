import React from 'react';
import { cn } from '../../lib/utils';

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  monospace?: boolean;
}

export function Input({ monospace = true, className, ...props }: InputProps) {
  return (
    <input
      className={cn(
        'w-full bg-zinc-900 border border-white/5 px-2 py-1.5 text-xs text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-colors',
        monospace && 'font-mono',
        className,
      )}
      {...props}
    />
  );
}

interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  monospace?: boolean;
}

export function Textarea({ monospace = true, className, ...props }: TextareaProps) {
  return (
    <textarea
      className={cn(
        'w-full bg-zinc-900 border border-white/5 px-2 py-1.5 text-xs text-white placeholder-zinc-700 outline-none focus:border-primary/40 transition-colors resize-none',
        monospace && 'font-mono',
        className,
      )}
      {...props}
    />
  );
}
