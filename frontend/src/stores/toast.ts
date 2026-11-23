import { create } from 'zustand';
import { useNotificationsStore } from './notifications';
import { translateMessage } from '../i18n';

// At most this many popups are on screen; older ones leave the screen (errors stay in the menu).
const MAX_POPUPS = 5;

type ToastLevel = 'info' | 'success' | 'warning' | 'error';

interface ToastItem {
  id: number;
  level: ToastLevel;
  message: string;
}

interface ToastState {
  items: ToastItem[];
  push: (message: string, level?: ToastLevel) => void;
  dismiss: (id: number) => void;
}

let nextId = 1;

export const useToastStore = create<ToastState>((set, get) => ({
  items: [],
  push: (text, level = 'info') => {
    const message = translateMessage(text);
    const id = nextId++;
    set({ items: [...get().items, { id, level, message }].slice(-MAX_POPUPS) });
    // Errors and warnings also go to the notifications menu, so they can be read later.
    if (level === 'error' || level === 'warning') useNotificationsStore.getState().addLocal(level, message);
    window.setTimeout(() => get().dismiss(id), level === 'error' ? 8000 : 5000);
  },
  dismiss: (id) => set({ items: get().items.filter((item) => item.id !== id) }),
}));

export const toast = {
  info: (message: string) => useToastStore.getState().push(message, 'info'),
  success: (message: string) => useToastStore.getState().push(message, 'success'),
  warning: (message: string) => useToastStore.getState().push(message, 'warning'),
  error: (message: string) => useToastStore.getState().push(message, 'error'),
};

export async function guard<T>(action: () => Promise<T>): Promise<T | undefined> {
  try {
    return await action();
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error));
    return undefined;
  }
}
