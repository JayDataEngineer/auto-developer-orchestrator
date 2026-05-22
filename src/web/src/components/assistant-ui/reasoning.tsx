"use client";

import { type ReactNode, useState } from "react";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@/components/ui/collapsible";
import { Brain, ChevronDownIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface ReasoningRootProps {
	defaultOpen?: boolean;
	children: ReactNode;
}

export function ReasoningRoot({ defaultOpen = false, children }: ReasoningRootProps) {
	const [open, setOpen] = useState(defaultOpen);
	return (
		<Collapsible open={open} onOpenChange={setOpen} className="border-b border-border mb-2">
			{children}
		</Collapsible>
	);
}

interface ReasoningTriggerProps {
	active?: boolean;
}

export function ReasoningTrigger({ active }: ReasoningTriggerProps) {
	return (
		<CollapsibleTrigger className="flex w-full items-center gap-2 px-2 py-1.5 text-xs transition-colors hover:bg-accent/30 group">
			<Brain size={12} className={cn("shrink-0", active ? "text-blue-500" : "text-muted-foreground")} />
			<span className="font-medium text-muted-foreground">Thinking</span>
			{active && <span className="text-muted-foreground">...</span>}
			<ChevronDownIcon
				size={10}
				className="shrink-0 text-muted-foreground transition-transform duration-150 -rotate-90 group-data-[state=open]:rotate-0"
			/>
		</CollapsibleTrigger>
	);
}

export function ReasoningContent({ children }: { children: ReactNode }) {
	return <CollapsibleContent>{children}</CollapsibleContent>;
}

export function ReasoningText({ children }: { children: ReactNode }) {
	return (
		<div className="px-2 pb-2 pl-6">
			<pre className="max-h-48 overflow-y-auto whitespace-pre-wrap rounded-md bg-muted/50 p-2 text-[11px] leading-relaxed text-foreground">
				{children}
			</pre>
		</div>
	);
}
