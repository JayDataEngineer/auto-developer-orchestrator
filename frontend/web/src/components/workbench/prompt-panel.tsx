import { useEffect, useState, useCallback } from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { ArrowLeft, ChevronRight, Loader2, RefreshCw, Save } from "lucide-react";

interface PromptSection {
	name: string;
	content: string;
}

export function PromptDetail({ onBack }: { onBack: () => void }) {
	const [sections, setSections] = useState<PromptSection[]>([]);
	const [loading, setLoading] = useState(true);
	const [openSections, setOpenSections] = useState<Set<string>>(new Set());
	const [sectionEdits, setSectionEdits] = useState<Map<string, string>>(new Map());
	const [dirtySections, setDirtySections] = useState<Set<string>>(new Set());
	const [savingSection, setSavingSection] = useState<string | null>(null);

	const fetchSections = useCallback(async () => {
		try {
			const resp = await fetch("/api/prompt-sections/");
			if (resp.ok) {
				const data = await resp.json();
				const fetched: PromptSection[] = data.sections || [];
				setSections(fetched);
				// Populate edit state from fetched content
				const edits = new Map<string, string>();
				for (const s of fetched) edits.set(s.name, s.content);
				setSectionEdits(edits);
				setDirtySections(new Set());
			}
		} catch { /* ignore */ } finally { setLoading(false); }
	}, []);

	useEffect(() => { fetchSections(); }, [fetchSections]);

	const toggleSection = (name: string) => {
		setOpenSections((prev) => {
			const next = new Set(prev);
			if (next.has(name)) {
				next.delete(name);
			} else {
				next.add(name);
			}
			return next;
		});
	};

	const updateEdit = (name: string, content: string) => {
		setSectionEdits((prev) => {
			const next = new Map(prev);
			next.set(name, content);
			return next;
		});
		const original = sections.find((s) => s.name === name)?.content;
		if (content !== original) {
			setDirtySections((prev) => new Set(prev).add(name));
		} else {
			setDirtySections((prev) => {
				const next = new Set(prev);
				next.delete(name);
				return next;
			});
		}
	};

	const saveSection = async (name: string) => {
		const content = sectionEdits.get(name);
		if (content === undefined) return;
		setSavingSection(name);
		try {
			const resp = await fetch(`/api/prompt-sections/${name}`, {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ content }),
			});
			if (resp.ok) {
				setSections((prev) =>
					prev.map((s) => s.name === name ? { ...s, content } : s)
				);
				setDirtySections((prev) => {
					const next = new Set(prev);
					next.delete(name);
					return next;
				});
			}
		} finally { setSavingSection(null); }
	};

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	if (sections.length === 0) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3">
				<span className="text-xs text-muted-foreground">No prompt sections found</span>
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col">
			{/* Header */}
			<div className="flex items-center gap-1.5 border-b border-border px-2 py-1.5">
				<Button variant="ghost" size="icon" className="size-6 shrink-0" onClick={onBack} title="Back to agents">
					<ArrowLeft size={12} />
				</Button>
				<span className="text-xs font-medium">Prompt Sections</span>
				<Button variant="ghost" size="icon" className="size-6 ml-auto" onClick={fetchSections} title="Refresh">
					<RefreshCw size={12} />
				</Button>
			</div>

			{/* Collapsible sections */}
			<div className="flex-1 overflow-y-auto">
				{sections.map((s) => {
					const isOpen = openSections.has(s.name);
					const isDirty = dirtySections.has(s.name);
					const editContent = sectionEdits.get(s.name) ?? s.content;
					const isSaving = savingSection === s.name;

					return (
						<Collapsible
							key={s.name}
							open={isOpen}
							onOpenChange={() => toggleSection(s.name)}
							className="border-b border-border"
						>
							<CollapsibleTrigger asChild>
								<button className="flex w-full items-center gap-1.5 px-3 py-2 text-xs hover:bg-accent/50 transition-colors">
									<ChevronRight
										size={12}
										className="shrink-0 text-muted-foreground transition-transform"
										style={{ transform: isOpen ? "rotate(90deg)" : "rotate(0deg)" }}
									/>
									<span className="font-medium capitalize">{s.name}</span>
									{isDirty && (
										<span className="ml-auto text-amber-400 text-[10px]">unsaved</span>
									)}
								</button>
							</CollapsibleTrigger>
							<CollapsibleContent>
								<div className="px-3 pb-3">
									<Textarea
										value={editContent}
										onChange={(e) => updateEdit(s.name, e.target.value)}
										className="resize-none rounded-md border border-border font-mono text-xs leading-relaxed focus-visible:ring-1 focus-visible:ring-ring"
										rows={8}
										placeholder="Section content..."
									/>
									<div className="mt-1.5 flex justify-end">
										<Button
											size="sm"
											className="h-6 text-[11px] gap-1"
											onClick={(e) => { e.stopPropagation(); saveSection(s.name); }}
											disabled={!isDirty || isSaving}
										>
											{isSaving ? (
												<Loader2 size={10} className="animate-spin" />
											) : (
												<Save size={10} />
											)}
											Save
										</Button>
									</div>
								</div>
							</CollapsibleContent>
						</Collapsible>
					);
				})}
			</div>
		</div>
	);
}
