import { t } from '../../i18n';
import { KBD_SERIAL_ROOT, KBD_HEAD, KBD_CLOSE, KBD_ROW, KBD_SERIAL_KEY, KBD_ON, KBD_COMBO } from './styles';

// Virtual keyboard of the Serial terminal. Unlike the VNC on-screen keyboard,
// which sends X11 keysyms, here the buttons produce regular ANSI sequences:
// the terminal receives the same bytes as when a physical key is pressed.

interface SerialModifiers {
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
}

const SPECIAL: Record<string, string> = {
  Esc: '\x1b', Tab: '\t', Enter: '\r', Bksp: '\x7f', Space: ' ',
  Del: '\x1b[3~', Ins: '\x1b[2~', Home: '\x1b[H', End: '\x1b[F',
  PgUp: '\x1b[5~', PgDn: '\x1b[6~',
  '←': '\x1b[D', '↑': '\x1b[A', '↓': '\x1b[B', '→': '\x1b[C',
};

const FKEYS = ['\x1bOP', '\x1bOQ', '\x1bOR', '\x1bOS', '\x1b[15~', '\x1b[17~', '\x1b[18~', '\x1b[19~', '\x1b[20~', '\x1b[21~', '\x1b[23~', '\x1b[24~'];

function controlChar(key: string): string | null {
  const lower = key.toLowerCase();
  const code = lower.charCodeAt(0);
  if (code >= 97 && code <= 122) return String.fromCharCode(code - 96);
  switch (lower) {
    case '@': case '2': return '\x00';
    case '[': case '3': return '\x1b';
    case '\\': case '4': return '\x1c';
    case ']': case '5': return '\x1d';
    case '^': case '6': return '\x1e';
    case '_': case '-': case '/': return '\x1f';
    case '?': case '8': return '\x7f';
    default: return null;
  }
}

// Translates a pressed virtual keyboard button into bytes for the Serial console.
export function serialSequence(key: string, mods: SerialModifiers = {}): string {
  if (key === 'Tab' && mods.shift) return '\x1b[Z';
  let data: string;
  const functionKey = /^F([1-9]|1[0-2])$/.exec(key);
  if (functionKey) data = FKEYS[Number.parseInt(functionKey[1] ?? '0', 10) - 1] ?? '';
  else if (Object.prototype.hasOwnProperty.call(SPECIAL, key)) data = SPECIAL[key] ?? '';
  else if ([...key].length === 1) data = mods.shift ? key.toUpperCase() : key;
  else return '';
  if (mods.ctrl) {
    const control = key === 'Space' ? '\x00' : controlChar(key);
    if (control !== null) data = control;
  }
  if (mods.alt) data = '\x1b' + data;
  return data;
}

const MODS: Array<[string, keyof SerialModifiers]> = [['Ctrl', 'ctrl'], ['Alt', 'alt'], ['Shift', 'shift']];
const KEYS_TOP: string[] = ['Esc', 'Tab', 'Enter', 'Bksp', 'Space', 'Del', 'Ins', 'Home'];
const KEYS_BOTTOM: string[] = ['End', 'PgUp', 'PgDn', '←', '↑', '↓', '→'];
const COMBOS: Array<[string, string]> = [
  ['Ctrl+C', 'C'], ['Ctrl+D', 'D'], ['Ctrl+Z', 'Z'], ['Ctrl+L', 'L'], ['Ctrl+A', 'A'],
  ['Ctrl+E', 'E'], ['Ctrl+U', 'U'], ['Ctrl+K', 'K'], ['Ctrl+W', 'W'], ['Ctrl+R', 'R'],
];

function row(children: HTMLElement[]): HTMLDivElement {
  const element = document.createElement('div');
  element.className = KBD_ROW;
  element.append(...children);
  return element;
}

function pushButton(parent: HTMLElement, label: string, title: string, action: () => void, className = ''): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  button.className = `${KBD_SERIAL_KEY}${className ? ` ${className}` : ''}`;
  button.textContent = label;
  button.title = title;
  button.onclick = action;
  parent.append(button);
  return button;
}

export function createSerialKeyboard(send: (data: string) => void, onClose: () => void): HTMLDivElement {
  const held = new Set<keyof SerialModifiers>();
  const root = document.createElement('div');
  root.className = KBD_SERIAL_ROOT;
  root.dataset['vkbd'] = '';
  // The buttons must not steal focus from the terminal: otherwise the
  // physical keyboard stops working while the on-screen one is open.
  root.onmousedown = (event) => event.preventDefault();

  const head = document.createElement('div');
  head.className = KBD_HEAD;
  const title = document.createElement('b');
  title.textContent = t('Virtual keyboard');
  const hint = document.createElement('span');
  hint.className = 'text-muted';
  hint.textContent = t('Serial keyboard');
  const close = document.createElement('button');
  close.type = 'button';
  close.className = KBD_CLOSE;
  close.title = t('Close');
  close.textContent = '✕';
  close.onclick = onClose;
  head.append(title, hint, close);

  const mods = new Map<keyof SerialModifiers, HTMLButtonElement>();
  const modRow = row([]);
  for (const [label, key] of MODS) {
    const button = pushButton(modRow, label, `${t('Modifier')}: ${label}`, () => {
      if (held.has(key)) held.delete(key); else held.add(key);
      updateMods();
    }, '');
    mods.set(key, button);
  }
  const updateMods = (): void => {
    for (const [key, button] of mods) button.className = held.has(key) ? `${KBD_SERIAL_KEY} ${KBD_ON}` : KBD_SERIAL_KEY;
  };

  const sendKey = (key: string): void => {
    const data = serialSequence(key, { ctrl: held.has('ctrl'), alt: held.has('alt'), shift: held.has('shift') });
    held.clear();
    updateMods();
    if (data) send(data);
  };

  const fkeyRow = row([]);
  FKEYS.forEach((_, index) => {
    const label = `F${index + 1}`;
    pushButton(fkeyRow, label, label, () => sendKey(label));
  });
  const keyTop = row([]);
  for (const label of KEYS_TOP) pushButton(keyTop, label, label, () => sendKey(label));
  const keyBottom = row([]);
  for (const label of KEYS_BOTTOM) pushButton(keyBottom, label, label, () => sendKey(label));
  const comboRow = row([]);
  for (const [label, key] of COMBOS) {
    pushButton(comboRow, label, label, () => {
      const data = serialSequence(key, { ctrl: true });
      if (data) send(data);
    }, KBD_COMBO);
  }

  root.append(head, modRow, fkeyRow, keyTop, keyBottom, comboRow);
  return root;
}
