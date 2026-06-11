"use client";

import {
	ComposerPrimitive,
	AttachmentPrimitive,
	MessagePrimitive,
} from "@assistant-ui/react";
import type { Attachment, CompleteAttachment } from "@assistant-ui/react";
import { PaperclipIcon, XIcon, FileIcon } from "lucide-react";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";

export function ComposerAddAttachment() {
	return (
		<ComposerPrimitive.AddAttachment asChild>
			<TooltipIconButton
				tooltip="Attach file"
				side="bottom"
				className="size-7 rounded-md text-muted-foreground hover:text-foreground"
			>
				<PaperclipIcon className="size-4" />
			</TooltipIconButton>
		</ComposerPrimitive.AddAttachment>
	);
}

export function ComposerAttachments() {
	return (
		<ComposerPrimitive.Attachments>
			{({ attachment }: { attachment: Attachment }) => (
				<AttachmentPrimitive.Root
					key={attachment.id}
					className="flex items-center gap-2 rounded-lg border border-border bg-muted/50 px-2 py-1.5 text-sm"
				>
					<span className="max-w-40 truncate text-muted-foreground">
						<AttachmentPrimitive.Name />
					</span>
					<AttachmentPrimitive.Remove asChild>
						<button className="text-muted-foreground hover:text-foreground">
							<XIcon className="size-3.5" />
						</button>
					</AttachmentPrimitive.Remove>
				</AttachmentPrimitive.Root>
			)}
		</ComposerPrimitive.Attachments>
	);
}

export function UserMessageAttachments() {
	return (
		<MessagePrimitive.Attachments>
			{({ attachment }: { attachment: CompleteAttachment }) => (
				<AttachmentPrimitive.Root
					key={attachment.id}
					className="flex items-center gap-2 rounded-lg border border-border px-2 py-1 text-xs text-muted-foreground"
				>
					<FileIcon className="size-3" />
					<span className="max-w-32 truncate">
						<AttachmentPrimitive.Name />
					</span>
				</AttachmentPrimitive.Root>
			)}
		</MessagePrimitive.Attachments>
	);
}
