/**
 * ErrorMessage — renders error states using ErrorPrimitive.
 *
 * Uses ErrorPrimitive.Root and ErrorPrimitive.Message
 * from assistant-ui for consistent error rendering.
 */

import React from "react";
import { Box, Text } from "ink";
import { ErrorPrimitive } from "@assistant-ui/react-ink";
import { useColors, symbols } from "../theme.js";

export function ErrorMessage() {
	const colors = useColors();
	return (
		<ErrorPrimitive.Root>
			<Box paddingLeft={2} marginBottom={1}>
				<Text color={colors.error}>{symbols.cross} </Text>
				<ErrorPrimitive.Message color={colors.error} />
			</Box>
		</ErrorPrimitive.Root>
	);
}
