import { useState, useCallback, useRef, useEffect } from 'react';

interface UseResizableOptions {
  defaultWidth: number;
  minWidth: number;
  maxWidth: number;
  /** 'right' = drag handle on right edge (left sidebar), 'left' = drag handle on left edge (right panel) */
  side: 'left' | 'right';
}

interface UseResizableReturn {
  width: number;
  isDragging: boolean;
  handleProps: {
    onPointerDown: (e: React.PointerEvent) => void;
  };
}

export function useResizable({ defaultWidth, minWidth, maxWidth, side }: UseResizableOptions): UseResizableReturn {
  // Width state — only updated on pointerup to avoid re-renders during drag
  const [width, setWidth] = useState(defaultWidth);
  const [isDragging, setIsDragging] = useState(false);

  // Store current width in a ref so pointerdown always reads the latest value
  // without needing width in the useCallback dependency array
  const widthRef = useRef(defaultWidth);
  widthRef.current = width;

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    // Only respond to primary button
    if (e.button !== 0) return;
    e.preventDefault();

    const handle = e.currentTarget as HTMLElement;
    const panel = handle.parentElement;
    if (!panel) return;

    const startX = e.clientX;
    const startWidth = widthRef.current;

    // Capture the pointer — all subsequent events fire on this element
    // even if the pointer moves over iframes, canvas, or outside the window.
    // This fixes the "stuck in resize mode" bug when dragging over noVNC.
    handle.setPointerCapture(e.pointerId);

    setIsDragging(true);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    // Disable CSS transitions on the panel during drag for instant feedback
    panel.style.transition = 'none';

    const handlePointerMove = (ev: PointerEvent) => {
      const delta = ev.clientX - startX;
      const newWidth = side === 'right'
        ? startWidth + delta
        : startWidth - delta;
      const clamped = Math.min(maxWidth, Math.max(minWidth, newWidth));
      // Direct DOM manipulation — no React re-render
      panel.style.width = `${clamped}px`;
    };

    const handlePointerUp = (ev: PointerEvent) => {
      handle.releasePointerCapture(ev.pointerId);
      handle.removeEventListener('pointermove', handlePointerMove);
      handle.removeEventListener('pointerup', handlePointerUp);

      // Re-enable transitions
      panel.style.transition = '';
      document.body.style.cursor = '';
      document.body.style.userSelect = '';

      // Read the final width from the DOM
      const finalWidth = parseInt(panel.style.width, 10);
      const clamped = Math.min(maxWidth, Math.max(minWidth, finalWidth));

      setWidth(clamped);
      setIsDragging(false);
    };

    // Add listeners to the handle element (not window).
    // With pointer capture, these fire even when pointer is over other elements.
    handle.addEventListener('pointermove', handlePointerMove);
    handle.addEventListener('pointerup', handlePointerUp);
  }, [minWidth, maxWidth, side]);

  // Cleanup on unmount in case component is removed while dragging
  useEffect(() => {
    return () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, []);

  return { width, isDragging, handleProps: { onPointerDown: handlePointerDown } };
}
