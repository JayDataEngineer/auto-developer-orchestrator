// Global toast bridge — allows non-React code (hooks) to fire toasts
// without needing React context. The ToastProvider registers the real handler.

type ToastType = 'success' | 'error' | 'info';
type ToastHandler = (type: ToastType, message: string) => void;

let handler: ToastHandler = () => {};

export function registerToastHandler(h: ToastHandler) {
  handler = h;
}

export function unregisterToastHandler() {
  handler = () => {};
}

export function showToast(type: ToastType, message: string) {
  handler(type, message);
}
