import React from 'react';
import { cn } from '../../lib/utils';
import { Button } from './button';

interface EmptyStateProps {
  icon: React.ReactNode;
  title: string;
  description?: string;
  action?: { label: string; onClick: () => void };
  className?: string;
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center h-full text-center space-y-2 p-8', className)}>
      <div className="text-zinc-700 opacity-50">{icon}</div>
      <p className="text-sm font-mono text-zinc-600">{title}</p>
      {description && (
        <p className="text-xs font-mono text-zinc-700 max-w-xs">{description}</p>
      )}
      {action && (
        <Button
          variant="outline"
          size="xs"
          onClick={action.onClick}
          className="mt-2"
        >
          {action.label}
        </Button>
      )}
    </div>
  );
}
