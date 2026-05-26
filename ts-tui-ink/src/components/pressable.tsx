/**
 * Pressable — reusable focus+press pattern from @assistant-ui/react-ink.
 *
 * Wraps Ink's useFocus + useInput into a single component that handles
 * focus management and Enter/Space activation. Replaces raw useFocus +
 * useInput combinations throughout the TUI.
 *
 * Copied from: @assistant-ui/react-ink primitives/internal/Pressable.tsx
 */

import type { ComponentProps, ReactNode } from "react";
import { Box, useFocus, useInput } from "ink";

export type PressableState = {
	isFocused: boolean;
	disabled: boolean;
};

export type PressableProps = Omit<ComponentProps<typeof Box>, "children"> & {
	children: ReactNode | ((state: PressableState) => ReactNode);
	onPress?: (() => void) | undefined;
	disabled?: boolean | undefined;
};

export function Pressable({
	children,
	onPress,
	disabled,
	...boxProps
}: PressableProps) {
	const isDisabled = !!disabled;
	const { isFocused } = useFocus({ isActive: !isDisabled });

	useInput(
		(input, key) => {
			if ((key.return || input === " ") && onPress) {
				onPress();
			}
		},
		{ isActive: isFocused && !isDisabled },
	);

	return (
		<Box {...boxProps}>
			{typeof children === "function"
				? children({ isFocused, disabled: isDisabled })
				: children}
		</Box>
	);
}
