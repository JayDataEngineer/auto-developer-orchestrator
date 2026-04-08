import React, { createContext, useContext, useState, useCallback, useRef } from 'react';
import { cn } from '../../lib/utils';
import { CheckCircle, XCircle, Info, X } from 'lucide-react';

interface Toast {
  id: string;
  type: 'success' | 'error' | 'info';
  message: string;
}

interface ToastContextType {
  addToast: (type: Toast['type'], message: string) => void;
}

const ToastContext = createContext<ToastContextType>({ addToast: () => {} });

export function useToastContext() {
  return useContext(ToastContext);
}

const TOAST_STYLES = {
  success: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300',
  error: 'border-red-500/30 bg-red-500/10 text-red-300',
  info: 'border-primary/30 bg-primary/10 text-primary',
};

const TOAST_ICONS = {
  success: CheckCircle,
  error: XCircle,
  info: Info,
};

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const counterRef = useRef(0);

  const removeToast = useCallback((id: string) => {
    setToasts(prev => prev.filter(t => t.id !== id));
  }, []);

  const addToast = useCallback((type: Toast['type'], message: string) => {
    const id = String(++counterRef.current);
    setToasts(prev => [...prev, { id, type, message }]);
    setTimeout(() => removeToast(id), 4000);
  }, [removeToast]);

  return (
    <ToastContext.Provider value={{ addToast }}>
      {children}
      {/* Toast container */}
      <div className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 pointer-events-none">
        {toasts.map(toast => {
          const Icon = TOAST_ICONS[toast.type];
          return (
            <div
              key={toast.id}
              className={cn(
                'pointer-events-auto flex items-center gap-2 px-3 py-2 border rounded min-w-[250px] max-w-[400px]',
                'animate-[slideIn_0.2s_ease-out]',
                TOAST_STYLES[toast.type]
              )}
            >
              <Icon size={12} className="shrink-0" />
              <span className="text-[10px] font-mono flex-1">{toast.message}</span>
              <button
                onClick={() => removeToast(toast.id)}
                className="shrink-0 opacity-50 hover:opacity-100 transition-opacity"
              >
                <X size={10} />
              </button>
            </div>
          );
        })}
      </div>
      <style>{`
        @keyframes slideIn {
          from { transform: translateX(100%); opacity: 0; }
          to { transform: translateX(0); opacity: 1; }
        }
      `}</style>
    </ToastContext.Provider>
  );
}
