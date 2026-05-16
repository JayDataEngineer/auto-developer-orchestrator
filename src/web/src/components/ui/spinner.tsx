import { LoaderPinwheel } from "lucide-react";
import { cn } from "@/lib/utils";

export function Spinner({ className }: { className?: string }) {
	return <LoaderPinwheel className={cn("animate-spin", className)} />;
}
