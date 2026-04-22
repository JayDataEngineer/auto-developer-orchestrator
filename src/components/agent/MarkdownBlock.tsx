import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';

export const proseClasses = `prose prose-sm max-w-none
  prose-headings:text-foreground prose-headings:font-bold
  prose-p:text-foreground prose-p:text-sm prose-p:leading-relaxed
  prose-code:text-primary prose-code:bg-primary/10 prose-code:px-1 prose-code:rounded
  prose-pre:bg-muted prose-pre:border prose-pre:border-border prose-pre:rounded-lg
  prose-a:text-primary prose-a:no-underline hover:prose-a:underline
  prose-strong:text-foreground
  prose-ul:text-foreground prose-ol:text-foreground
  prose-li:text-sm`;

export function MarkdownBlock({ content, streaming }: { content: string; streaming: boolean }) {
  return (
    <div className={proseClasses}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code({ className, children, ...props }: any) {
            const match = /language-(\w+)/.exec(className || '');
            const code = String(children).replace(/\n$/, '');
            return match ? (
              <SyntaxHighlighter
                style={oneDark}
                language={match[1]}
                PreTag="div"
                customStyle={{
                  margin: 0,
                  padding: '12px',
                  fontSize: '11px',
                  background: '#09090b',
                  border: '1px solid rgba(255,255,255,0.05)',
                }}
              >
                {code}
              </SyntaxHighlighter>
            ) : (
              <code className={className} {...props}>{children}</code>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
      {streaming && (
        <span className="inline-block w-2 h-4 bg-primary/60 animate-pulse ml-0.5 align-text-bottom" />
      )}
    </div>
  );
}
