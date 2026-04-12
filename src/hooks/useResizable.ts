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
  const [width, setWidth] = useState(defaultWidth);
  const [isDragging, setIsDragging] = useState(false);
  const dragState = useRef<{ startX: number; startWidth: number } | null>(null);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = width;
    dragState.current = { startX, startWidth };
    setIsDragging(true);

    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    const handleMouseMove = (ev: MouseEvent) => {
      if (!dragState.current) return;
      const delta = ev.clientX - dragState.current.startX;
      const newWidth = side === 'right'
        ? dragState.current.startWidth + delta
        : dragState.current.startWidth - delta;
      setWidth(Math.min(maxWidth, Math.max(minWidth, newWidth)));
    };

    const handleMouseUp = () => {
      dragState.current = null;
      setIsDragging(false);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
    };

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
  }, [width, minWidth, maxWidth, side]);

  // Cleanup on unmount in case component is removed while dragging
  useEffect(() => {
    return () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, []);

  return { width, isDragging, handleProps: { onMouseDown: handleMouseDown } };
}
