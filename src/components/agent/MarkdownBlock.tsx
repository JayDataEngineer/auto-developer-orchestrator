import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism';

export function MarkdownBlock({ content, streaming }: { content: string; streaming: boolean }) {
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
