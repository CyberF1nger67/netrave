// Built-in text terminal (a subset of VT100/ANSI), with no external dependencies.
// The screen size is fixed (COLS x ROWS): the serial line does not transmit the window size, so the guest
// still assumes an 80x24 screen, and the local grid must match it.
// The screen model is a cell grid. A wide character (CJK, emoji) occupies two columns, and the continuation cell
// is stored as an empty string ''. Rows are redrawn only if they changed; the cursor is a separate element,
// so text is not split into nodes and does not "jump" with the cursor.
// Supported: CUP/ED/EL/ICH/DCH/ECH/IL/DL, scrolling and scroll region (DECSTBM), alternate screen,
// cursor save, DSR 6, DEC graphics (ESC ( 0), bracketed paste, DECCKM.
// Scrolling down is performed only if the user is already at the bottom.
const FKEYS = ['\x1bOP', '\x1bOQ', '\x1bOR', '\x1bOS', '\x1b[15~', '\x1b[17~', '\x1b[18~', '\x1b[19~', '\x1b[20~', '\x1b[21~', '\x1b[23~', '\x1b[24~'];
const KEYMAP: Record<string, string> = {
  Enter: '\r', Backspace: '\x7f', Tab: '\t', Escape: '\x1b', Delete: '\x1b[3~', Insert: '\x1b[2~',
  ArrowUp: '\x1b[A', ArrowDown: '\x1b[B', ArrowRight: '\x1b[C', ArrowLeft: '\x1b[D',
  Home: '\x1b[H', End: '\x1b[F', PageUp: '\x1b[5~', PageDown: '\x1b[6~',
};

// In application cursor keys mode (DECCKM) arrows and Home/End are sent in SS3 form.
const APP_KEYMAP: Record<string, string> = {
  ArrowUp: '\x1bOA', ArrowDown: '\x1bOB', ArrowRight: '\x1bOC', ArrowLeft: '\x1bOD',
  Home: '\x1bOH', End: '\x1bOF',
};

type Cell = string; // '' - the second half of a wide character
const COLS = 80;
const ROWS = 24;
const MAX_HISTORY = 2000;
// Row height in em; matches leading-[1.6] in TERM_ROOT and h-[1.6em] on screen rows.
const LINE_EM = 1.6;

// DEC Special graphics (ESC ( 0): box-drawing pseudographics used by htop, mc and menus.
const DEC_SPECIAL: Record<string, string> = {
  '`': '◆', a: '▒', b: '␉', c: '␌', d: '␍', e: '␊', f: '°', g: '±', h: '␤', i: '␋', j: '┘', k: '┐', l: '┌', m: '└',
  n: '┼', o: '⎺', p: '⎻', q: '─', r: '⎼', s: '⎽', t: '├', u: '┤', v: '┴', w: '┬', x: '│', y: '≤', z: '≥', '{': 'π',
  '|': '≠', '}': '£', '~': '·',
};
type Range = readonly [number, number];

const TERM_ROOT = 'group/term relative box-border h-full w-full overflow-y-auto overflow-x-hidden [scrollbar-gutter:stable] [--term-accent:#8b9cff] bg-[radial-gradient(120%_95%_at_100%_0%,rgba(99,102,241,.16),transparent_56%),radial-gradient(90%_80%_at_0%_100%,rgba(34,211,238,.07),transparent_62%),linear-gradient(180deg,#0b111b_0%,#070a10_100%)] px-5 py-4 text-[#dbe6f5] [font-family:"JetBrains_Mono","Cascadia_Code","SF_Mono",ui-monospace,Menlo,Consolas,"Liberation_Mono","DejaVu_Sans_Mono",monospace] text-[13.5px] leading-[1.6] [font-variant-ligatures:none] [font-kerning:none] [font-feature-settings:"liga"_0,"clig"_0,"calt"_0] tracking-[.005em] [tab-size:8] [text-shadow:0_0_22px_rgba(129,140,248,.08)] outline-none antialiased [scrollbar-width:thin] [scrollbar-color:rgba(148,163,184,.32)_transparent] selection:bg-[rgba(99,102,241,.4)] selection:text-white focus-visible:outline focus-visible:outline-1 focus-visible:-outline-offset-1 focus-visible:outline-[rgba(129,140,248,.6)] focus-visible:shadow-[inset_0_0_0_1px_rgba(129,140,248,.2),inset_0_0_90px_rgba(99,102,241,.06)] [&::-webkit-scrollbar]:h-2.5 [&::-webkit-scrollbar]:w-2.5 [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:border-2 [&::-webkit-scrollbar-thumb]:border-transparent [&::-webkit-scrollbar-thumb]:bg-[rgba(148,163,184,.26)] [&::-webkit-scrollbar-thumb]:bg-clip-padding hover:[&::-webkit-scrollbar-thumb]:bg-[rgba(148,163,184,.45)]';
const TERM_OUT = 'm-0 whitespace-pre [font:inherit] [font-variant-ligatures:none] [font-kerning:none] tracking-normal [tab-size:8] [word-break:normal] [overflow-wrap:normal]';
const TERM_WIDE = 'inline-block w-[2ch] whitespace-pre text-center align-baseline';
const TERM_CUR = 'rounded-[3px] bg-[var(--term-accent)] text-[#070b12] [font:inherit] shadow-[0_0_0_1px_rgba(139,156,255,.5),0_0_16px_rgba(99,102,241,.55)] group-focus/term:animate-[term-caret_1.15s_ease-in-out_infinite]';

// Zero-width characters: combining marks, variation selectors, ZWJ/ZWSP.
const ZERO_WIDTH: readonly Range[] = [
  [0x0300, 0x036f], [0x0483, 0x0489], [0x0591, 0x05bd], [0x05bf, 0x05bf],
  [0x05c1, 0x05c2], [0x05c4, 0x05c5], [0x05c7, 0x05c7], [0x0610, 0x061a],
  [0x064b, 0x065f], [0x0670, 0x0670], [0x06d6, 0x06dc], [0x06df, 0x06e4],
  [0x06e7, 0x06e8], [0x06ea, 0x06ed], [0x0711, 0x0711], [0x0730, 0x074a],
  [0x07a6, 0x07b0], [0x07eb, 0x07f3], [0x0816, 0x0819], [0x081b, 0x0823],
  [0x0825, 0x0827], [0x0829, 0x082d], [0x0859, 0x085b], [0x08e3, 0x0903],
  [0x093a, 0x093c], [0x093e, 0x094f], [0x0951, 0x0957], [0x0962, 0x0963],
  [0x0981, 0x0983], [0x09bc, 0x09bc], [0x09be, 0x09c4], [0x09c7, 0x09c8],
  [0x09cb, 0x09cd], [0x09d7, 0x09d7], [0x09e2, 0x09e3], [0x0a01, 0x0a03],
  [0x0a3c, 0x0a3c], [0x0a3e, 0x0a42], [0x0a47, 0x0a48], [0x0a4b, 0x0a4d],
  [0x0a51, 0x0a51], [0x0a70, 0x0a71], [0x0a75, 0x0a75], [0x0a81, 0x0a83],
  [0x0abc, 0x0abc], [0x0abe, 0x0ac5], [0x0ac7, 0x0ac9], [0x0acb, 0x0acd],
  [0x0ae2, 0x0ae3], [0x0b01, 0x0b03], [0x0b3c, 0x0b3c], [0x0b3e, 0x0b44],
  [0x0b47, 0x0b48], [0x0b4b, 0x0b4d], [0x0b56, 0x0b57], [0x0b62, 0x0b63],
  [0x0b82, 0x0b82], [0x0bbe, 0x0bc2], [0x0bc6, 0x0bc8], [0x0bca, 0x0bcd],
  [0x0bd7, 0x0bd7], [0x0c00, 0x0c04], [0x0c3e, 0x0c44], [0x0c46, 0x0c48],
  [0x0c4a, 0x0c4d], [0x0c55, 0x0c56], [0x0c62, 0x0c63], [0x0c81, 0x0c83],
  [0x0cbc, 0x0cbc], [0x0cbe, 0x0cc4], [0x0cc6, 0x0cc8], [0x0cca, 0x0ccd],
  [0x0cd5, 0x0cd6], [0x0ce2, 0x0ce3], [0x0d01, 0x0d03], [0x0d3e, 0x0d44],
  [0x0d46, 0x0d48], [0x0d4a, 0x0d4d], [0x0d57, 0x0d57], [0x0d62, 0x0d63],
  [0x0d82, 0x0d83], [0x0dca, 0x0dca], [0x0dcf, 0x0dd4], [0x0dd6, 0x0dd6],
  [0x0dd8, 0x0ddf], [0x0df2, 0x0df3], [0x0e31, 0x0e31], [0x0e34, 0x0e3a],
  [0x0e47, 0x0e4e], [0x0eb1, 0x0eb1], [0x0eb4, 0x0eb9], [0x0ebb, 0x0ebc],
  [0x0ec8, 0x0ecd], [0x0f18, 0x0f19], [0x0f35, 0x0f35], [0x0f37, 0x0f37],
  [0x0f39, 0x0f39], [0x0f3e, 0x0f3f], [0x0f71, 0x0f84], [0x0f86, 0x0f87],
  [0x0f8d, 0x0f97], [0x0f99, 0x0fbc], [0x0fc6, 0x0fc6], [0x102b, 0x103e],
  [0x1056, 0x1059], [0x105e, 0x1060], [0x1062, 0x1064], [0x1067, 0x106d],
  [0x1071, 0x1074], [0x1082, 0x108d], [0x108f, 0x108f], [0x109a, 0x109d],
  [0x135d, 0x135f], [0x1712, 0x1714], [0x1732, 0x1734], [0x1752, 0x1753],
  [0x1772, 0x1773], [0x17b4, 0x17d3], [0x17dd, 0x17dd], [0x180b, 0x180e],
  [0x18a9, 0x18a9], [0x1920, 0x192b], [0x1930, 0x193b], [0x1a17, 0x1a1b],
  [0x1a55, 0x1a5e], [0x1a60, 0x1a7c], [0x1a7f, 0x1a7f], [0x1ab0, 0x1abe],
  [0x1b00, 0x1b04], [0x1b34, 0x1b44], [0x1b6b, 0x1b73], [0x1b80, 0x1b82],
  [0x1ba1, 0x1bad], [0x1be6, 0x1bf3], [0x1c24, 0x1c37], [0x1cd0, 0x1cd2],
  [0x1cd4, 0x1ce8], [0x1ced, 0x1ced], [0x1cf2, 0x1cf4], [0x1cf8, 0x1cf9],
  [0x1dc0, 0x1df9], [0x1dfb, 0x1dff], [0x200b, 0x200f], [0x202a, 0x202e],
  [0x2060, 0x2064], [0x2066, 0x206f], [0x20d0, 0x20f0], [0x2cef, 0x2cf1],
  [0x2d7f, 0x2d7f], [0x2de0, 0x2dff], [0x302a, 0x302f], [0x3099, 0x309a],
  [0xa66f, 0xa672], [0xa674, 0xa67d], [0xa69e, 0xa69f], [0xa6f0, 0xa6f1],
  [0xa802, 0xa802], [0xa806, 0xa806], [0xa80b, 0xa80b], [0xa823, 0xa827],
  [0xa880, 0xa881], [0xa8b4, 0xa8c4], [0xa8e0, 0xa8f1], [0xa926, 0xa92d],
  [0xa947, 0xa953], [0xa980, 0xa983], [0xa9b3, 0xa9c0], [0xa9e5, 0xa9e5],
  [0xaa29, 0xaa36], [0xaa43, 0xaa43], [0xaa4c, 0xaa4d], [0xaa7b, 0xaa7d],
  [0xaab0, 0xaab0], [0xaab2, 0xaab4], [0xaab7, 0xaab8], [0xaabe, 0xaabf],
  [0xaac1, 0xaac1], [0xaaeb, 0xaaef], [0xaaf5, 0xaaf6], [0xabe3, 0xabea],
  [0xabec, 0xabed], [0xfb1e, 0xfb1e], [0xfe00, 0xfe0f], [0xfe20, 0xfe2f],
  [0xfeff, 0xfeff], [0x101fd, 0x101fd], [0x102e0, 0x102e0], [0x10376, 0x1037a],
  [0x10a01, 0x10a0f], [0x10a38, 0x10a3f], [0x10ae5, 0x10ae6], [0x11000, 0x11002],
  [0x11038, 0x11046], [0x1107f, 0x11082], [0x110b0, 0x110ba], [0x11100, 0x11102],
  [0x11127, 0x11134], [0x11173, 0x11173], [0x11180, 0x11182], [0x111b3, 0x111c0],
  [0x1122c, 0x11237], [0x112df, 0x112ea], [0x11300, 0x11303], [0x1133c, 0x1133c],
  [0x1133e, 0x11344], [0x11347, 0x11348], [0x1134b, 0x1134d], [0x11357, 0x11357],
  [0x11362, 0x11363], [0x11435, 0x11446], [0x114b0, 0x114c3], [0x115af, 0x115b5],
  [0x115b8, 0x115c0], [0x11630, 0x11640], [0x116ab, 0x116b7], [0x1171d, 0x1172b],
  [0x11c2f, 0x11c36], [0x11c38, 0x11c3f], [0x11c92, 0x11ca7], [0x11ca9, 0x11cb6],
  [0x16af0, 0x16af4], [0x16b30, 0x16b36], [0x16f51, 0x16f92], [0x1bc9d, 0x1bc9e],
  [0x1d165, 0x1d169], [0x1d16d, 0x1d172], [0x1d17b, 0x1d182], [0x1d185, 0x1d18b],
  [0x1d1aa, 0x1d1ad], [0x1d242, 0x1d244], [0x1da00, 0x1da36], [0x1da3b, 0x1da6c],
  [0x1da75, 0x1da75], [0x1da84, 0x1da84], [0x1da9b, 0x1da9f], [0x1daa1, 0x1daaf],
  [0x1e000, 0x1e02a], [0x1e8d0, 0x1e8d6], [0x1e944, 0x1e94a], [0xe0100, 0xe01ef],
  [0x1f3fb, 0x1f3ff],
];

// Double-width characters (East Asian Wide/Fullwidth and emoji).
const WIDE: readonly Range[] = [
  [0x1100, 0x115f], [0x231a, 0x231b], [0x2329, 0x232a], [0x23e9, 0x23ec],
  [0x23f0, 0x23f0], [0x23f3, 0x23f3], [0x25fd, 0x25fe], [0x2614, 0x2615],
  [0x2648, 0x2653], [0x267f, 0x267f], [0x2693, 0x2693], [0x26a1, 0x26a1],
  [0x26aa, 0x26ab], [0x26bd, 0x26be], [0x26c4, 0x26c5], [0x26ce, 0x26ce],
  [0x26d4, 0x26d4], [0x26ea, 0x26ea], [0x26f2, 0x26f3], [0x26f5, 0x26f5],
  [0x26fa, 0x26fa], [0x26fd, 0x26fd], [0x2705, 0x2705], [0x270a, 0x270b],
  [0x2728, 0x2728], [0x274c, 0x274c], [0x274e, 0x274e], [0x2753, 0x2755],
  [0x2757, 0x2757], [0x2795, 0x2797], [0x27b0, 0x27b0], [0x27bf, 0x27bf],
  [0x2b1b, 0x2b1c], [0x2b50, 0x2b50], [0x2b55, 0x2b55], [0x2e80, 0x303e],
  [0x3041, 0x33ff], [0x3400, 0x4dbf], [0x4e00, 0x9fff], [0xa000, 0xa4cf],
  [0xa960, 0xa97f], [0xac00, 0xd7a3], [0xf900, 0xfaff], [0xfe10, 0xfe19],
  [0xfe30, 0xfe6f], [0xff00, 0xff60], [0xffe0, 0xffe6], [0x16fe0, 0x16fe4],
  [0x17000, 0x187f7], [0x18800, 0x18af2], [0x1b000, 0x1b11e], [0x1b170, 0x1b2fb],
  [0x1f004, 0x1f004], [0x1f0cf, 0x1f0cf], [0x1f18e, 0x1f18e], [0x1f191, 0x1f19a],
  [0x1f200, 0x1f320], [0x1f32d, 0x1f335], [0x1f337, 0x1f37c], [0x1f37e, 0x1f393],
  [0x1f3a0, 0x1f3ca], [0x1f3cf, 0x1f3d3], [0x1f3e0, 0x1f3f0], [0x1f3f4, 0x1f3f4],
  [0x1f3f8, 0x1f43e], [0x1f440, 0x1f440], [0x1f442, 0x1f4fc], [0x1f4ff, 0x1f53d],
  [0x1f54b, 0x1f54e], [0x1f550, 0x1f567], [0x1f57a, 0x1f57a], [0x1f595, 0x1f596],
  [0x1f5a4, 0x1f5a4], [0x1f5fb, 0x1f64f], [0x1f680, 0x1f6c5], [0x1f6cc, 0x1f6cc],
  [0x1f6d0, 0x1f6d2], [0x1f6d5, 0x1f6d7], [0x1f6eb, 0x1f6ec], [0x1f6f4, 0x1f6fc],
  [0x1f7e0, 0x1f7eb], [0x1f90c, 0x1f93a], [0x1f93c, 0x1f945], [0x1f947, 0x1f9ff],
  [0x1fa70, 0x1fa74], [0x1fa78, 0x1fa7a], [0x1fa80, 0x1fa86], [0x1fa90, 0x1faa8],
  [0x1fab0, 0x1fab6], [0x1fac0, 0x1fac2], [0x1fad0, 0x1fad6],
  [0x20000, 0x2fffd], [0x30000, 0x3fffd],
];

function inRanges(code: number, ranges: readonly Range[]): boolean {
  for (const [start, end] of ranges) if (code >= start && code <= end) return true;
  return false;
}

function charWidth(char: string): number {
  const code = char.codePointAt(0) ?? 0;
  if (code === 0) return 0;
  if (inRanges(code, ZERO_WIDTH)) return 0;
  if (inRanges(code, WIDE)) return 2;
  return 1;
}

function cellWidth(cell: Cell | undefined): number {
  if (cell === undefined || cell === '') return 0;
  return charWidth(cell);
}

export function canTakeKeyboard(): boolean {
  if (document.querySelector('[role=dialog][aria-modal=true]')) return false;
  const active = document.activeElement;
  if (!active || active === document.body || active === document.documentElement) return true;
  if (active instanceof Element && active.closest('[data-term], [data-vnc], [data-console-dock], [data-console-panel]')) return true;
  if (active instanceof HTMLElement && (/^(INPUT|SELECT|TEXTAREA)$/.test(active.tagName) || active.isContentEditable)) return false;
  return true;
}

function ctrlChar(key: string): string | null {
  const code = key.toLowerCase().charCodeAt(0);
  if (code >= 97 && code <= 122) return String.fromCharCode(code - 96);
  switch (key) {
    case ' ': case '@': case '2': return '\x00';
    case '[': return '\x1b';
    case '\\': return '\x1c';
    case ']': return '\x1d';
    case '^': case '6': return '\x1e';
    case '_': case '-': case '/': return '\x1f';
    case '?': case '8': return '\x7f';
    default: return null;
  }
}

interface SavedCursor { row: number; col: number; charset: 'B' | '0'; }
interface AltState { screen: Cell[][]; row: number; col: number; pendingWrap: boolean; top: number; bottom: number; }

export class TextTerminal {
  readonly root: HTMLDivElement;
  private readonly out: HTMLDivElement;
  private readonly history: HTMLDivElement;
  private readonly screenEl: HTMLDivElement;
  private readonly cursorEl: HTMLSpanElement;
  private readonly rowEls: HTMLDivElement[] = [];
  private send: (data: string) => void;
  onZoom: ((direction: number) => void) | null = null;
  onCapture: (() => void) | null = null;
  onPasteDenied: (() => void) | null = null;
  // The paste permission is granted by the administrator at the lab level. The terminal
  // itself decides whether to send text to the machine - this is the single point
  // through which both Ctrl+V and the clipboard button pass.
  allowPaste = true;
  captured = false;
  private font = 13;
  private screen: Cell[][] = [];
  private dirty: boolean[] = [];
  private pendingHistory: Cell[][] = [];
  private row = 0;
  private col = 0;
  // The cursor is past the last column: the next printable character wraps to a new line.
  private pendingWrap = false;
  private top = 0;
  private bottom = ROWS - 1;
  private state: 'text' | 'esc' | 'esc1' | 'csi' | 'osc' | 'oscesc' = 'text';
  private designator = '';
  private seq = '';
  private charset: 'B' | '0' = 'B';
  dec = new TextDecoder('utf-8');
  private raf = 0;
  private saved: SavedCursor | null = null;
  private cursorVisible = true;
  private autoWrap = true;
  private appCursor = false;
  private bracketedPaste = false;
  private alt: AltState | null = null;

  constructor(container: HTMLElement, send: (data: string) => void) {
    this.send = send;
    this.out = document.createElement('div');
    this.out.className = TERM_OUT;
    this.out.dataset['termOut'] = '';
    this.history = document.createElement('div');
    this.screenEl = document.createElement('div');
    this.screenEl.className = 'relative';
    this.cursorEl = document.createElement('span');
    this.cursorEl.className = `pointer-events-none absolute left-0 top-0 overflow-hidden text-center ${TERM_CUR}`;
    this.cursorEl.dataset['cursor'] = '';
    for (let index = 0; index < ROWS; index++) {
      const rowEl = document.createElement('div');
      rowEl.className = 'h-[1.6em] overflow-hidden whitespace-pre';
      this.rowEls.push(rowEl);
    }
    this.screenEl.append(...this.rowEls, this.cursorEl);
    this.out.append(this.history, this.screenEl);
    this.root = document.createElement('div');
    this.root.className = TERM_ROOT;
    this.root.dataset['term'] = '';
    this.root.tabIndex = 0;
    this.root.append(this.out);
    container.append(this.root);
    this.reset();
    this.root.addEventListener('keydown', (event) => this.onKey(event));
    this.root.addEventListener('focus', () => this.setCaptured(true));
    this.root.addEventListener('blur', () => this.setCaptured(false));
    this.root.addEventListener('paste', (event) => {
      event.preventDefault(); event.stopPropagation();
      const raw = event.clipboardData?.getData('text') ?? '';
      if (!raw) return;
      if (!this.allowPaste) { this.onPasteDenied?.(); return; }
      this.pasteText(raw);
    });
    this.root.addEventListener('mousedown', () => this.root.focus());
    this.root.addEventListener('wheel', (event) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      const direction = event.deltaY < 0 ? 1 : -1;
      if (this.onZoom) this.onZoom(direction); else this.setFont(this.font + direction);
    }, { passive: false });
  }

  sendText(data: string): void { this.send(data); }

  // Pasting text from the clipboard and the panel button. Returns false if
  // the administrator has disabled pasting in this lab.
  pasteText(text: string): boolean {
    if (!this.allowPaste || !text) return false;
    const normalized = text.replace(/\r?\n/g, '\r');
    // Bracketed paste (DECSET 2004): the shell/editor can distinguish a paste from typed text.
    this.sendText(this.bracketedPaste ? `\x1b[200~${normalized}\x1b[201~` : normalized);
    return true;
  }

  // Text selected with the mouse inside this terminal (for the "Copy" button).
  selectedText(): string {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return '';
    if (!this.root.contains(selection.getRangeAt(0).commonAncestorContainer)) return '';
    return selection.toString();
  }

  // The visible screen without trailing spaces - what the user sees in the terminal.
  screenText(): string {
    const rows = this.screen.map((cells) => cells.join('').trimEnd());
    while (rows.length > 0 && rows[rows.length - 1] === '') rows.pop();
    return rows.join('\n');
  }

  focusInput(): void {
    if (this.root.isConnected && this.root.offsetParent) this.root.focus({ preventScroll: true });
  }

  setCaptured(on: boolean): void {
    if (this.captured === on) return;
    this.captured = on;
    if (!on && this.root.ownerDocument.activeElement === this.root) this.root.blur();
    this.onCapture?.();
  }

  destroy(): void {
    this.setCaptured(false);
    this.root.remove();
  }

  feed(bytes: Uint8Array): void { this.write(this.dec.decode(bytes, { stream: true })); }

  setFont(px: number): void {
    this.font = Math.max(9, Math.min(26, Math.round(px)));
    this.root.style.fontSize = `${this.font}px`;
    this.fit();
  }

  // The size is fixed, so there is no need to fit the grid: it is enough to redraw the cursor
  // (the font may have changed, and the cursor position is specified in font units).
  fit(): void { this.schedule(); }

  write(text: string): void {
    for (const char of text) this.putChar(char);
    this.schedule();
  }

  reset(): void {
    this.screen = Array.from({ length: ROWS }, () => this.blankRow());
    this.pendingHistory = [];
    this.history.replaceChildren();
    this.row = 0; this.col = 0; this.pendingWrap = false;
    this.top = 0; this.bottom = ROWS - 1;
    this.state = 'text'; this.seq = ''; this.designator = '';
    this.charset = 'B';
    this.dec = new TextDecoder('utf-8');
    this.saved = null;
    this.alt = null;
    this.cursorVisible = true;
    this.autoWrap = true;
    this.appCursor = false;
    this.bracketedPaste = false;
    this.markAll();
    this.schedule();
  }

  resetDecoder(): void {
    this.dec = new TextDecoder('utf-8');
  }

  // ----- stream parsing -----
  private putChar(char: string): void {
    switch (this.state) {
      case 'osc':
        if (char === '\x07' || char === '\x18' || char === '\x1a') this.state = 'text';
        else if (char === '\x1b') this.state = 'oscesc';
        return;
      case 'oscesc':
        this.state = 'text';
        return;
      case 'esc':
        if (char === '[') { this.state = 'csi'; this.seq = ''; }
        else if (char === ']' || char === 'P' || char === '_' || char === '^' || char === 'X') this.state = 'osc';
        else if ('()*+'.includes(char)) { this.designator = char; this.state = 'esc1'; }
        else if (char === '#' || char === '%') { this.designator = ''; this.state = 'esc1'; }
        else { this.state = 'text'; this.esc(char); }
        return;
      case 'esc1':
        if (this.designator === '(') this.charset = char === '0' ? '0' : 'B';
        this.state = 'text';
        return;
      case 'csi':
        if (char === '\x1b') { this.state = 'esc'; this.seq = ''; }
        else if (char === '\x18' || char === '\x1a') this.state = 'text';
        else if (char >= '@' && char <= '~') { this.csi(this.seq, char); this.state = 'text'; }
        else if (char >= ' ') this.seq += char;
        return;
      default:
        break;
    }
    switch (char) {
      case '\x1b': this.state = 'esc'; return;
      case '\r': this.col = 0; this.pendingWrap = false; return;
      // Serial devices very often terminate a line with a single LF: LF means both a line feed
      // and a carriage return (LNM mode), otherwise each line starts at the previous line's column.
      case '\n': case '\x0b': case '\x0c': this.col = 0; this.index(); return;
      case '\b': if (this.col > 0) this.col--; this.pendingWrap = false; return;
      case '\t': this.col = Math.min(COLS - 1, (this.col | 7) + 1); this.pendingWrap = false; return;
      default: break;
    }
    const code = char.codePointAt(0) ?? 0;
    if (code < 32 || (code >= 0x7f && code <= 0x9f)) return;
    let out = char;
    if (this.charset === '0' && code >= 0x60 && code <= 0x7e) out = DEC_SPECIAL[char] ?? char;
    this.print(out);
  }

  private blankRow(): Cell[] {
    return Array.from({ length: COLS }, () => ' ');
  }

  private markAll(): void { this.dirty = Array<boolean>(ROWS).fill(true); }

  private markRows(from: number, to: number): void {
    for (let index = from; index <= to; index++) this.dirty[index] = true;
  }

  // Character width: 0 - combining, 2 - wide (CJK/emoji), 1 - normal.
  private print(char: string): void {
    const width = charWidth(char);
    if (width === 0) { this.attachZeroWidth(char); return; }
    if (this.pendingWrap) {
      this.pendingWrap = false;
      if (this.autoWrap) { this.col = 0; this.index(); } else this.col = COLS - 1;
    }
    if (width === 2 && this.col >= COLS - 1) {
      if (this.autoWrap) { this.col = 0; this.index(); } else this.col = COLS - 2;
    }
    this.setCell(this.row, this.col, char, width);
    this.col += width;
    if (this.col >= COLS) { this.col = COLS - 1; this.pendingWrap = true; }
  }

  private attachZeroWidth(char: string): void {
    const line = this.screen[this.row];
    if (!line) return;
    let index = this.pendingWrap ? COLS - 1 : this.col - 1;
    while (index > 0 && line[index] === '') index--;
    if (index < 0) return;
    const cell = line[index];
    if (cell === undefined || cell === '') return;
    line[index] = cell + char;
    this.dirty[this.row] = true;
  }

  // Writes a character without leaving "halves" of wide characters in neighboring cells.
  private setCell(row: number, col: number, char: string, width: number): void {
    const line = this.screen[row];
    if (!line) return;
    this.clearWideAt(line, col);
    if (width === 2) this.clearWideAt(line, col + 1);
    line[col] = char;
    if (width === 2 && col + 1 < COLS) line[col + 1] = '';
    this.dirty[row] = true;
  }

  // If the cell is part of a wide character, erase the whole character.
  private clearWideAt(line: Cell[], col: number): void {
    if (col < 0 || col >= COLS) return;
    if (line[col] === '') {
      if (col > 0) line[col - 1] = ' ';
      line[col] = ' ';
    } else if (col + 1 < COLS && line[col + 1] === '') {
      line[col + 1] = ' ';
    }
  }

  // Line feed inside the scroll region.
  private index(): void {
    if (this.row === this.bottom) this.scrollUp(1, true);
    else if (this.row < ROWS - 1) this.row++;
    this.pendingWrap = false;
  }

  private reverseIndex(): void {
    if (this.row === this.top) this.scrollDown(1);
    else if (this.row > 0) this.row--;
    this.pendingWrap = false;
  }

  private scrollUp(count: number, toHistory: boolean): void {
    for (let step = 0; step < count; step++) {
      const removed = this.screen.splice(this.top, 1)[0];
      this.screen.splice(this.bottom, 0, this.blankRow());
      // Only full-screen rows of the main buffer go into history, not the alternate buffer.
      if (toHistory && removed && this.top === 0 && this.bottom === ROWS - 1 && !this.alt) {
        this.pendingHistory.push(removed);
      }
    }
    this.markRows(this.top, this.bottom);
  }

  private scrollDown(count: number): void {
    for (let step = 0; step < count; step++) {
      this.screen.splice(this.bottom, 1);
      this.screen.splice(this.top, 0, this.blankRow());
    }
    this.markRows(this.top, this.bottom);
  }

  private setAltScreen(on: boolean): void {
    if (on) {
      if (this.alt) return;
      this.alt = { screen: this.screen, row: this.row, col: this.col, pendingWrap: this.pendingWrap, top: this.top, bottom: this.bottom };
      this.screen = Array.from({ length: ROWS }, () => this.blankRow());
      this.row = 0; this.col = 0; this.pendingWrap = false;
      this.top = 0; this.bottom = ROWS - 1;
      this.markAll();
    } else if (this.alt) {
      this.screen = this.alt.screen;
      this.row = this.alt.row; this.col = this.alt.col; this.pendingWrap = this.alt.pendingWrap;
      this.top = this.alt.top; this.bottom = this.alt.bottom;
      this.alt = null;
      this.markAll();
    }
  }

  private saveCursor(): void { this.saved = { row: this.row, col: this.col, charset: this.charset }; }

  private restoreCursor(): void {
    if (!this.saved) return;
    this.row = this.saved.row;
    this.col = this.saved.col;
    this.charset = this.saved.charset;
    this.pendingWrap = false;
  }

  private esc(char: string): void {
    switch (char) {
      case 'M': this.reverseIndex(); break;
      case 'D': this.index(); break;
      case 'E': this.col = 0; this.index(); break;
      case 'c': this.reset(); break;
      case '7': this.saveCursor(); break;
      case '8': this.restoreCursor(); break;
      default: break; // ESC = and ESC > (keyboard mode) and the like do not affect the grid
    }
  }

  private csi(params: string, final: string): void {
    if (params.startsWith('?')) { this.privateMode(params.slice(1), final); return; }
    if (params.startsWith('>') || params.startsWith('=') || params.startsWith('<')) return;
    const parsed = params === '' ? [] : params.split(';').map((value) => Number.parseInt(value, 10));
    const raw = (index: number): number | undefined => {
      const value = parsed[index];
      return value !== undefined && Number.isFinite(value) ? value : undefined;
    };
    const n = (index: number, fallback: number): number => {
      const value = raw(index);
      return value !== undefined && value > 0 ? value : fallback;
    };
    // ANSI arguments must not move the cursor off screen or cause extra allocations.
    const rowFloor = this.row >= this.top && this.row <= this.bottom ? this.top : 0;
    const rowCeil = this.row >= this.top && this.row <= this.bottom ? this.bottom : ROWS - 1;
    switch (final) {
      case 'A': this.row = Math.max(rowFloor, this.row - n(0, 1)); break;
      case 'B': case 'e': this.row = Math.min(rowCeil, this.row + n(0, 1)); break;
      case 'C': case 'a': this.col = Math.min(COLS - 1, this.col + n(0, 1)); break;
      case 'D': this.col = Math.max(0, this.col - n(0, 1)); break;
      case 'E': this.row = Math.min(rowCeil, this.row + n(0, 1)); this.col = 0; break;
      case 'F': this.row = Math.max(rowFloor, this.row - n(0, 1)); this.col = 0; break;
      case 'G': case '`': this.col = Math.min(COLS - 1, n(0, 1) - 1); break;
      case 'd': this.row = Math.min(ROWS - 1, n(0, 1) - 1); break;
      case 'H': case 'f':
        this.row = Math.min(ROWS - 1, n(0, 1) - 1);
        this.col = Math.min(COLS - 1, n(1, 1) - 1);
        break;
      case 'J': this.eraseDisplay(raw(0) ?? 0); break;
      case 'K': this.eraseLine(raw(0) ?? 0); break;
      case 'X': this.eraseCells(this.row, this.col, this.col + n(0, 1)); break;
      case 'P': this.deleteCells(n(0, 1)); break;
      case '@': this.insertCells(n(0, 1)); break;
      case 'L': this.insertLines(n(0, 1)); break;
      case 'M': this.deleteLines(n(0, 1)); break;
      case 'S': this.scrollUp(n(0, 1), true); break;
      case 'T': this.scrollDown(n(0, 1)); break;
      case 'r': {
        const top = n(0, 1) - 1;
        const bottom = n(1, ROWS) - 1;
        if (top < bottom && bottom < ROWS) {
          this.top = top; this.bottom = bottom;
          this.row = 0; this.col = 0;
        }
        this.pendingWrap = false;
        break;
      }
      case 'Z': this.col = Math.max(0, ((this.col - 1) & ~7)); break;
      case 'I': this.col = Math.min(COLS - 1, (this.col | 7) + 1); break;
      case 's': this.saveCursor(); break;
      case 'u': this.restoreCursor(); break;
      case 'n': {
        // DSR 6 - report the cursor position to the application (readline, full-screen editors).
        if (raw(0) === 6) {
          const line = this.row + 1;
          const column = Math.min(this.col, COLS - 1) + 1;
          this.send(`\x1b[${line};${column}R`);
        }
        break;
      }
      case 'c': {
        // DA - respond like VT102 so programs do not hang waiting.
        if (params === '' || raw(0) === 0) this.send('\x1b[?6c');
        break;
      }
      default: break; // SGR (m) and other sequences that do not affect the grid are ignored
    }
    this.pendingWrap = false;
  }

  private eraseDisplay(mode: number): void {
    if (mode === 0) {
      this.eraseLine(0);
      this.eraseRange(this.row + 1, ROWS - 1);
    } else if (mode === 1) {
      this.eraseRange(0, this.row - 1);
      this.eraseLine(1);
    } else if (mode === 2 || mode === 3) {
      this.eraseRange(0, ROWS - 1);
      if (mode === 3) this.history.replaceChildren();
    }
  }

  private eraseRange(from: number, to: number): void {
    for (let index = Math.max(0, from); index <= Math.min(ROWS - 1, to); index++) {
      this.screen[index] = this.blankRow();
      this.dirty[index] = true;
    }
  }

  private eraseLine(mode: number): void {
    if (mode === 0) this.eraseCells(this.row, this.col, COLS);
    else if (mode === 1) this.eraseCells(this.row, 0, this.col + 1);
    else this.eraseCells(this.row, 0, COLS);
  }

  private eraseCells(row: number, from: number, to: number): void {
    const line = this.screen[row];
    if (!line) return;
    for (let index = Math.max(0, from); index < Math.min(COLS, to); index++) {
      this.clearWideAt(line, index);
      line[index] = ' ';
    }
    this.dirty[row] = true;
  }

  private deleteCells(count: number): void {
    const line = this.screen[this.row];
    if (!line) return;
    this.clearWideAt(line, this.col);
    line.splice(this.col, Math.min(count, COLS - this.col));
    while (line.length < COLS) line.push(' ');
    this.clearWideAt(line, this.col);
    this.dirty[this.row] = true;
  }

  private insertCells(count: number): void {
    const line = this.screen[this.row];
    if (!line) return;
    this.clearWideAt(line, this.col);
    line.splice(this.col, 0, ...Array<Cell>(Math.min(count, COLS - this.col)).fill(' '));
    line.length = COLS;
    this.dirty[this.row] = true;
  }

  private insertLines(count: number): void {
    if (this.row < this.top || this.row > this.bottom) return;
    for (let step = 0; step < count; step++) {
      this.screen.splice(this.bottom, 1);
      this.screen.splice(this.row, 0, this.blankRow());
    }
    this.markRows(this.row, this.bottom);
  }

  private deleteLines(count: number): void {
    if (this.row < this.top || this.row > this.bottom) return;
    for (let step = 0; step < count; step++) {
      this.screen.splice(this.row, 1);
      this.screen.splice(this.bottom, 0, this.blankRow());
    }
    this.markRows(this.row, this.bottom);
  }

  private privateMode(spec: string, final: string): void {
    if (final !== 'h' && final !== 'l') return;
    const on = final === 'h';
    for (const part of spec.split(';')) {
      const mode = Number.parseInt(part, 10);
      if (!Number.isFinite(mode)) continue;
      switch (mode) {
        case 1: this.appCursor = on; break;        // DECCKM: application cursor keys
        case 7: this.autoWrap = on; break;         // DECAWM
        case 25: this.cursorVisible = on; break;   // DECTCEM
        case 47: case 1047: this.setAltScreen(on); break;
        case 1049:
          if (on) { this.saveCursor(); this.setAltScreen(true); }
          else { this.setAltScreen(false); this.restoreCursor(); }
          break;
        case 2004: this.bracketedPaste = on; break;
        default: break;                            // mouse and other modes are not needed
      }
    }
    this.pendingWrap = false;
  }

  // ----- rendering -----
  private schedule(): void {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => { this.raf = 0; this.render(); });
  }

  private isAtBottom(): boolean {
    return this.root.scrollHeight - this.root.scrollTop - this.root.clientHeight <= 4;
  }

  // Wide characters are rendered as inline-block 2ch wide so the grid stays even
  // even if the browser's fallback font is wider than two cells.
  private renderRow(el: HTMLElement, cells: Cell[]): void {
    const fragment = document.createDocumentFragment();
    let run = '';
    for (const cell of cells) {
      if (cell === '') continue;
      if (cellWidth(cell) === 2) {
        if (run) { fragment.append(run); run = ''; }
        const wide = document.createElement('span');
        wide.className = TERM_WIDE;
        wide.dataset['wide'] = '';
        wide.textContent = cell;
        fragment.append(wide);
      } else {
        run += cell;
      }
    }
    if (run) fragment.append(run);
    el.replaceChildren(fragment);
  }

  private render(): void {
    // The scroll decision is made from the state before the DOM change: if the user
    // is at the bottom, they stay at the bottom; if they scrolled up, their position does not change.
    const stick = this.isAtBottom();
    if (this.pendingHistory.length > 0) {
      for (const cells of this.pendingHistory) {
        const el = document.createElement('div');
        el.className = 'h-[1.6em] overflow-hidden whitespace-pre';
        this.renderRow(el, cells);
        this.history.append(el);
      }
      this.pendingHistory = [];
      while (this.history.childElementCount > MAX_HISTORY) this.history.firstElementChild?.remove();
    }
    for (let index = 0; index < ROWS; index++) {
      if (!this.dirty[index]) continue;
      this.renderRow(this.rowEls[index]!, this.screen[index] ?? this.blankRow());
      this.dirty[index] = false;
    }
    this.placeCursor();
    if (stick) this.root.scrollTop = this.root.scrollHeight;
  }

  private placeCursor(): void {
    const line = this.screen[this.row] ?? this.blankRow();
    let col = Math.min(this.col, COLS - 1);
    if (line[col] === '' && col > 0) col--;
    const ch = line[col] || ' ';
    const wide = charWidth(ch) === 2;
    this.cursorEl.textContent = ch;
    this.cursorEl.style.left = `${col}ch`;
    this.cursorEl.style.top = `${this.row * LINE_EM}em`;
    this.cursorEl.style.width = wide ? '2ch' : '1ch';
    this.cursorEl.style.height = `${LINE_EM}em`;
    this.cursorEl.style.lineHeight = `${LINE_EM}em`;
    this.cursorEl.style.display = this.cursorVisible ? '' : 'none';
  }

  private onKey(event: KeyboardEvent): void {
    event.stopPropagation();
    const key = event.key;
    if (event.isComposing && (key === 'Process' || key === 'Dead' || event.keyCode === 229 || [...key].length !== 1)) return;
    const latin = /^Key[A-Z]$/.test(event.code) && !/^[\x00-\x7f]$/.test(key) ? (event.shiftKey ? event.code[3] ?? '' : (event.code[3] ?? '').toLowerCase()) : key;
    if ((event.ctrlKey || event.metaKey) && !event.altKey && latin.toLowerCase() === 'v') return;
    const altGr = Boolean(event.getModifierState?.('AltGraph'));
    let data: string | null = null;
    if (key === 'Tab' && event.shiftKey) data = '\x1b[Z';
    else if (this.appCursor && Object.prototype.hasOwnProperty.call(APP_KEYMAP, key)) data = APP_KEYMAP[key] ?? null;
    else if (Object.prototype.hasOwnProperty.call(KEYMAP, key)) data = KEYMAP[key] ?? null;
    else if (/^F([1-9]|1[0-2])$/.test(key)) data = FKEYS[Number.parseInt(key.slice(1), 10) - 1] ?? null;
    else if (altGr && [...key].length === 1) data = key;
    else if (event.ctrlKey && !event.metaKey && latin.length === 1) {
      const control = ctrlChar(latin);
      if (control !== null) data = event.altKey ? '\x1b' + control : control;
    } else if (event.altKey && !event.ctrlKey && !event.metaKey && latin.length === 1) data = '\x1b' + latin;
    else if (!event.ctrlKey && !event.metaKey && [...key].length === 1) data = key;
    if (data === null) return;
    event.preventDefault();
    this.sendText(data);
  }
}
