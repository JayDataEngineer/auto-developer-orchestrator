import { useEffect, useRef } from "react";
import { usePuxStore } from "@/lib/pux-store";
import { PermissionsPanel } from "@/components/workbench/permissions-panel";
import { Palette, Shield, Check } from "lucide-react";
import { cn } from "@/lib/utils";

const THEMES = [
	{
		id: "default-dark",
		label: "Default Dark",
		description: "Custom dark theme with oklch colors",
	},
	{
		id: "assistant-ui",
		label: "Assistant UI",
		description: "Standard @assistant-ui/shadcn default theme",
	},
	{
		id: "mono",
		label: "Mono",
		description: "All gray, monochrome theme",
	},
] as const;

function applyTheme(id: string) {
	const root = document.documentElement;
	root.setAttribute("data-pux-theme", id);
}

export function SettingsPanel() {
	const theme = usePuxStore((s) => s.theme);
	const setTheme = usePuxStore((s) => s.setTheme);
	const appliedRef = useRef(theme);

	// Apply theme on mount and when store changes
	useEffect(() => {
		if (appliedRef.current !== theme) {
			appliedRef.current = theme;
		}
		applyTheme(theme);
	}, [theme]);

	return (
		<div className="flex flex-col gap-4 overflow-y-auto h-full">
			{/* ── Color Theme ── */}
			<div className="border-b border-border px-4 pb-4">
				<div className="flex items-center gap-2 mb-2 pt-3">
					<Palette className="h-4 w-4 text-muted-foreground" />
					<h3 className="text-sm font-semibold">Color Theme</h3>
				</div>
				<div className="flex flex-col gap-1.5">
					{THEMES.map((t) => {
						const active = theme === t.id;
						return (
							<button
								key={t.id}
								onClick={() => setTheme(t.id)}
								className={cn(
									"flex items-center gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
									active
										? "border-primary/50 bg-primary/10"
										: "border-border hover:bg-accent/50"
								)}
							>
								<div
									className={cn(
										"flex h-4 w-4 shrink-0 items-center justify-center rounded-full border",
										active ? "border-primary" : "border-muted-foreground/40"
									)}
								>
									{active && <Check className="h-2.5 w-2.5 text-primary" />}
								</div>
								<div className="flex flex-col min-w-0">
									<span className="text-sm font-medium">{t.label}</span>
									<span className="text-xs text-muted-foreground">{t.description}</span>
								</div>
							</button>
						);
					})}
				</div>
			</div>

			{/* ── Permissions (subgroup) ── */}
			<div>
				<div className="flex items-center gap-2 px-4 mb-1">
					<Shield className="h-4 w-4 text-muted-foreground" />
					<h3 className="text-sm font-semibold">Permissions</h3>
				</div>
				<PermissionsPanel embedded />
			</div>
		</div>
	);
}
