import { useEffect, useState, useCallback } from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Loader2, RefreshCw, Save, FileText } from "lucide-react";

interface PromptSection {
	name: string;
	content: string;
}

export function PromptPanel() {
	const [sections, setSections] = useState<PromptSection[]>([]);
	const [loading, setLoading] = useState(true);
	const [activeIdx, setActiveIdx] = useState(0);
	const [editContent, setEditContent] = useState("");
	const [saving, setSaving] = useState(false);
	const [dirty, setDirty] = useState(false);

	const fetchSections = useCallback(async () => {
		try {
			const resp = await fetch("/api/prompt-sections/");
			if (resp.ok) {
				const data = await resp.json();
				setSections(data.sections || []);
			}
		} catch { /* ignore */ } finally { setLoading(false); }
	}, []);

	useEffect(() => { fetchSections(); }, [fetchSections]);

	// When sections load or active tab changes, update editor
	useEffect(() => {
		if (sections.length > 0 && sections[activeIdx]) {
			setEditContent(sections[activeIdx].content);
			setDirty(false);
		}
	}, [sections, activeIdx]);

	const save = async () => {
		if (sections.length === 0) return;
		const section = sections[activeIdx];
		setSaving(true);
		try {
			const resp = await fetch(`/api/prompt-sections/${section.name}`, {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ content: editContent }),
			});
			if (resp.ok) {
				// Update local state
				setSections((prev) =>
					prev.map((s, i) => i === activeIdx ? { ...s, content: editContent } : s)
				);
				setDirty(false);
			}
		} finally { setSaving(false); }
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
				<FileText className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">No prompt sections found</span>
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col">
			{/* Header with section tabs */}
			<div className="flex items-center gap-1 border-b border-border px-2 py-1.5">
				{sections.map((s, i) => (
					<button
						key={s.name}
						onClick={() => { if (dirty) { setEditContent(sections[i].content); setDirty(false); } setActiveIdx(i); }}
						className={cn(
							"rounded-md px-2 py-1 text-[10px] font-medium transition-colors",
							i === activeIdx
								? "bg-accent text-accent-foreground"
								: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
						)}
					>
						{s.name}
						{dirty && i === activeIdx && <span className="ml-1 text-amber-400">●</span>}
					</button>
				))}
				<div className="ml-auto flex items-center gap-1">
					<Button variant="ghost" size="icon" className="size-6" onClick={fetchSections} title="Refresh">
						<RefreshCw size={12} />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						className={cn("size-6", dirty && "text-amber-400")}
						onClick={save}
						disabled={!dirty || saving}
						title="Save"
					>
						{saving ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
					</Button>
				</div>
			</div>

			{/* Editor */}
			<Textarea
				value={editContent}
				onChange={(e) => { setEditContent(e.target.value); setDirty(true); }}
				className="flex-1 resize-none rounded-none border-0 font-mono text-xs leading-relaxed focus-visible:ring-0"
				placeholder="Section content..."
			/>
		</div>
	);
}
