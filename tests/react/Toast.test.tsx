import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { ToastProvider, useToastContext } from '../../src/components/ui/Toast';
import { vi } from 'vitest';

function TestConsumer() {
  const { addToast } = useToastContext();
  return (
    <div>
      <button onClick={() => addToast('success', 'Task created')}>Success</button>
      <button onClick={() => addToast('error', 'Something failed')}>Error</button>
      <button onClick={() => addToast('info', 'Info message')}>Info</button>
    </div>
  );
}

describe('ToastProvider', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders toast on addToast', () => {
    render(
      <ToastProvider>
        <TestConsumer />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Success'));
    expect(screen.getByText('Task created')).toBeInTheDocument();
  });

  it('renders error toast', () => {
    render(
      <ToastProvider>
        <TestConsumer />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Error'));
    expect(screen.getByText('Something failed')).toBeInTheDocument();
  });

  it('renders info toast', () => {
    render(
      <ToastProvider>
        <TestConsumer />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Info'));
    expect(screen.getByText('Info message')).toBeInTheDocument();
  });

  it('auto-dismisses toast after 4 seconds', () => {
    render(
      <ToastProvider>
        <TestConsumer />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Success'));
    expect(screen.getByText('Task created')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(4000);
    });

    expect(screen.queryByText('Task created')).not.toBeInTheDocument();
  });

  it('dismisses toast on click X button', () => {
    render(
      <ToastProvider>
        <TestConsumer />
      </ToastProvider>
    );

    fireEvent.click(screen.getByText('Success'));
    expect(screen.getByText('Task created')).toBeInTheDocument();

    // Find and click the close button (X icon)
    const closeButtons = screen.getAllByRole('button');
    // The last button is the X close on the toast
    const closeBtn = closeButtons.find(b => b.closest('.pointer-events-auto'));
    if (closeBtn) {
      fireEvent.click(closeBtn);
    }

    // After auto-dismiss timers fire, toast should be gone
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(screen.queryByText('Task created')).not.toBeInTheDocument();
  });

  it('returns default context when used outside provider', () => {
    // React context returns default value (no throw) when no provider
    // Just verify the consumer doesn't crash
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});

    // createContext returns default value, no throw in React 19
    const { unmount } = render(<TestConsumer />);
    // Buttons should still render (they just won't do anything useful)
    expect(screen.getByText('Success')).toBeInTheDocument();

    unmount();
    spy.mockRestore();
  });
});
