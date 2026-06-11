/**
 * Get a compact argument preview for common tools.
 */
export function getToolArgPreview(toolName: string, args?: Record<string, unknown>, maxLen = 60): string {
    if (!args) return "";
    const entries = Object.entries(args);
    if (entries.length === 0) return "";

    if (["bash", "shell", "run_command"].includes(toolName)) {
        const cmd = (args.command as string) || (args.cmd as string) || "";
        if (cmd) return cmd.length > maxLen ? cmd.slice(0, maxLen - 3) + "..." : cmd;
    }

    if (["delegate_to", "delegate_async"].includes(toolName)) {
        return (args.agent as string) || "";
    }

    if (["file_read", "file_write", "file_edit"].includes(toolName)) {
        const path = (args.path as string) || (args.file_path as string) || "";
        if (path) return path.length > maxLen ? path.slice(0, maxLen - 3) + "..." : path;
    }

    const firstVal = entries[0]?.[1];
    if (firstVal && typeof firstVal === "string") {
        return firstVal.length > maxLen ? firstVal.slice(0, maxLen - 3) + "..." : firstVal;
    }

    return entries.length <= 2
        ? entries.map(([k, v]) => {
            const val = typeof v === "string" ? v.slice(0, 30) : JSON.stringify(v)?.slice(0, 30);
            return `${k}: ${val}`;
        }).join(", ")
        : `${entries.length} args`;
}
