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
    onMouseDown: (e: React.MouseEvent) => void;
  };
}

export function useResizable({ defaultWidth, minWidth, maxWidth, side }: UseResizableOptions): UseResizableReturn {
  // Width state — only updated on mouseup to avoid re-renders during drag
  const [width, setWidth] = useState(defaultWidth);
  const [isDragging, setIsDragging] = useState(false);

  // Store current width in a ref so mousedown always reads the latest value
  // without needing width in the useCallback dependency array
  const widthRef = useRef(defaultWidth);
  widthRef.current = width;

  // Ref to the panel container element — set during mousedown for direct DOM manipulation
  const panelRef = useRef<HTMLElement | null>(null);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();

    // Find the panel container — the parent of the drag handle
    const handle = e.currentTarget as HTMLElement;
    const panel = handle.parentElement;
    if (!panel) return;

    panelRef.current = panel;
    const startX = e.clientX;
    const startWidth = widthRef.current;

    setIsDragging(true);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    // Disable transitions on the panel during drag for instant feedback
    panel.style.transition = 'none';

    const handleMouseMove = (ev: MouseEvent) => {
      const delta = ev.clientX - startX;
      const newWidth = side === 'right'
        ? startWidth + delta
        : startWidth - delta;
      const clamped = Math.min(maxWidth, Math.max(minWidth, newWidth));
      // Direct DOM manipulation — no React re-render
      panel.style.width = `${clamped}px`;
    };

    const handleMouseUp = () => {
      // Re-enable transitions
      if (panelRef.current) panelRef.current.style.transition = '';
      panelRef.current = null;

      document.body.style.cursor = '';
      document.body.style.userSelect = '';

      // Read the final width from the DOM to avoid stale values
      const finalWidth = panel ? parseInt(panel.style.width, 10) : widthRef.current;
      const clamped = Math.min(maxWidth, Math.max(minWidth, finalWidth));

      setWidth(clamped);
      setIsDragging(false);

      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
  }, [minWidth, maxWidth, side]);

  // Cleanup on unmount in case component is removed while dragging
  useEffect(() => {
    return () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, []);

  return { width, isDragging, handleProps: { onMouseDown: handleMouseDown } };
}
