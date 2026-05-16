/**
 * Format a tool result into preview lines.
 * @param maxLines Maximum lines to show (default 3). Excess lines produce a "+N more" indicator.
 */
export function formatToolResult(result: unknown, maxLines = 3): string[] {
    if (result === undefined || result === null) return [];
    let text: string;
    if (typeof result === "string") {
        // Try parsing JSON strings (e.g. load_spilled returns JSON-encoded content)
        try {
            const parsed = JSON.parse(result);
            if (typeof parsed === "object" && parsed !== null) {
                const obj = parsed as Record<string, unknown>;
                text = (obj.output as string) || (obj.content as string) || (obj.text as string) || (obj.result as string) || "";
                if (!text) text = result; // fallback to raw string
            } else {
                text = result;
            }
        } catch {
            text = result;
        }
    } else if (typeof result === "object") {
        const obj = result as Record<string, unknown>;
        text = (obj.output as string) || (obj.content as string) || (obj.text as string) || (obj.result as string) || "";
        if (!text) text = JSON.stringify(result);
    } else {
        text = JSON.stringify(result);
    }
    if (!text.trim()) return [];
    text = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
    const lines = text.split("\n").filter(l => l.trim());
    if (lines.length === 0) return [];
    if (lines.length <= maxLines) return lines;
    return [...lines.slice(0, maxLines), `... +${lines.length - maxLines} more lines`];
}
