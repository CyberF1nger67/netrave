// Confirmation and text dialogs in the application's own style. They replace the browser's
// confirm() and prompt(): callers await the answer and the dialog is shown by ConfirmHost.
import { create } from 'zustand';

export interface ConfirmOptions {
  title: string;
  message?: string | undefined;
  /** Destructive actions use the danger style and the "Delete" label by default. */
  danger?: boolean;
  confirmLabel?: string;
}

export interface TextOptions {
  title: string;
  label?: string;
  initial?: string;
  confirmLabel?: string;
}

export type DialogRequest =
  | { id: number; kind: 'confirm'; options: ConfirmOptions; resolve: (ok: boolean) => void }
  | { id: number; kind: 'text'; options: TextOptions; resolve: (value: string | null) => void };

interface DialogState {
  request: DialogRequest | null;
  set: (request: DialogRequest | null) => void;
}

export const useDialogStore = create<DialogState>((set) => ({
  request: null,
  set: (request) => set({ request }),
}));

let nextId = 1;

/** Cancels the dialog that is open, if any, so its caller does not wait forever. */
function cancelOpen() {
  const open = useDialogStore.getState().request;
  if (!open) return;
  if (open.kind === 'confirm') open.resolve(false);
  else open.resolve(null);
}

/** Asks the user to confirm an action. Resolves to true only on an explicit confirmation. */
export function askConfirm(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    cancelOpen();
    useDialogStore.getState().set({ id: nextId++, kind: 'confirm', options, resolve });
  });
}

/** Asks for a short text. Resolves to the trimmed text, or null when cancelled or empty. */
export function askText(options: TextOptions): Promise<string | null> {
  return new Promise((resolve) => {
    cancelOpen();
    useDialogStore.getState().set({ id: nextId++, kind: 'text', options, resolve });
  });
}
