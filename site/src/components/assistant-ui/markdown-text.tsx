// Markdown rendering for assistant message text parts.
// Uses @assistant-ui/react-markdown's MarkdownTextPrimitive which pulls text
// from the surrounding MessagePartContext automatically.
//
// Lighter than the legacy version: no react-shiki (2MB), no memoization tricks.
// Code blocks render with a simple mono font + a copy button. GFM tables,
// lists, links all work via remark-gfm.

import "@assistant-ui/react-markdown/styles/dot.css";

import {
  type CodeHeaderProps,
  MarkdownTextPrimitive,
} from "@assistant-ui/react-markdown";
import remarkGfm from "remark-gfm";
import { type FC, memo, useState } from "react";
import { CheckIcon, CopyIcon } from "lucide-react";
import { TooltipIconButton } from "./tooltip-icon-button.tsx";

function useCopyToClipboard(copiedDuration = 2000) {
  const [isCopied, setIsCopied] = useState(false);
  const copyToClipboard = (value: string) => {
    if (!value) return;
    navigator.clipboard.writeText(value).then(() => {
      setIsCopied(true);
      setTimeout(() => setIsCopied(false), copiedDuration);
    });
  };
  return { isCopied, copyToClipboard };
}

const CodeHeader: FC<CodeHeaderProps> = ({ language, code }) => {
  const { isCopied, copyToClipboard } = useCopyToClipboard();
  const onCopy = () => {
    if (!code || isCopied) return;
    copyToClipboard(code);
  };
  return (
    <div className="mt-2.5 flex items-center justify-between rounded-t-md border border-border border-b-0 bg-muted/60 px-3 py-1 text-[11px]">
      <span className="font-mono lowercase text-muted-foreground">
        {language || "text"}
      </span>
      <TooltipIconButton tooltip="Copy" onClick={onCopy} className="size-5">
        {!isCopied && <CopyIcon className="size-3" />}
        {isCopied && <CheckIcon className="size-3" />}
      </TooltipIconButton>
    </div>
  );
};

const MarkdownTextImpl = () => {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm]}
      className="aui-md"
      components={{
        CodeHeader,
        pre: ({ className, ...props }) => (
          <pre
            className={
              "overflow-x-auto rounded-b-md border border-border border-t-0 bg-muted/30 p-3 text-xs leading-relaxed text-foreground " +
              (className ?? "")
            }
            {...props}
          />
        ),
        code: ({ className, ...props }) => (
          <code
            className={
              "rounded border border-border/50 bg-muted/60 px-1 py-0.5 font-mono text-[0.85em] " +
              (className ?? "")
            }
            {...props}
          />
        ),
      }}
    />
  );
};

export const MarkdownText = memo(MarkdownTextImpl);
