import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { usePuxStore } from "@/lib/pux-store";
import { X } from "lucide-react";
import { useEffect, useState } from "react";
import type { FieldConfig } from "./types";

// ── Field wrapper (label + input) ──

function Field({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
	return (
		<label className="flex flex-col gap-1">
			<span className="text-[10px] uppercase tracking-wider text-muted-foreground">
				{label}
				{required && <span className="text-red-400"> *</span>}
			</span>
			{children}
		</label>
	);
}

// ── Multiselect (Badge pills + toggle) ──

function MultiSelect({
	options,
	selected,
	onChange,
}: {
	options: { value: string; label: string }[];
	selected: string[];
	onChange: (vals: string[]) => void;
}) {
	const toggle = (val: string) => {
		if (selected.includes(val)) {
			onChange(selected.filter((v) => v !== val));
		} else {
			onChange([...selected, val]);
		}
	};

	return (
		<div className="flex flex-wrap gap-1.5">
			{options.map((opt) => {
				const active = selected.includes(opt.value);
				return (
					<button
						key={opt.value}
						type="button"
						onClick={() => toggle(opt.value)}
						className={active ? undefined : undefined}
					>
						<Badge variant={active ? "default" : "outline"} className="cursor-pointer text-[10px]">
							{active && <X className="mr-0.5 size-2.5" />}
							{opt.label}
						</Badge>
					</button>
				);
			})}
		</div>
	);
}

// ── Field renderer ──

export function FieldRenderer<T>({
	field,
	value,
	onChange,
}: {
	field: FieldConfig<T>;
	value: any;
	onChange: (val: any) => void;
}) {
	switch (field.type) {
		case "text":
			return (
				<Field label={field.label} required={field.required}>
					<Input
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value)}
						placeholder={field.placeholder}
						className="h-8 text-xs"
					/>
				</Field>
			);

		case "textarea":
			return (
				<Field label={field.label} required={field.required}>
					<Textarea
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value)}
						placeholder={field.placeholder}
						rows={field.rows ?? 3}
						className="min-h-16 resize-none text-xs"
					/>
				</Field>
			);

		case "select":
			return (
				<Field label={field.label} required={field.required}>
					<Select value={value || "__none__"} onValueChange={(v) => onChange(v === "__none__" ? "" : v)}>
						<SelectTrigger className="h-8 text-xs">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{field.options.map((opt) => (
								<SelectItem key={opt.value || "__none__"} value={opt.value || "__none__"} className="text-xs">
									{opt.label}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</Field>
			);

		case "multiselect":
			return (
				<Field label={field.label}>
					<MultiSelect
						options={field.options}
						selected={value ?? []}
						onChange={onChange}
					/>
				</Field>
			);

		case "number":
			return (
				<Field label={field.label}>
					<Input
						type="number"
						value={value ?? ""}
						onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
						placeholder={field.placeholder}
						min={field.min}
						max={field.max}
						className="h-8 text-xs"
					/>
				</Field>
			);

		case "model": {
			const models = usePuxStore((s) => s.modelList);
			const defaultLogic = usePuxStore((s) => s.defaultLogic);
			const defaultWorker = usePuxStore((s) => s.defaultWorker);
			const logicName = models.find((m) => m.id === defaultLogic)?.name || defaultLogic;
			const workerName = models.find((m) => m.id === defaultWorker)?.name || defaultWorker;
			return (
				<Field label={field.label}>
					<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
						<SelectTrigger className="h-8 text-xs">
							<SelectValue placeholder="Default" />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__default__" className="text-xs">Default (Worker)</SelectItem>
							{defaultLogic && defaultLogic !== defaultWorker && (
								<SelectItem value="__logic__" className="text-xs">Logic: {logicName}</SelectItem>
							)}
							{(defaultLogic || defaultWorker) && <SelectSeparator />}
							{models.map((m) => (
								<SelectItem key={m.id} value={m.id} className="text-xs">
									{m.name}{m.id === defaultWorker ? " (worker)" : m.id === defaultLogic ? " (logic)" : ""}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</Field>
			);
		}

		case "workers": {
			const [workerNames, setWorkerNames] = useState<string[]>([]);
			useEffect(() => {
				fetch("/api/workers/")
					.then((r) => r.json())
					.then((data) => {
						const seen = new Set<string>();
						const names: string[] = [];
						for (const w of (data.workers || [])) {
							const n = (w as any).name as string;
							if (n && n !== field.exclude && !seen.has(n)) {
								seen.add(n);
								names.push(n);
							}
						}
						setWorkerNames(names);
					})
					.catch(() => {});
			}, [field.exclude]);
			const options = workerNames.map((n) => ({ value: n, label: n }));
			return (
				<Field label={field.label}>
					<MultiSelect
						options={options}
						selected={value ?? []}
						onChange={onChange}
					/>
				</Field>
			);
		}

		case "worker": {
			const [workerList, setWorkerList] = useState<{name: string; hint: string; source: string}[]>([]);
			useEffect(() => {
				fetch("/api/workers/")
					.then((r) => r.json())
					.then((data) => {
						setWorkerList((data.workers || []).map((w: any) => ({
							name: w.name as string,
							hint: (w.hint || w.persona || "") as string,
							source: (w.source || "kernel") as string,
						})));
					})
					.catch(() => {});
			}, []);
			return (
				<Field label={field.label}>
					<Select value={value || "__default__"} onValueChange={(v) => onChange(v === "__default__" ? "" : v)}>
						<SelectTrigger className="h-8 text-xs">
							<SelectValue placeholder="Default (CTO)" />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value="__default__" className="text-xs">Default (CTO)</SelectItem>
							{workerList.map((w) => (
								<SelectItem key={`${w.source}-${w.name}`} value={w.name} className="text-xs">
									{w.name}{w.hint ? ` — ${w.hint.slice(0, 50)}` : ""}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</Field>
			);
		}

		default:
			return null;
	}
}
