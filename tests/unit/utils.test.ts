import { describe, it, expect } from 'vitest';
import { cn } from '../../src/lib/utils';

describe('utils - cn()', () => {
  it('should merge tailwind classes', () => {
    expect(cn('p-2', 'p-4')).toBe('p-4');
  });

  it('should handle conditional classes', () => {
    const isTrue = true;
    const isFalse = false;
    expect(cn('base', isTrue && 'truthy', isFalse && 'falsy')).toBe('base truthy');
  });

  it('should merge conflicting classes properly', () => {
    expect(cn('bg-red-500', 'bg-blue-500')).toBe('bg-blue-500');
    expect(cn('text-sm', 'text-lg')).toBe('text-lg');
  });
});
