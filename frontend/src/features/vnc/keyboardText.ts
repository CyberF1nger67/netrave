// Pure helpers for sending text to a VNC guest. No DOM access, so they can be tested directly.
//
// Why this exists: a VNC KeyEvent carries a keysym, and the server (QEMU) derives the
// physical key from its own keymap. Symbols that need Shift on the server's layout
// (for example "_" = Shift + minus, "@" = Shift + 2) are not Shift-aware on the
// server side: the base key arrives without Shift, so "_" turns into "-" and "@"
// into "2". Pressing Shift explicitly around such characters fixes this for any
// host keyboard layout, because keysyms do not depend on the host layout.

export const KEYSYM_SHIFT_L = 0xffe1;
const KEYSYM_ENTER = 0xff0d;
const KEYSYM_TAB = 0xff09;

// Symbols that need Shift on a US (en-us) layout. Uppercase letters are handled separately.
const US_SHIFTED_SYMBOLS = new Set(['~', '!', '@', '#', '$', '%', '^', '&', '*', '(', ')', '_', '+', '{', '}', '|', ':', '"', '<', '>', '?']);

interface KeyStep {
  keysym: number;
  down: boolean;
}

interface TypingPlan {
  steps: KeyStep[];
  // Characters that cannot be produced by a keysym on a US-layout guest (for example Cyrillic).
  skipped: number;
}

export function needsShift(char: string): boolean {
  return (char >= 'A' && char <= 'Z') || US_SHIFTED_SYMBOLS.has(char);
}

// Converts text into key steps. Each printable ASCII character becomes a
// press/release pair of its keysym, wrapped in Shift press/release when required.
export function typingPlan(text: string): TypingPlan {
  const steps: KeyStep[] = [];
  let skipped = 0;
  for (const char of text) {
    if (char === '\r') continue;
    if (char === '\n') { steps.push({ keysym: KEYSYM_ENTER, down: true }, { keysym: KEYSYM_ENTER, down: false }); continue; }
    if (char === '\t') { steps.push({ keysym: KEYSYM_TAB, down: true }, { keysym: KEYSYM_TAB, down: false }); continue; }
    const code = char.codePointAt(0) ?? 0;
    if (code < 0x20 || code > 0x7e) { skipped++; continue; }
    const shift = needsShift(char);
    if (shift) steps.push({ keysym: KEYSYM_SHIFT_L, down: true });
    steps.push({ keysym: code, down: true }, { keysym: code, down: false });
    if (shift) steps.push({ keysym: KEYSYM_SHIFT_L, down: false });
  }
  return { steps, skipped };
}

// RFB ClientCutText carries ISO 8859-1 only. Characters outside Latin-1 cannot be
// represented, so the caller must be told instead of silently sending "?".
export function latin1Payload(text: string): { bytes: Uint8Array; unsupported: number } {
  const bytes = new Uint8Array(text.length);
  let unsupported = 0;
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index);
    if (code <= 0xff) bytes[index] = code;
    else { bytes[index] = 0x3f; unsupported++; }
  }
  return { bytes, unsupported };
}
