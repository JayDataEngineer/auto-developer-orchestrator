import { useEffect, useState, useCallback } from "react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Shield, ShieldCheck, ShieldAlert, ShieldOff } from "lucide-react";

interface ToolPermission {
	tool: string;
	level: "auto" | "confirm" | "deny";
	reason?: string;
	risk?: string;
}

const LEVEL_META: Record<string, { label: string; icon: typeof Shield; color: string }> = {
	auto: { label: "Auto", icon: ShieldCheck, color: "text-green-500" },
	confirm: { label: "Confirm", icon: ShieldAlert, color: "text-yellow-500" },
	deny: { label: "Deny", icon: ShieldOff, color: "text-red-500" },
};

const RISK_COLORS: Record<string, string> = {
	low: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
	medium: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300",
	high: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
};

export function PermissionsPanel() {
	const [permissions, setPermissions] = useState<Record<string, ToolPermission>>({});
	const [loading, setLoading] = useState(true);
	const [saving, setSaving] = useState<string | null>(null);

	const fetchPermissions = useCallback(async () => {
		try {
			const resp = await fetch("/api/pux/tool-permissions");
			if (resp.ok) {
				setPermissions(await resp.json());
			}
		} catch {
			/* ignore */
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		fetchPermissions();
	}, [fetchPermissions]);

	const updatePermission = async (tool: string, level: string) => {
		setSaving(tool);
		try {
			const resp = await fetch("/api/pux/tool-permissions", {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ tool, level }),
			});
			if (resp.ok) {
				setPermissions((prev) => ({
					...prev,
					[tool]: { ...prev[tool], level: level as ToolPermission["level"] },
				}));
			}
		} catch {
			/* ignore */
		} finally {
			setSaving(null);
		}
	};

	if (loading) {
		return (
			<div className="flex items-center justify-center p-8 text-muted-foreground">
				Loading permissions...
			</div>
		);
	}

	const tools = Object.values(permissions).sort((a, b) => {
		const riskOrder: Record<string, number> = { high: 0, medium: 1, low: 2 };
		return (riskOrder[a.risk || "low"] ?? 2) - (riskOrder[b.risk || "low"] ?? 2);
	});

	return (
		<div className="flex flex-col gap-2 p-4">
			<div className="flex items-center gap-2 mb-2">
				<Shield className="h-4 w-4 text-muted-foreground" />
				<h3 className="text-sm font-semibold">Tool Permissions</h3>
			</div>
			<p className="text-xs text-muted-foreground mb-3">
				Set approval requirements per tool. "Confirm" pauses the agent and asks
				for your approval before executing.
			</p>
			{tools.map((perm) => {
				const meta = LEVEL_META[perm.level] || LEVEL_META.auto;
				const Icon = meta.icon;
				return (
					<div
						key={perm.tool}
						className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
					>
						<div className="flex items-center gap-2 min-w-0">
							<Icon className={cn("h-4 w-4 shrink-0", meta.color)} />
							<span className="text-sm font-mono truncate">
								{perm.tool}
							</span>
							{perm.risk && (
								<Badge
									variant="outline"
									className={cn("text-[10px] px-1.5 py-0", RISK_COLORS[perm.risk])}
								>
									{perm.risk}
								</Badge>
							)}
						</div>
						<Select
							value={perm.level}
							onValueChange={(v) => updatePermission(perm.tool, v)}
							disabled={saving === perm.tool}
						>
							<SelectTrigger className="w-28 h-7 text-xs">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="auto">
									<span className="flex items-center gap-1.5">
										<ShieldCheck className="h-3 w-3 text-green-500" /> Auto
									</span>
								</SelectItem>
								<SelectItem value="confirm">
									<span className="flex items-center gap-1.5">
										<ShieldAlert className="h-3 w-3 text-yellow-500" /> Confirm
									</span>
								</SelectItem>
								<SelectItem value="deny">
									<span className="flex items-center gap-1.5">
										<ShieldOff className="h-3 w-3 text-red-500" /> Deny
									</span>
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
				);
			})}
		</div>
	);
}
