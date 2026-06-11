"use client";

import { type ReactNode, useState } from "react";
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@/components/ui/collapsible";
import { CheckIcon, ChevronDownIcon, LoaderIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface ReasoningRootProps {
	defaultOpen?: boolean;
	children: ReactNode;
}

export function ReasoningRoot({ defaultOpen = false, children }: ReasoningRootProps) {
	const [open, setOpen] = useState(defaultOpen);
	return (
		<Collapsible open={open} onOpenChange={setOpen} className="border-l-2 border-muted-foreground/20 mb-2">
			{children}
		</Collapsible>
	);
}

interface ReasoningTriggerProps {
	active?: boolean;
}

export function ReasoningTrigger({ active }: ReasoningTriggerProps) {
	return (
		<CollapsibleTrigger className="flex w-full items-center gap-2 px-3 py-1.5 text-xs transition-colors hover:bg-accent/30 group">
			{active ? (
				<LoaderIcon size={12} className="shrink-0 animate-spin text-blue-400" />
			) : (
				<CheckIcon size={12} className="shrink-0 text-muted-foreground/60" />
			)}
			<span className={cn("font-medium", active ? "text-blue-400" : "text-muted-foreground")}>
				Thinking
			</span>
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
		<div className="px-3 pb-2 pl-5">
			<pre className="max-h-48 overflow-y-auto whitespace-pre-wrap rounded-md bg-muted/30 p-2 text-[11px] leading-relaxed text-muted-foreground">
				{children}
			</pre>
		</div>
	);
}
