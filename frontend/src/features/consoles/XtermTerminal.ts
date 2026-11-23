// The serial console on xterm.js. The font keeps its size, and the number of columns and rows grows
// or shrinks with the panel, so the text area is as wide and tall as the panel allows. The serial line
// carries no window size, so the guest still formats its output for 80x24; the terminal never shows
// fewer than that. Output is written as bytes (UTF-8 state is kept by xterm itself), and input goes out
// through `send` as typed text.
import { Terminal } from '@xterm/xterm';
import type { ITheme } from '@xterm/xterm';
import { WebglAddon } from '@xterm/addon-webgl';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { clipboardAction, copyText, readClipboardText } from './clipboard';

const COLS = 80;
const ROWS = 24;
const FONT_MIN = 9;
const FONT_MAX = 26;
const DEFAULT_FONT_SIZE = 15;
// More than 1: at exactly 1 the descenders and box-drawing glyphs of many monospace fonts touch
// or lose pixels, and the text lines look cramped.
const LINE_HEIGHT = 1.2;
// A font stack rather than the generic family: the cell width is measured from the actual font, so
// long lines wrap and mouse selection maps to the right column only when the metrics are known.
const FONT_FAMILY = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "DejaVu Sans Mono", monospace';

// A complete 16-color palette. The terminal renders 256-color and TrueColor sequences from the byte
// stream exactly as the guest writes them; minimumContrastRatio stays at 1, so the sent colors are
// not adjusted for contrast.
const THEME: ITheme = {
  background: '#070a0f',
  foreground: '#d8e1ec',
  cursor: '#d8e1ec',
  cursorAccent: '#070a0f',
  selectionBackground: 'rgba(99, 102, 241, .35)',
  selectionInactiveBackground: 'rgba(99, 102, 241, .18)',
  black: '#1d2733',
  red: '#e06c75',
  green: '#98c379',
  yellow: '#e5c07b',
  blue: '#61afef',
  magenta: '#c678dd',
  cyan: '#56b6c2',
  white: '#d8dee9',
  brightBlack: '#5c6370',
  brightRed: '#ff7b86',
  brightGreen: '#b5e890',
  brightYellow: '#ffd68a',
  brightBlue: '#7cb8ff',
  brightMagenta: '#d7a1f0',
  brightCyan: '#7ad4dd',
  brightWhite: '#ffffff',
};

// select-text: keep the text selectable even where a parent sets user-select.
const ROOT_CLASS = 'group/term relative box-border h-full w-full select-text overflow-auto bg-black [scrollbar-width:thin]';

/** Returns false while a dialog or a text field has the keyboard, so the terminal does not steal keys from it. */
export function canTakeKeyboard(): boolean {
  if (document.querySelector('[role=dialog][aria-modal=true]')) return false;
  const active = document.activeElement;
  if (!active || active === document.body || active === document.documentElement) return true;
  if (active instanceof Element && active.closest('[data-term], [data-vnc], [data-console-dock], [data-console-panel]')) return true;
  if (active instanceof HTMLElement && (/^(INPUT|SELECT|TEXTAREA)$/.test(active.tagName) || active.isContentEditable)) return false;
  return true;
}

export class XtermTerminal {
  readonly root: HTMLDivElement;
  // The xterm surface. It has no padding, so the fit addon measures exactly the area the grid can use.
  private readonly host: HTMLDivElement;
  private readonly term: Terminal;
  private readonly fitAddon = new FitAddon();
  private readonly send: (data: string) => void;
  onZoom: ((direction: number) => void) | null = null;
  onCapture: (() => void) | null = null;
  onPasteDenied: (() => void) | null = null;
  // The paste permission is granted by the administrator at the lab level. Pasting is blocked
  // here, before xterm sees the event, so Ctrl+V and the clipboard panel follow the same rule.
  allowPaste = true;
  captured = false;
  private fontSize = DEFAULT_FONT_SIZE;
  // Font size offset from the default size, changed by the zoom controls.
  private zoomOffset = 0;

  constructor(container: HTMLElement, send: (data: string) => void) {
    this.send = send;
    this.root = document.createElement('div');
    this.root.className = ROOT_CLASS;
    this.root.dataset['term'] = '';
    this.host = document.createElement('div');
    this.host.className = 'h-full w-full';
    this.root.append(this.host);
    container.append(this.root);

    this.term = new Terminal({
      cols: COLS,
      rows: ROWS,
      fontSize: this.fontSize,
      fontFamily: FONT_FAMILY,
      lineHeight: LINE_HEIGHT,
      letterSpacing: 0,
      scrollback: 1000,
      cursorBlink: false,
      cursorStyle: 'block',
      drawBoldTextInBrightColors: true,
      minimumContrastRatio: 1,
      theme: THEME,
    });
    // The WebGL renderer has to be loaded before open(): otherwise xterm creates its DOM renderer
    // first, and that renderer writes inline styles on every cell, which the strict
    // Content-Security-Policy (style-src 'self') blocks. WebGL draws to a canvas and writes no
    // inline styles. If the context is lost, the addon is disposed and xterm falls back to the DOM
    // renderer, so the terminal keeps working.
    this.loadWebgl();
    this.term.loadAddon(this.fitAddon);
    this.term.open(this.host);
    this.term.onData((data) => this.send(data));
    this.term.attachCustomKeyEventHandler((event) => this.onShortcutKey(event));

    // A context-menu copy produces a DOM copy event; the terminal selection is not DOM text, so it
    // is placed into the clipboard data explicitly.
    this.root.addEventListener('copy', (event) => {
      if (event.defaultPrevented) return;
      const text = this.term.getSelection();
      if (!text || !event.clipboardData) return;
      event.clipboardData.setData('text/plain', text);
      event.preventDefault();
    });

    this.root.addEventListener('paste', (event) => {
      if (this.allowPaste) return;
      event.preventDefault();
      event.stopPropagation();
      this.onPasteDenied?.();
    }, { capture: true });
    this.root.addEventListener('focusin', () => this.setCaptured(true));
    this.root.addEventListener('focusout', () => this.setCaptured(false));
    this.root.addEventListener('wheel', (event) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      const direction = event.deltaY < 0 ? 1 : -1;
      if (this.onZoom) this.onZoom(direction); else this.setZoomOffset(this.zoomOffset + direction);
    }, { passive: false });
  }

  /** Loads the WebGL renderer; on failure xterm keeps its DOM renderer. */
  private loadWebgl(): void {
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      this.term.loadAddon(webgl);
    } catch {
      // WebGL2 is unavailable: xterm uses its DOM renderer, and the console shows CSP warnings.
    }
  }

  /** Handles clipboard shortcuts and leaves every other key to the terminal and the guest. */
  private onShortcutKey(event: KeyboardEvent): boolean {
    const action = clipboardAction(event);
    if (!action) return true;
    event.preventDefault();
    if (action === 'copy') void this.copySelection();
    else void this.pasteClipboard();
    return false;
  }

  /** Copies the current mouse selection, if there is one. */
  private async copySelection(): Promise<void> {
    const text = this.term.getSelection();
    if (text) await copyText(text);
  }

  /** Pastes clipboard text through the terminal, honouring the administrator's permission. */
  private async pasteClipboard(): Promise<void> {
    if (!this.allowPaste) { this.onPasteDenied?.(); return; }
    const text = await readClipboardText();
    if (text) this.term.paste(text);
  }

  /** Sends text to the machine as if it were typed (used by the virtual keyboard). */
  sendText(data: string): void {
    this.term.input(data);
  }

  /** Pastes text into the machine. Returns false if the administrator has disabled pasting. */
  pasteText(text: string): boolean {
    if (!this.allowPaste || !text) return false;
    this.term.paste(text);
    return true;
  }

  /** The text selected with the mouse inside this terminal. */
  selectedText(): string {
    return this.term.getSelection();
  }

  /** The visible screen without trailing spaces and empty lines at the bottom. */
  screenText(): string {
    const buffer = this.term.buffer.active;
    const rows: string[] = [];
    for (let y = 0; y < this.term.rows; y++) rows.push(buffer.getLine(buffer.baseY + y)?.translateToString(true) ?? '');
    while (rows.length > 0 && rows[rows.length - 1] === '') rows.pop();
    return rows.join('\n');
  }

  focusInput(): void {
    if (this.root.isConnected && this.root.offsetParent) this.term.focus();
  }

  setCaptured(on: boolean): void {
    if (this.captured === on) return;
    this.captured = on;
    this.onCapture?.();
  }

  /** The font size in use now, including the zoom offset. */
  get currentFont(): number {
    return this.fontSize;
  }

  /** Changes the font size offset and refits the grid at once. */
  setZoomOffset(offset: number): void {
    this.zoomOffset = offset;
    this.fit();
  }

  private applyFont(px: number): void {
    const next = Math.max(FONT_MIN, Math.min(FONT_MAX, Math.round(px)));
    this.fontSize = next;
    if (this.term.options.fontSize === next) return;
    // xterm measures the cell size synchronously when the font option changes, so the grid size
    // read right after this call is current.
    this.term.options.fontSize = next;
    this.term.refresh(0, this.term.rows - 1);
  }

  /**
   * Fits the terminal grid to the panel. The font keeps its default size (the zoom offset changes it),
   * and the number of columns and rows is as large as the panel allows, never below 80x24. On a narrow
   * panel, where 80 columns would not fit at that size, the font shrinks so that the 80 columns still
   * show without horizontal scrolling. The guest formats its output for 80x24 on the serial line.
   * Called when the panel becomes visible or changes size. While the panel is hidden it has no size,
   * so the call does nothing and the last grid stays in place.
   */
  fit(): void {
    if (this.host.clientWidth === 0 || this.host.clientHeight === 0) return;
    this.applyFont(DEFAULT_FONT_SIZE + this.zoomOffset);
    this.remeasure();
    let dims = this.fitAddon.proposeDimensions();
    // Narrow panel: shrink the font until 80 columns fit, but never below the minimum size.
    while (dims && dims.cols < COLS && this.fontSize > FONT_MIN) {
      this.applyFont(this.fontSize - 1);
      this.remeasure();
      dims = this.fitAddon.proposeDimensions();
    }
    if (!dims) return;
    const cols = Math.max(COLS, dims.cols);
    const rows = Math.max(ROWS, dims.rows);
    if (cols !== this.term.cols || rows !== this.term.rows) this.term.resize(cols, rows);
    this.term.refresh(0, this.term.rows - 1);
  }

  /** Makes xterm measure the cell size again by changing the font option and restoring it. */
  private remeasure(): void {
    // A terminal opened while its panel was hidden keeps an empty cell measurement, so the cell size
    // is measured again now that the panel has a size.
    const size = this.fontSize;
    this.term.options.fontSize = size + 1;
    this.term.options.fontSize = size;
  }

  /** Writes bytes received from the serial line. */
  feed(bytes: Uint8Array): void {
    this.term.write(bytes);
  }

  /** Clears the screen and the scrollback before the server replays the console history. */
  reset(): void {
    this.term.reset();
  }

  /** xterm keeps its UTF-8 decoder state between writes, so there is nothing to reset. */
  resetDecoder(): void {}

  destroy(): void {
    this.term.dispose();
    this.root.remove();
  }
}
