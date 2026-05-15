export interface RelativeTimeOptions {
    never?: string;   // fallback for undefined input, default ""
    now?: string;     // text for < 1 min, default "now"
    suffix?: string;  // " ago" or "", default ""
}

/**
 * Format an ISO timestamp as a relative time string.
 */
export function relativeTime(iso?: string, opts?: RelativeTimeOptions): string {
    if (!iso) return opts?.never ?? "";
    const diff = Date.now() - new Date(iso).getTime();
    if (isNaN(diff)) return opts?.never ?? "—";
    const mins = Math.floor(diff / 60000);
    if (mins < 1) return opts?.now ?? "now";
    const suffix = opts?.suffix ?? "";
    if (mins < 60) return `${mins}m${suffix}`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h${suffix}`;
    const days = Math.floor(hrs / 24);
    return `${days}d${suffix}`;
}
