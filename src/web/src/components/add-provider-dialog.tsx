import { usePuxStore } from "@/lib/pux-store";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Sheet,
	SheetContent,
	SheetHeader,
	SheetTitle,
	SheetDescription,
} from "@/components/ui/sheet";
import { PlusIcon, TrashIcon, CheckCircle, XCircle, Loader2 } from "lucide-react";
import { useState, type FC } from "react";

interface ProviderForm {
	id: string;
	baseUrl: string;
	apiKey: string;
	models: { id: string; name: string; contextWindow: number; maxTokens: number }[];
}

const emptyModel = () => ({ id: "", name: "", contextWindow: 128000, maxTokens: 8192 });

const emptyForm = (): ProviderForm => ({
	id: "",
	baseUrl: "",
	apiKey: "",
	models: [emptyModel()],
});

export const AddProviderDialog: FC<{ open: boolean; onOpenChange: (v: boolean) => void }> = ({
	open,
	onOpenChange,
}) => {
	const addProvider = usePuxStore((s) => s.addProvider);
	const [form, setForm] = useState<ProviderForm>(emptyForm);
	const [saving, setSaving] = useState(false);
	const [error, setError] = useState("");
	const [testResult, setTestResult] = useState<"idle" | "testing" | "ok" | "fail">("idle");
	const [testDetail, setTestDetail] = useState("");

	const setField = (field: keyof ProviderForm, value: string) => {
		setForm((f) => ({ ...f, [field]: value }));
		setError("");
		setTestResult("idle");
	};

	const setModel = (i: number, field: string, value: string | number) => {
		setForm((f) => {
			const models = [...f.models];
			models[i] = { ...models[i], [field]: value };
			return { ...f, models };
		});
		setError("");
	};

	const addModel = () => setForm((f) => ({ ...f, models: [...f.models, emptyModel()] }));

	const removeModel = (i: number) =>
		setForm((f) => ({ ...f, models: f.models.filter((_, idx) => idx !== i) }));

	const testConnection = async () => {
		if (!form.baseUrl) {
			setTestResult("fail");
			setTestDetail("Base URL is required");
			return;
		}
		setTestResult("testing");
		setTestDetail("");
		try {
			const headers: Record<string, string> = { "Content-Type": "application/json" };
			if (form.apiKey) headers["Authorization"] = `Bearer ${form.apiKey}`;

			// Normalize: try /v1/models, then /models
			const base = form.baseUrl.replace(/\/+$/, "");
			const url = base.endsWith("/v1") || base.endsWith("/v1/")
				? `${base}/models`
				: `${base}/v1/models`;

			const resp = await fetch(url, { headers, signal: AbortSignal.timeout(10000) });
			if (resp.ok) {
				const data = await resp.json();
				const modelIds: string[] = (data?.data || data?.models || [])
					.map((m: any) => m.id || m.name)
					.filter(Boolean);
				setTestResult("ok");
				setTestDetail(modelIds.length > 0 ? `${modelIds.length} models available` : "Connected");
			} else {
				const text = await resp.text().catch(() => "");
				setTestResult("fail");
				setTestDetail(`${resp.status}: ${text.slice(0, 100) || resp.statusText}`);
			}
		} catch (e: any) {
			setTestResult("fail");
			setTestDetail(e?.message || "Connection failed");
		}
	};

	const submit = async () => {
		if (!form.id || !form.baseUrl) {
			setError("Provider name and base URL are required");
			return;
		}
		if (form.models.length === 0 || !form.models.some((m) => m.id)) {
			setError("At least one model is required");
			return;
		}
		setSaving(true);
		try {
			await addProvider({
				id: form.id,
				baseUrl: form.baseUrl,
				apiKey: form.apiKey,
				models: form.models.filter((m) => m.id),
			});
			onOpenChange(false);
			setForm(emptyForm());
			setTestResult("idle");
		} catch {
			setError("Failed to add provider");
		} finally {
			setSaving(false);
		}
	};

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent side="right" className="sm:max-w-md overflow-y-auto">
				<SheetHeader>
					<SheetTitle>Add Provider</SheetTitle>
					<SheetDescription>
						Connect an OpenAI-compatible API endpoint.
					</SheetDescription>
				</SheetHeader>

				<div className="mt-6 flex flex-col gap-4 px-1">
					<label className="flex flex-col gap-1.5 text-sm">
						<span className="font-medium">Name</span>
						<Input
							placeholder="e.g. openrouter"
							value={form.id}
							onChange={(e) => setField("id", e.target.value)}
						/>
					</label>
					<label className="flex flex-col gap-1.5 text-sm">
						<span className="font-medium">Base URL</span>
						<Input
							placeholder="e.g. https://openrouter.ai/api/v1"
							value={form.baseUrl}
							onChange={(e) => setField("baseUrl", e.target.value)}
						/>
					</label>
					<label className="flex flex-col gap-1.5 text-sm">
						<span className="font-medium">API Key</span>
						<Input
							type="password"
							placeholder="sk-..."
							value={form.apiKey}
							onChange={(e) => setField("apiKey", e.target.value)}
						/>
					</label>

					{/* Test connection */}
					<div className="flex items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							onClick={testConnection}
							disabled={testResult === "testing" || !form.baseUrl}
							className="h-8 gap-1.5 text-xs"
						>
							{testResult === "testing" ? (
								<Loader2 className="size-3 animate-spin" />
							) : testResult === "ok" ? (
								<CheckCircle className="size-3 text-green-500" />
							) : testResult === "fail" ? (
								<XCircle className="size-3 text-red-500" />
							) : null}
							Test Connection
						</Button>
						{testDetail && (
							<span className={`text-xs ${testResult === "ok" ? "text-green-600" : testResult === "fail" ? "text-red-500" : "text-muted-foreground"}`}>
								{testDetail}
							</span>
						)}
					</div>

					<div className="flex flex-col gap-3 pt-2">
						<div className="flex items-center justify-between">
							<span className="text-sm font-medium">Models</span>
							<Button variant="ghost" size="sm" onClick={addModel} className="h-7 gap-1 text-xs">
								<PlusIcon className="size-3" />
								Add model
							</Button>
						</div>

						{form.models.map((m, i) => (
							<div key={i} className="flex items-start gap-2">
								<div className="flex flex-1 flex-col gap-2 rounded-md border border-border p-3">
									<Input
										placeholder="Model ID (e.g. gpt-4o)"
										value={m.id}
										onChange={(e) => setModel(i, "id", e.target.value)}
										className="h-8 text-sm"
									/>
									<Input
										placeholder="Display name (optional)"
										value={m.name}
										onChange={(e) => setModel(i, "name", e.target.value)}
										className="h-8 text-sm"
									/>
								</div>
								{form.models.length > 1 && (
									<Button
										variant="ghost"
										size="icon"
										className="mt-1 size-8 shrink-0 text-muted-foreground"
										onClick={() => removeModel(i)}
									>
										<TrashIcon className="size-3.5" />
									</Button>
								)}
							</div>
						))}
					</div>

					{error && <p className="text-sm text-destructive">{error}</p>}

					<Button onClick={submit} disabled={saving} className="mt-2">
						{saving ? "Saving..." : "Add Provider"}
					</Button>
				</div>
			</SheetContent>
		</Sheet>
	);
};
