// Browser clipboard helpers shared by the terminal shortcuts and the clipboard panel.

/** Copies text to the browser clipboard. Returns false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
  if (!text) return false;
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(text); return true; }
  } catch {
    // Below is a fallback for browsers without the Clipboard API.
  }
  const textarea = document.createElement('textarea');
  textarea.style.cssText = 'position:fixed;opacity:0;left:-9999px';
  textarea.value = text;
  document.body.append(textarea);
  textarea.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch { ok = false; }
  textarea.remove();
  return ok;
}

/** Reads text from the browser clipboard. Returns an empty string when reading is not allowed. */
export async function readClipboardText(): Promise<string> {
  try { return (await navigator.clipboard?.readText?.()) ?? ''; } catch { return ''; }
}

/** A clipboard action requested by a key press. */
type ClipboardAction = 'copy' | 'paste';

/** The subset of a keyboard event relevant to the shortcut mapping. */
export interface KeyPress {
  type: string;
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

/**
 * Maps a key press to a clipboard action. Only combinations that cannot be meant for the guest are
 * mapped: Ctrl+C and Ctrl+V still reach the serial line, so copying uses Ctrl+Shift+C and pasting
 * Ctrl+Shift+V (Insert combinations and the Command key on macOS are supported as well).
 */
export function clipboardAction(event: KeyPress): ClipboardAction | null {
  if (event.type !== 'keydown' || event.altKey) return null;
  const key = event.key.toLowerCase();
  const command = event.metaKey && !event.ctrlKey;
  const control = event.ctrlKey && !event.metaKey;
  if (key === 'insert') {
    if (control && !event.shiftKey) return 'copy';
    if (event.shiftKey) return 'paste';
    return null;
  }
  const withShift = (event.ctrlKey || event.metaKey) && event.shiftKey;
  if (key === 'c' && (withShift || (command && !event.shiftKey))) return 'copy';
  if (key === 'v' && (withShift || (command && !event.shiftKey))) return 'paste';
  return null;
}
