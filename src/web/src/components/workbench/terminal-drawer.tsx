import { useState, useCallback } from "react";
import { TerminalPanel } from "@/components/workbench/terminal-panel";
import { PlusIcon, XIcon } from "lucide-react";
import { cn } from "@/lib/utils";

interface TermTab {
	id: string;
	label: string;
}

let tabCounter = 0;

export function TerminalDrawer({ cwd, onClose }: { cwd?: string; onClose: () => void }) {
	const [tabs, setTabs] = useState<TermTab[]>([{ id: `term-${++tabCounter}`, label: "bash" }]);
	const [activeTab, setActiveTab] = useState<string>(tabs[0].id);

	const addTab = useCallback(() => {
		const id = `term-${++tabCounter}`;
		setTabs((prev) => [...prev, { id, label: "bash" }]);
		setActiveTab(id);
	}, []);

	const closeTab = useCallback(
		(id: string) => {
			setTabs((prev) => {
				const next = prev.filter((t) => t.id !== id);
				if (id === activeTab && next.length > 0) {
					setActiveTab(next[next.length - 1].id);
				} else if (next.length === 0) {
					onClose();
				}
				return next;
			});
		},
		[activeTab, onClose],
	);

	return (
		<div className="flex h-full flex-col border-t border-border">
			{/* Tab bar */}
			<div className="flex h-7 items-end border-b border-border bg-muted/20">
				{tabs.map((tab) => (
					<div
						key={tab.id}
						className={cn(
							"group/tab flex h-6 shrink-0 cursor-pointer items-center gap-1 border-r border-border px-2 text-[11px] transition-colors",
							tab.id === activeTab
								? "border-b-2 border-b-primary bg-background text-foreground"
								: "text-muted-foreground hover:bg-accent/50",
						)}
						onClick={() => setActiveTab(tab.id)}
					>
						<span>{tab.label}</span>
						{tabs.length > 1 && (
							<button
								onClick={(e) => {
									e.stopPropagation();
									closeTab(tab.id);
								}}
								className="rounded p-0.5 opacity-0 hover:bg-accent group-hover/tab:opacity-100"
							>
								<XIcon size={9} />
							</button>
						)}
					</div>
				))}
				<button
					onClick={addTab}
					className="ml-1 flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-accent-foreground"
					title="New terminal"
				>
					<PlusIcon size={11} />
				</button>
				<div className="flex-1" />
				<button
					onClick={onClose}
					className="mr-1 flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-accent-foreground"
					title="Close terminal"
				>
					<XIcon size={12} />
				</button>
			</div>
			{/* Terminal instances — only active one visible */}
			<div className="relative flex-1 overflow-hidden">
				{tabs.map((tab) => (
					<div
						key={tab.id}
						className={cn(
							"absolute inset-0",
							tab.id === activeTab ? "z-10" : "pointer-events-none z-0 opacity-0",
						)}
					>
						{tab.id === activeTab && <TerminalPanel cwd={cwd} />}
					</div>
				))}
			</div>
		</div>
	);
}
