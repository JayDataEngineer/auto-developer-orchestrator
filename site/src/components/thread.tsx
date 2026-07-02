// assistant-ui Thread driving pi-mono via @assistant-ui/react-pi.
// Built against @assistant-ui/react@^0.14.24 primitives API:
//   - Top-level UserMessage / AssistantMessage FCs passed to ThreadPrimitive.Messages
//   - MessagePrimitive.Parts (alias Content) for per-part rendering
//   - ThreadPrimitive.If running for the cancel button
//   - @radix-ui/react-tooltip directly (no TooltipPrimitive export)

import { type FC, useEffect, useRef, useState } from "react";
import {
  ActionBarPrimitive,
  BranchPickerPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
} from "@assistant-ui/react";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CopyIcon,
  PencilIcon,
  RefreshCwIcon,
  SquareIcon,
} from "lucide-react";
import { usePiRuntimeExtras, usePiSession } from "@assistant-ui/react-pi";
import { MarkdownText } from "./assistant-ui/markdown-text";
import { ToolFallback } from "./assistant-ui/tool-fallback";
import { useMessage } from "@assistant-ui/react";

export const Thread: FC = () => {
  return (
    <ThreadPrimitive.Root
      className="flex h-full flex-col bg-background text-sm"
      style={{ ["--thread-max-width" as string]: "44rem" } as React.CSSProperties}
    >
      <ThreadPrimitive.Viewport className="relative flex flex-1 flex-col overflow-x-hidden overflow-y-scroll px-4 pt-4">
        <ThreadPrimitive.Empty>
          <ThreadWelcome />
        </ThreadPrimitive.Empty>

        <ThreadPrimitive.Messages
          components={{
            UserMessage,
            AssistantMessage,
            EditComposer,
          }}
        />

        <ScrollToBottom />
      </ThreadPrimitive.Viewport>

      <Composer />
    </ThreadPrimitive.Root>
  );
};

// ── Welcome (empty state) ───────────────────────────────────────────────────

const ThreadWelcome: FC = () => {
  const session = usePiSession();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-4 py-12 text-center">
      <div className="text-3xl">π</div>
      <div className="text-lg font-semibold tracking-tight">pux site</div>
      <div className="max-w-md text-sm text-muted-foreground">
        assistant-ui driving pi-mono in-process. Ask anything — bash, file ops,
        subagents, the whole pi toolchain.
      </div>
      {session && (
        <div className="mt-1 text-xs text-muted-foreground/70">
          session: <code className="font-mono">{session.id.slice(0, 8)}</code>
        </div>
      )}
    </div>
  );
};

// ── User message ────────────────────────────────────────────────────────────

const UserMessage: FC = () => {
  return (
    <MessagePrimitive.Root className="mb-6 flex w-full justify-end">
      <MessagePrimitive.Parts
        components={{
          Text: ({ text }) => (
            <div className="max-w-[80%] rounded-lg bg-secondary px-3 py-2 text-secondary-foreground whitespace-pre-wrap break-words">
              {text}
            </div>
          ),
        }}
      />
    </MessagePrimitive.Root>
  );
};

// ── Assistant message ───────────────────────────────────────────────────────

const AssistantMessage: FC = () => {
  return (
    <MessagePrimitive.Root className="mb-6 w-full">
      <AssistantError />
      <MessagePrimitive.Parts
        components={{
          Text: () => (
            <div className="aui-md-wrap max-w-none">
              <MarkdownText />
            </div>
          ),
          Reasoning: ({ text }) => (
            <details className="mb-2 rounded-md border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
              <summary className="cursor-pointer select-none">reasoning</summary>
              <div className="mt-2 whitespace-pre-wrap">{text}</div>
            </details>
          ),
          tools: {
            Fallback: ToolFallback,
          },
        }}
      />

      <AssistantActionBar />
      <BranchPicker />
    </MessagePrimitive.Root>
  );
};

// Renders an inline error banner when the assistant turn ended in error
// (provider 401, network failure, etc.). The pi snapshot stores this on the
// assistant message itself as stopReason:"error" + errorMessage.
const AssistantError: FC = () => {
  const message = useMessage() as unknown as {
    message?: { stopReason?: string; errorMessage?: string };
  };
  const err = message?.message;
  if (!err || err.stopReason !== "error" || !err.errorMessage) return null;
  return (
    <div className="mb-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
      <div className="font-semibold">assistant error</div>
      <pre className="mt-1 whitespace-pre-wrap break-words font-mono text-[11px]">
        {err.errorMessage}
      </pre>
    </div>
  );
};

const AssistantActionBar: FC = () => {
  return (
    <ActionBarPrimitive.Root className="mt-2 flex items-center gap-1 text-muted-foreground">
      <ActionBarPrimitive.Copy asChild>
        <button
          type="button"
          className="flex size-7 items-center justify-center rounded-md hover:bg-accent hover:text-foreground"
          title="Copy"
        >
          <CopyIcon className="size-3.5" />
        </button>
      </ActionBarPrimitive.Copy>
      <ActionBarPrimitive.Reload asChild>
        <button
          type="button"
          className="flex size-7 items-center justify-center rounded-md hover:bg-accent hover:text-foreground"
          title="Retry"
        >
          <RefreshCwIcon className="size-3.5" />
        </button>
      </ActionBarPrimitive.Reload>
      <ActionBarPrimitive.Edit asChild>
        <button
          type="button"
          className="flex size-7 items-center justify-center rounded-md hover:bg-accent hover:text-foreground"
          title="Edit"
        >
          <PencilIcon className="size-3.5" />
        </button>
      </ActionBarPrimitive.Edit>
    </ActionBarPrimitive.Root>
  );
};

const BranchPicker: FC = () => {
  return (
    <BranchPickerPrimitive.Root
      hideWhenSingleBranch
      className="mt-1 flex items-center gap-1 text-xs text-muted-foreground"
    >
      <BranchPickerPrimitive.Previous asChild>
        <button className="hover:text-foreground">‹</button>
      </BranchPickerPrimitive.Previous>
      <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
      <BranchPickerPrimitive.Next asChild>
        <button className="hover:text-foreground">›</button>
      </BranchPickerPrimitive.Next>
    </BranchPickerPrimitive.Root>
  );
};

// ── Composer ────────────────────────────────────────────────────────────────

const Composer: FC = () => {
  const extras = usePiRuntimeExtras();

  return (
    <div className="border-t border-border px-4 py-3">
      {extras?.readiness && extras.readiness.state !== "ready" && (
        <div className="mb-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-1.5 text-xs text-destructive">
          {extras.readiness.message}
        </div>
      )}
      <ComposerPrimitive.Root className="flex items-end gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 focus-within:border-ring">
        <ComposerPrimitive.Input
          asChild
          submitMode="enter"
          rows={1}
        >
          <textarea
            placeholder="ask pi anything…  (Enter = send, Shift+Enter = newline, Cmd/Ctrl+Shift+Enter = steer)"
            className="flex-1 resize-none bg-transparent text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
            style={{ minHeight: "1.5rem", maxHeight: "10rem" }}
          />
        </ComposerPrimitive.Input>

        <ThreadPrimitive.If running>
          <ComposerPrimitive.Cancel asChild>
            <button
              className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground hover:text-foreground"
              title="Cancel"
            >
              <SquareIcon className="size-3.5" />
            </button>
          </ComposerPrimitive.Cancel>
        </ThreadPrimitive.If>

        <ThreadPrimitive.If running={false}>
          <ComposerPrimitive.Send asChild>
            <button
              className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground hover:opacity-90 disabled:opacity-40"
              title="Send"
            >
              <ArrowUpIcon className="size-4" />
            </button>
          </ComposerPrimitive.Send>
        </ThreadPrimitive.If>
      </ComposerPrimitive.Root>
      <div className="mt-1.5 flex items-center justify-between text-[10px] text-muted-foreground/70">
        <div>
          {extras?.contextUsage?.percent != null && (
            <span title="context window utilization">
              ctx {extras.contextUsage.percent.toFixed(0)}%
            </span>
          )}
        </div>
        <div>
          {extras?.readiness?.state === "ready" && extras.readiness.selection && (
            <span>
              {extras.readiness.selection.provider}/{extras.readiness.selection.modelId}
            </span>
          )}
        </div>
      </div>
    </div>
  );
};

// ── Edit composer (when user clicks edit on their own message) ─────────────

const EditComposer: FC = () => {
  return (
    <ComposerPrimitive.Root className="flex w-full max-w-[80%] flex-col gap-2 rounded-lg border border-ring bg-secondary px-3 py-2">
      <ComposerPrimitive.Input asChild>
        <textarea
          className="flex-1 resize-none bg-transparent text-secondary-foreground focus:outline-none"
          rows={1}
        />
      </ComposerPrimitive.Input>
      <div className="flex justify-end gap-2 text-xs">
        <ComposerPrimitive.Cancel asChild>
          <button className="rounded-md px-2 py-1 text-muted-foreground hover:text-foreground">
            cancel
          </button>
        </ComposerPrimitive.Cancel>
        <ComposerPrimitive.Send asChild>
          <button className="rounded-md bg-primary px-2 py-1 text-primary-foreground">
            save
          </button>
        </ComposerPrimitive.Send>
      </div>
    </ComposerPrimitive.Root>
  );
};

// ── Scroll-to-bottom button ─────────────────────────────────────────────────

const ScrollToBottom: FC = () => {
  const ref = useRef<HTMLDivElement>(null);
  const [show, setShow] = useState(false);

  useEffect(() => {
    const el = ref.current?.parentElement;
    if (!el) return;
    const onScroll = () => {
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
      setShow(!atBottom);
    };
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <div ref={ref} className="pointer-events-none sticky bottom-0 flex justify-center pb-2">
      {show && (
        <ThreadPrimitive.ScrollToBottom asChild>
          <button className="pointer-events-auto flex size-8 items-center justify-center rounded-full border border-border bg-background text-foreground shadow-md hover:bg-accent">
            <ArrowDownIcon className="size-4" />
          </button>
        </ThreadPrimitive.ScrollToBottom>
      )}
    </div>
  );
};
