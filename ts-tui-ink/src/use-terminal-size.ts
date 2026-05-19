/**
 * Reactive terminal dimensions — re-renders on resize.
 *
 * Ink's built-in resize handler recalculates Yoga layout width and re-renders
 * the output buffer, but does NOT trigger React component re-renders. So
 * `useStdout().stdout.rows` / `.columns` hold stale values after a resize.
 *
 * This hook listens to `process.stdout.on('resize')` and bumps a state counter
 * to force React to re-render, giving fresh rows/columns on every resize.
 */
import { useState, useEffect } from "react";

export function useTerminalSize() {
	const [tick, setTick] = useState(0);

	useEffect(() => {
		const onResize = () => setTick((t) => t + 1);
		process.stdout.on("resize", onResize);
		return () => {
			process.stdout.off("resize", onResize);
		};
	}, []);

	// tick is intentionally consumed to prevent dead-code elimination
	void tick;

	return {
		rows: process.stdout.rows ?? 24,
		cols: process.stdout.columns ?? 80,
	};
}
