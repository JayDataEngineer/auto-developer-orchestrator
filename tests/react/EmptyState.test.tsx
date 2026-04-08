import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { EmptyState } from '../../src/components/ui/EmptyState';
import { Monitor } from 'lucide-react';

describe('EmptyState', () => {
  it('renders icon, title, and description', () => {
    render(
      <EmptyState
        icon={<Monitor data-testid="icon" />}
        title="No items"
        description="Create one to get started"
      />
    );

    expect(screen.getByTestId('icon')).toBeInTheDocument();
    expect(screen.getByText('No items')).toBeInTheDocument();
    expect(screen.getByText('Create one to get started')).toBeInTheDocument();
  });

  it('renders action button when provided', () => {
    const onClick = vi.fn();
    render(
      <EmptyState
        icon={<Monitor data-testid="icon" />}
        title="Empty"
        action={{ label: '+ New', onClick }}
      />
    );

    const button = screen.getByText('+ New');
    expect(button).toBeInTheDocument();
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('does not render action button when not provided', () => {
    render(
      <EmptyState
        icon={<Monitor data-testid="icon" />}
        title="Empty"
      />
    );

    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
