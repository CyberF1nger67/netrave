import { create } from 'zustand';

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
  push: (message, level = 'info') => {
    const id = nextId++;
    set({ items: [...get().items, { id, level, message }] });
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
