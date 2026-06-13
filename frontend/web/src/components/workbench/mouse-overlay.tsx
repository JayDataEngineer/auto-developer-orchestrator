import React, { useRef, useEffect, useState, useCallback } from "react";
import { usePuxStore } from "@pux/shared";

// SVG cursor arrow (28x28, inspired by browser-use-wasm)
const CURSOR_SVG = `<svg width="28" height="28" viewBox="0 0 28 28" fill="none" xmlns="http://www.w3.org/2000/svg">
  <path d="M5 2L5 22L10 17L15 25L19 23L14 15L22 15L5 2Z" fill="black" stroke="white" stroke-width="1.5" stroke-linejoin="round"/>
</svg>`;

const cursorUrl = `data:image/svg+xml;base64,${btoa(CURSOR_SVG)}`;

interface ClickMarkerProps {
	normX: number;
	normY: number;
	ts: number;
	containerWidth: number;
	containerHeight: number;
}

function ClickMarker({ normX, normY, ts, containerWidth, containerHeight }: ClickMarkerProps) {
	const [opacity, setOpacity] = useState(0.8);

	useEffect(() => {
		// Fade out after 10 seconds
		const timer = setTimeout(() => setOpacity(0), 10000);
		return () => clearTimeout(timer);
	}, [ts]);

	const left = normX * containerWidth;
	const top = normY * containerHeight;

	if (opacity <= 0) return null;

	return (
		<div
			style={{
				position: "absolute",
				left: left - 6,
				top: top - 6,
				width: 12,
				height: 12,
				borderRadius: "50%",
				border: "2px solid #ef4444",
				background: "rgba(239, 68, 68, 0.15)",
				transform: "translate(0, 0)",
				opacity,
				transition: "opacity 1s ease-out",
				pointerEvents: "none",
			}}
		/>
	);
}

interface MouseOverlayProps {
	containerRef: React.RefObject<HTMLDivElement | null>;
}

export function MouseOverlay({ containerRef }: MouseOverlayProps) {
	const mouseOverlay = usePuxStore((s) => s.mouseOverlay);
	const clickTrail = usePuxStore((s) => s.clickTrail);
	const [size, setSize] = useState({ width: 0, height: 0 });

	const updateSize = useCallback(() => {
		const el = containerRef.current;
		if (el) {
			setSize({ width: el.clientWidth, height: el.clientHeight });
		}
	}, [containerRef]);

	// Track container size
	useEffect(() => {
		const el = containerRef.current;
		if (!el) return;

		updateSize();
		const observer = new ResizeObserver(() => updateSize());
		observer.observe(el);
		return () => observer.disconnect();
	}, [containerRef, updateSize]);

	// Prune old click markers (>15s)
	const now = Date.now();
	const visibleMarkers = clickTrail.filter((m) => now - m.ts < 15000);

	if (!mouseOverlay && visibleMarkers.length === 0) return null;

	const cursorLeft = mouseOverlay ? mouseOverlay.normX * size.width : 0;
	const cursorTop = mouseOverlay ? mouseOverlay.normY * size.height : 0;

	return (
		<div
			style={{
				position: "absolute",
				inset: 0,
				pointerEvents: "none",
				overflow: "hidden",
				zIndex: 100,
			}}
		>
			{/* Animated cursor */}
			{mouseOverlay && (
				<>
					<img
						src={cursorUrl}
						alt=""
						draggable={false}
						style={{
							position: "absolute",
							left: cursorLeft - 4,
							top: cursorTop - 4,
							width: 28,
							height: 28,
							transition: mouseOverlay.state === "moving"
								? "left 300ms ease-out, top 300ms ease-out"
								: "none",
							animation: mouseOverlay.state === "click"
								? "mouse-pop 400ms ease-out"
								: undefined,
						}}
					/>
					{/* Thinking pulse ring */}
					{mouseOverlay.state === "thinking" && (
						<div
							style={{
								position: "absolute",
								left: cursorLeft - 8,
								top: cursorTop - 8,
								width: 16,
								height: 16,
								borderRadius: "50%",
								border: "2px solid #f97316",
								animation: "mouse-pulse 1.5s ease-in-out infinite",
							}}
						/>
					)}
					{/* Typing indicator */}
					{mouseOverlay.state === "typing" && (
						<div
							style={{
								position: "absolute",
								left: cursorLeft + 4,
								top: cursorTop + 20,
								width: 2,
								height: 14,
								background: "#3b82f6",
								animation: "mouse-blink 1s step-end infinite",
							}}
						/>
					)}
				</>
			)}

			{/* Click trail markers */}
			{visibleMarkers.map((marker) => (
				<ClickMarker
					key={marker.ts}
					normX={marker.normX}
					normY={marker.normY}
					ts={marker.ts}
					containerWidth={size.width}
					containerHeight={size.height}
				/>
			))}
		</div>
	);
}
