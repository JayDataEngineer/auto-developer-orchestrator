import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
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
  return (
    <div className="prose prose-invert prose-sm max-w-none
      prose-headings:text-white prose-headings:font-bold prose-headings:tracking-widest prose-headings:uppercase
      prose-p:text-zinc-300 prose-p:text-[12px] prose-p:leading-relaxed prose-p:font-mono
      prose-code:text-primary prose-code:bg-primary/10 prose-code:px-1 prose-code:rounded
      prose-pre:bg-zinc-950 prose-pre:border prose-pre:border-white/5 prose-pre:rounded-none
      prose-a:text-primary prose-a:no-underline hover:prose-a:underline
      prose-strong:text-white
      prose-ul:text-zinc-300 prose-ol:text-zinc-300
      prose-li:text-[12px] prose-li:font-mono
    ">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>
        {content}
      </ReactMarkdown>
    </div>
  );
}

function TodoArtifact({ content }: { content: string }) {
  // Parse markdown todo list into items
  const lines = content.split('\n');
  const items = lines.map((line, i) => {
    const checkedMatch = line.match(/^- \[x\]\s*(.*)/i);
    const uncheckedMatch = line.match(/^- \[ \]\s*(.*)/);
    if (checkedMatch) {
      return { checked: true, text: checkedMatch[1], key: i };
    }
    if (uncheckedMatch) {
      return { checked: false, text: uncheckedMatch[1], key: i };
    }
    return null;
  }).filter(Boolean) as Array<{ checked: boolean; text: string; key: number }>;

  if (items.length === 0) {
    // Not a todo list, render as markdown
    return <MarkdownArtifact content={content} />;
  }

  const completed = items.filter(i => i.checked).length;

  return (
    <div className="space-y-1">
      {/* Progress bar */}
      <div className="flex items-center gap-3 mb-3">
        <div className="flex-1 h-1 bg-zinc-800 rounded-full overflow-hidden">
          <div
            className="h-full bg-primary rounded-full transition-all"
            style={{ width: `${items.length > 0 ? (completed / items.length) * 100 : 0}%` }}
          />
        </div>
        <span className="text-[9px] font-mono text-muted-foreground">
          {completed}/{items.length}
        </span>
      </div>

      {/* Items */}
      {items.map(item => (
        <div
          key={item.key}
          className="flex items-start gap-2 px-2 py-1"
        >
          <div className={`w-3.5 h-3.5 mt-0.5 border flex items-center justify-center shrink-0 ${
            item.checked ? 'bg-primary border-primary' : 'border-zinc-600'
          }`}>
            {item.checked && (
              <svg width="8" height="8" viewBox="0 0 8 8" fill="none">
                <path d="M1 4L3 6L7 2" stroke="black" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            )}
          </div>
          <span className={`text-[11px] font-mono leading-relaxed ${
            item.checked ? 'text-muted-foreground line-through' : 'text-zinc-300'
          }`}>
            {item.text}
          </span>
        </div>
      ))}
    </div>
  );
}
