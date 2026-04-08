import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from '../../src/components/ui/StatusBadge';

describe('StatusBadge', () => {
  it('renders pending status with zinc dot', () => {
    const { container } = render(<StatusBadge status="pending" />);
    const dot = container.querySelector('.bg-zinc-600');
    expect(dot).toBeInTheDocument();
    expect(screen.getByText('pending')).toBeInTheDocument();
  });

  it('renders in_progress status with yellow dot', () => {
    const { container } = render(<StatusBadge status="in_progress" />);
    const dot = container.querySelector('.bg-yellow-400');
    expect(dot).toBeInTheDocument();
  });

  it('renders completed status with emerald dot', () => {
    const { container } = render(<StatusBadge status="completed" />);
    const dot = container.querySelector('.bg-emerald-400');
    expect(dot).toBeInTheDocument();
  });

  it('renders failed status with red dot', () => {
    const { container } = render(<StatusBadge status="failed" />);
    const dot = container.querySelector('.bg-red-400');
    expect(dot).toBeInTheDocument();
  });

  it('uses custom label when provided', () => {
    render(<StatusBadge status="completed" label="Done" />);
    expect(screen.getByText('Done')).toBeInTheDocument();
  });

  it('uses md size classes', () => {
    const { container } = render(<StatusBadge status="pending" size="md" />);
    const dot = container.querySelector('.w-2');
    expect(dot).toBeInTheDocument();
  });
});
