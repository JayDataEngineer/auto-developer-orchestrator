// Simple tool-call fallback renderer. Shows a compact one-line summary of
// the tool name + args, expands on click to show the full result.
// Handles running/complete/error states via the status field.

import { useState, type FC } from "react";
import {
  ChevronDownIcon,
  Loader2,
  CheckCircle2,
  XCircle,
  Wrench,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { ToolCallMessagePartComponent } from "@assistant-ui/react";

type Status = "running" | "complete" | "incomplete";

function statusType(s: unknown): Status {
  if (typeof s === "object" && s !== null && "type" in s) {
    const t = (s as { type: unknown }).type;
    if (t === "running" || t === "complete" || t === "incomplete") return t;
  }
  return "complete";
}

const StatusIcon: FC<{ status: Status }> = ({ status }) => {
  if (status === "running")
    return <Loader2 className="size-3 shrink-0 animate-spin text-blue-500" />;
  if (status === "incomplete")
    return <XCircle className="size-3 shrink-0 text-red-500" />;
  return <CheckCircle2 className="size-3 shrink-0 text-emerald-500" />;
};

function summarizeArgs(args: unknown, argsText?: string): string {
  if (args && typeof args === "object") {
    const entries = Object.entries(args as Record<string, unknown>);
    const parts = entries.slice(0, 3).map(([k, v]) => {
      const vs = typeof v === "string" ? v : JSON.stringify(v);
      const truncated = vs.length > 60 ? vs.slice(0, 57) + "…" : vs;
      return `${k}=${truncated}`;
    });
    if (entries.length > 3) parts.push(`+${entries.length - 3}`);
    return parts.join(" ");
  }
  if (argsText) return argsText.slice(0, 80);
  return "";
}

export const ToolFallback: ToolCallMessagePartComponent = ({
  toolName,
  args,
  argsText,
  result,
  status,
}) => {
  const [expanded, setExpanded] = useState(false);
  const st = statusType(status);
  const hasResult = result !== undefined && result !== null;
  const summary = summarizeArgs(args, argsText);
  const resultStr =
    typeof result === "string"
      ? result
      : result !== undefined && result !== null
        ? JSON.stringify(result, null, 2)
        : "";

  // Detect errors: status incomplete, or result string starting with Error:
  const isError =
    st === "incomplete" ||
    (typeof result === "string" &&
      (/^Error:/m.test(result) || result.includes("<tool_use_error>")));

  const cleanResult = isError
    ? resultStr.replace(/<\/?tool_use_error>/g, "").trim()
    : resultStr;

  return (
    <div className="group/tool my-1.5 rounded-md border border-border/60 bg-muted/20 text-xs">
      <button
        type="button"
        disabled={!hasResult && !isError}
        onClick={() => setExpanded((v) => !v)}
        className={cn(
          "flex w-full items-center gap-2 px-2.5 py-1.5 text-left",
          (hasResult || isError) && "cursor-pointer hover:bg-accent/40",
        )}
      >
        <StatusIcon status={st} />
        <Wrench className="size-3 shrink-0 text-muted-foreground" />
        <span
          className={cn(
            "font-mono font-medium",
            isError ? "text-red-500" : "text-foreground",
          )}
        >
          {toolName}
        </span>
        {summary && (
          <span className="truncate font-mono text-muted-foreground/80">
            {summary}
          </span>
        )}
        <span className="ml-auto flex items-center gap-1">
          {st === "running" && !hasResult && (
            <span className="text-muted-foreground">…</span>
          )}
          {(hasResult || isError) && (
            <ChevronDownIcon
              className={cn(
                "size-3 text-muted-foreground transition-transform",
                expanded ? "rotate-0" : "-rotate-90",
              )}
            />
          )}
        </span>
      </button>
      {(hasResult || isError) && expanded && (
        <div className="border-t border-border/60 px-2.5 py-2">
          {isError ? (
            <pre className="whitespace-pre-wrap rounded bg-red-500/5 p-2 font-mono text-[11px] text-red-400">
              {cleanResult}
            </pre>
          ) : (
            <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded bg-muted/40 p-2 font-mono text-[11px] text-muted-foreground">
              {cleanResult}
            </pre>
          )}
        </div>
      )}
    </div>
  );
};
