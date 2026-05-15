"use client";

import { useCallback, useRef, useState } from "react";
import { useScrollLock } from "@assistant-ui/react";

const ANIMATION_DURATION = 200;

/**
 * Shared hook for controlled/uncontrolled collapsible open state
 * with scroll locking. Used by ReasoningRoot, ToolFallbackRoot, ToolGroupRoot.
 */
export function useCollapsibleRoot(
	defaultOpen: boolean,
	controlledOpen?: boolean,
	controlledOnOpenChange?: (open: boolean) => void,
) {
	const collapsibleRef = useRef<HTMLDivElement>(null);
	const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen);
	const lockScroll = useScrollLock(collapsibleRef, ANIMATION_DURATION);

	const isControlled = controlledOpen !== undefined;
	const isOpen = isControlled ? controlledOpen : uncontrolledOpen;

	const handleOpenChange = useCallback(
		(open: boolean) => {
			if (!open) {
				lockScroll();
			}
			if (!isControlled) {
				setUncontrolledOpen(open);
			}
			controlledOnOpenChange?.(open);
		},
		[lockScroll, isControlled, controlledOnOpenChange],
	);

	return {
		collapsibleRef,
		isOpen,
		handleOpenChange,
		animationStyle: {
			"--animation-duration": `${ANIMATION_DURATION}ms`,
		} as React.CSSProperties,
	};
}
