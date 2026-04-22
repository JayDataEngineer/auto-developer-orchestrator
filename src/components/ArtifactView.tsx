import React from 'react';
import { MarkdownBlock } from './agent/MarkdownBlock';
import { Artifact } from '../lib/api';

interface ArtifactViewProps {
  artifact: Artifact;
}

export function ArtifactView({ artifact }: ArtifactViewProps) {
  if (artifact.type === 'todo') {
    return <TodoArtifact content={artifact.content} />;
  }
  return <MarkdownArtifact content={artifact.content} />;
}

function MarkdownArtifact({ content }: { content: string }) {
  return <MarkdownBlock content={content} streaming={false} />;
}

function TodoArtifact({ content }: { content: string }) {
  // Parse markdown todo list into items
  const lines = content.split('\n');
  const items = lines.map((line, i) => {
    const checkedMatch = line.match(/^- \[x\]\s*(.*)/i);
    const inProgressMatch = line.match(/^- \[>\]\s*(.*)/);
    const uncheckedMatch = line.match(/^- \[ \]\s*(.*)/);
    if (checkedMatch) {
      return { checked: true, inProgress: false, text: checkedMatch[1], key: i };
    }
    if (inProgressMatch) {
      return { checked: false, inProgress: true, text: inProgressMatch[1], key: i };
    }
    if (uncheckedMatch) {
      return { checked: false, inProgress: false, text: uncheckedMatch[1], key: i };
    }
    return null;
  }).filter(Boolean) as Array<{ checked: boolean; inProgress: boolean; text: string; key: number }>;

  if (items.length === 0) {
    // Not a todo list, render as markdown
    return <MarkdownArtifact content={content} />;
  }

  const completed = items.filter(i => i.checked).length;
  const inProgress = items.filter(i => i.inProgress).length;

  return (
    <div className="space-y-1">
      {/* Progress bar */}
      <div className="flex items-center gap-3 mb-3">
        <div className="flex-1 h-1 bg-muted rounded-full overflow-hidden">
          <div
            className="h-full bg-primary rounded-full transition-all"
            style={{ width: `${items.length > 0 ? (completed / items.length) * 100 : 0}%` }}
          />
        </div>
        <span className="text-xs font-mono text-muted-foreground">
          {completed}/{items.length}
          {inProgress > 0 && <span className="text-primary ml-1">({inProgress} active)</span>}
        </span>
      </div>

      {/* Items */}
      {items.map(item => (
        <div
          key={item.key}
          className="flex items-start gap-2 px-2 py-1"
        >
          <div className={`w-3.5 h-3.5 mt-0.5 border flex items-center justify-center shrink-0 ${
            item.checked ? 'bg-primary border-primary' : item.inProgress ? 'border-primary bg-primary/20' : 'border-muted-foreground/60'
          }`}>
            {item.checked && (
              <svg width="8" height="8" viewBox="0 0 8 8" fill="none" className="text-primary-foreground">
                <path d="M1 4L3 6L7 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            )}
            {item.inProgress && (
              <div className="w-1.5 h-1.5 bg-primary rounded-full" />
            )}
          </div>
          <span className={`text-xs font-mono leading-relaxed ${
            item.checked ? 'text-muted-foreground line-through' : item.inProgress ? 'text-primary' : 'text-foreground'
          }`}>
            {item.text}
          </span>
        </div>
      ))}
    </div>
  );
}
