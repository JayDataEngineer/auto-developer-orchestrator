import { useState, useCallback, useRef, useEffect } from 'react';

interface UseResizableOptions {
  defaultWidth: number;
  minWidth: number;
  maxWidth: number;
  /** 'right' = drag handle on right edge (left sidebar), 'left' = drag handle on left edge (right panel), 'bottom' = drag handle on bottom edge (vertical resize) */
  side: 'left' | 'right' | 'bottom';
}

interface UseResizableReturn {
  width: number;
  height: number;
  isDragging: boolean;
  handleProps: {
    onPointerDown: (e: React.PointerEvent) => void;
  };
}

export function useResizable({ defaultWidth, minWidth, maxWidth, side }: UseResizableOptions): UseResizableReturn {
  // Width/height state — only updated on pointerup to avoid re-renders during drag
  const [width, setWidth] = useState(defaultWidth);
  const [isDragging, setIsDragging] = useState(false);

  // Store current size in a ref so pointerdown always reads the latest value
  const sizeRef = useRef(defaultWidth);
  sizeRef.current = side === 'bottom' ? width : width; // reuse 'width' for height in bottom mode
  const height = side === 'bottom' ? width : 0;

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();

    const handle = e.currentTarget as HTMLElement;
    const panel = handle.parentElement;
    if (!panel) return;

    const isVertical = side === 'bottom';
    const startPos = isVertical ? e.clientY : e.clientX;
    const startSize = sizeRef.current;

    handle.setPointerCapture(e.pointerId);

    setIsDragging(true);
    document.body.style.cursor = isVertical ? 'row-resize' : 'col-resize';
    document.body.style.userSelect = 'none';
    panel.style.transition = 'none';

    const handlePointerMove = (ev: PointerEvent) => {
      let newSize: number;
      if (isVertical) {
        newSize = startSize + (ev.clientY - startPos);
      } else if (side === 'right') {
        newSize = startSize + (ev.clientX - startPos);
      } else {
        newSize = startSize - (ev.clientX - startPos);
      }
      const clamped = Math.min(maxWidth, Math.max(minWidth, newSize));
      if (isVertical) {
        panel.style.height = `${clamped}px`;
      } else {
        panel.style.width = `${clamped}px`;
      }
    };

    const handlePointerUp = (ev: PointerEvent) => {
      handle.releasePointerCapture(ev.pointerId);
      handle.removeEventListener('pointermove', handlePointerMove);
      handle.removeEventListener('pointerup', handlePointerUp);

      panel.style.transition = '';
      document.body.style.cursor = '';
      document.body.style.userSelect = '';

      const prop = isVertical ? 'height' : 'width';
      const finalVal = parseInt(panel.style[prop], 10);
      const clamped = Math.min(maxWidth, Math.max(minWidth, finalVal));

      setWidth(clamped);
      setIsDragging(false);
    };

    handle.addEventListener('pointermove', handlePointerMove);
    handle.addEventListener('pointerup', handlePointerUp);
  }, [minWidth, maxWidth, side]);

  useEffect(() => {
    return () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, []);

  return { width, height, isDragging, handleProps: { onPointerDown: handlePointerDown } };
}
