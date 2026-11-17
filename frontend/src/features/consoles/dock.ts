import type { ConsoleDevice } from './types';
import { readConsoles, writeConsoles, WINDOW_ID } from './registry';
import type { ConsoleEntry } from './registry';
import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import type { Root } from 'react-dom/client';
import { TextTerminal, canTakeKeyboard } from '../terminal/TextTerminal';
import { VNCView, VNC_COMBOS } from '../vnc/VNCView';
import { QualityControl } from '../vnc/QualityControl';
import { createSerialKeyboard } from './serialKeys';
import { deviceIconSrc } from '../../lib/deviceIcon';
import { onLanguageChange, t } from '../../i18n';
import { CONTROL_PONG, ReconnectSocket } from './reconnect';
import { KBD_HEAD, KBD_CLOSE, KBD_ROW, CLIP_SERIAL_ROOT, CLIP_TEXT, CLIP_STATUS } from './styles';

type SessionState = 'ok' | 'ext';
const CLIPBOARD_PERM = 'console.clipboard';

const PRIMARY = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent hover:brightness-110';
const HEAD_BTN = 'px-[9px] py-0.5';
const DOCK_BASE = 'fixed z-[75] flex h-[520px] w-[820px] min-h-[280px] min-w-[420px] flex-col overflow-hidden border bg-panel shadow-[0_24px_60px_-24px_rgba(0,0,0,.75),0_2px_8px_rgba(0,0,0,.35),inset_0_1px_0_rgba(255,255,255,.05)] data-resizing:select-none data-resizing:[&_[data-console-panels]]:pointer-events-none [&:fullscreen_[data-resizer]]:hidden';
const DOCK_FLOAT = 'bottom-4 right-4 rounded-[16px] border-[rgba(148,163,184,.18)]';
const DOCK_PAGE = 'inset-0 h-auto w-auto rounded-none border-0 bg-[linear-gradient(180deg,#0b111a,#070a0f)] shadow-none [&_[data-term]]:px-[22px] [&_[data-term]]:py-[18px] [&_[data-vkbd]]:bottom-3.5 [&_[data-vclip]]:right-3.5 [&_[data-vclip]]:top-3.5';
const HEAD = 'flex flex-none cursor-move select-none items-center gap-1.5 border-b border-line bg-gradient-to-b from-panel2 to-panel px-2.5 py-2';
const HEAD_PAGE = 'rounded-none px-[18px] py-3';
const HEAD_FLOAT = 'rounded-t-[16px]';
const TABS = 'flex flex-none items-stretch gap-1.5 overflow-x-auto overflow-y-hidden whitespace-nowrap border-b border-line bg-gradient-to-b from-panel2 to-panel px-2.5 py-2 [scrollbar-width:thin] [&::-webkit-scrollbar]:h-1.5 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-[rgba(148,163,184,.22)]';
const TAB = 'relative flex max-w-[220px] flex-none cursor-pointer items-center gap-1.5 rounded-[10px] border border-transparent bg-[rgba(148,163,184,.07)] px-[11px] py-1.5 text-xs transition-[background,border-color,box-shadow] duration-150 hover:bg-[rgba(148,163,184,.13)]';
const TAB_ON = 'border-[rgba(129,140,248,.38)] bg-gradient-to-b from-accent/20 to-accent/10 shadow-[inset_0_0_0_1px_rgba(129,140,248,.10),0_2px_10px_-6px_rgba(99,102,241,.8)]';
const TAB_NAME = 'overflow-hidden text-ellipsis font-semibold';
const TAB_ICON = 'text-[11px]';
const TAB_X = 'rounded-[5px] px-[3px] opacity-55 hover:bg-bad/15 hover:text-bad hover:opacity-100';
const TAB_ADD = 'flex-none cursor-pointer self-center rounded-[9px] border border-dashed border-[rgba(129,140,248,.55)] bg-accent/[.08] px-[11px] py-[5px] text-xs text-text hover:border-accent hover:bg-accent/[.16]';
const DOT = 'inline-block size-2 flex-none rounded-full bg-[#6b7686]';
const DOT_ST: Record<'ok' | 'ext' | 'off', string> = {
  ok: 'bg-ok shadow-[0_0_8px_rgba(34,197,94,.55)]',
  ext: 'bg-muted',
  off: '',
};
const BAR = 'flex flex-none flex-wrap items-center gap-1.5 border-b border-line bg-panel px-2.5 py-1.5 [&>button]:px-[9px] [&>button]:py-[3px] [&>button]:text-xs';
const BAR_ON = 'border-accent bg-accent-soft text-accent';
const ZOOM = 'min-w-[44px] text-center text-xs tabular-nums text-muted';
const STATUS = 'max-w-[50%] overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted';
const PANELS = 'relative min-h-0 flex-1 bg-[#070a0f] shadow-[inset_0_1px_0_rgba(255,255,255,.03)]';
const PANEL = 'absolute inset-0 hidden';
const PANEL_EMPTY = 'absolute inset-0 hidden flex-col items-center justify-center gap-3 bg-[radial-gradient(90%_70%_at_50%_0%,rgba(99,102,241,.10),transparent_60%),linear-gradient(180deg,#0d131c,#090d13)] p-6 text-center text-muted';
const SIDE = 'invisible absolute bottom-0 left-0 z-[4] flex w-[250px] max-w-[70%] -translate-x-[101%] flex-col border-r border-line bg-panel shadow-[8px_0_24px_rgba(0,0,0,.35)] transition-transform duration-[180ms] ease-out';
const SIDE_HEAD = 'flex flex-none items-center gap-1.5 border-b border-line bg-panel2 px-2.5 py-1.5 text-xs';
const SIDE_LIST = 'min-h-0 flex-1 overflow-y-auto overscroll-contain p-1';
const SIDE_ITEM = 'mb-0.5 flex w-full cursor-pointer items-center gap-1.5 rounded-md border border-transparent bg-transparent px-2 py-1.5 text-left text-xs text-text hover:bg-panel2 disabled:cursor-default disabled:opacity-45';
const SIDE_ITEM_ON = 'border-accent bg-bg';
const SIDE_NAME = 'min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap';
const SIDE_META = 'whitespace-nowrap text-[10px] text-muted';
const LIST_ICO = 'mr-1.5 inline-block size-[18px] object-contain align-middle';
const MUTED = 'text-muted';
const DOCK_LIST = 'fixed z-[100] flex max-h-[60vh] min-w-[220px] flex-col overflow-auto rounded-app-sm border border-line bg-panel p-1.5 shadow-app';
const DOCK_LIST_BTN = 'whitespace-nowrap rounded-lg border-0 bg-transparent px-[11px] py-2 text-left font-medium text-text hover:bg-panel2 disabled:cursor-default disabled:opacity-45';
const RESIZERS: Record<string, string> = {
  n: 'absolute left-2.5 right-2.5 top-0 z-[5] h-1.5 cursor-ns-resize bg-transparent',
  s: 'absolute bottom-0 left-2.5 right-2.5 z-[5] h-1.5 cursor-ns-resize bg-transparent',
  e: 'absolute bottom-2.5 right-0 top-2.5 z-[5] w-1.5 cursor-ew-resize bg-transparent',
  w: 'absolute bottom-2.5 left-0 top-2.5 z-[5] w-1.5 cursor-ew-resize bg-transparent',
  ne: 'absolute right-0 top-0 z-[5] size-3.5 cursor-nesw-resize bg-transparent',
  nw: 'absolute left-0 top-0 z-[5] size-3.5 cursor-nwse-resize bg-transparent',
  se: 'absolute bottom-0 right-0 z-[5] size-3.5 cursor-nwse-resize bg-transparent',
  sw: 'absolute bottom-0 left-0 z-[5] size-3.5 cursor-nesw-resize bg-transparent',
};

async function copyText(text: string): Promise<boolean> {
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

async function readClipboardText(): Promise<string> {
  try { return (await navigator.clipboard?.readText?.()) ?? ''; } catch { return ''; }
}

interface Session {
  readonly kind: 'serial' | 'vnc';
  readonly dev: ConsoleDevice;
  readonly panel: HTMLDivElement;
  state: SessionState;
  message: string;
  layout: () => void;
  focusInput: () => void;
  reconnect: () => void;
  dispose: () => void;
}

const wsURL = (kind: 'serial' | 'vnc', id: number): string =>
  `${location.protocol === 'https:' ? 'wss://' : 'ws://'}${location.host}/ws/${kind}/${id}`;

class SerialSession implements Session {
  readonly kind = 'serial' as const;
  readonly dev: ConsoleDevice;
  readonly panel = document.createElement('div');
  readonly view: TextTerminal;
  state: SessionState = 'ok';
  message = '';
  kbd: HTMLDivElement | null = null;
  clip: HTMLDivElement | null = null;
  private clipText = '';
  private readonly dock: ConsoleDock;
  private readonly socket: ReconnectSocket;
  private received = 0;
  private initialized = false;

  constructor(dock: ConsoleDock, dev: ConsoleDevice) {
    this.dock = dock;
    this.dev = dev;
    this.panel.className = PANEL;
    this.panel.dataset['consolePanel'] = '';
    dock.panels.append(this.panel);
    this.view = new TextTerminal(this.panel, (data) => {
      // Terminal input is binary so text frames stay reserved for console control.
      const bytes = new TextEncoder().encode(data);
      this.socket.send(bytes);
    });
    this.view.onZoom = (direction) => { this.dock.zoom(direction); };
    this.view.onCapture = () => { this.dock.renderBar(); };
    this.view.onPasteDenied = () => { this.notify(t('Paste is disabled by the administrator')); };
    this.view.setFont(dock.font);
    this.socket = new ReconnectSocket({
      url: () => `${wsURL('serial', this.dev.rid)}${this.initialized ? `?after=${this.received}` : ''}`,
      open: () => { this.state = 'ok'; this.dock.render(); },
      message: (_ws, event) => this.onMessage(event.data),
    });
    this.socket.start();
  }

  layout(): void { this.view.fit(); }
  focusInput(): void { this.view.focusInput(); }

  private onMessage(data: unknown): void {
    if (typeof data === 'string') {
      if (data === 'netrave:replay') {
        this.view.reset();
        this.received = 0;
        this.initialized = true;
      } else if (data.startsWith('netrave:sync:')) {
        const total = Number.parseInt(data.slice('netrave:sync:'.length), 10);
        if (Number.isFinite(total)) {
          this.received = Math.max(0, total);
          this.initialized = true;
          this.view.resetDecoder();
        }
      } else if (data.startsWith('netrave:offset:')) {
        const total = Number.parseInt(data.slice('netrave:offset:'.length), 10);
        if (Number.isFinite(total)) {
          this.received = Math.max(0, total);
          this.initialized = true;
        }
      } else if (data === CONTROL_PONG) {
        // Heartbeat acknowledgement; arrival already refreshed the timeout.
      }
      return;
    }
    if (!(data instanceof ArrayBuffer)) return;
    const bytes = new Uint8Array(data);
    this.view.feed(bytes);
    this.received += bytes.byteLength;
  }

  private notify(text: string): void {
    this.message = text;
    this.dock.renderBar();
    window.setTimeout(() => {
      if (this.message !== text) return;
      this.message = '';
      this.dock.renderBar();
    }, 4000);
  }

  setClipboardAllowed(on: boolean): void {
    this.view.allowPaste = on;
    if (!on) this.closeClip();
  }

  toggleKbd(): void { if (this.kbd) this.closeKbd(); else this.openKbd(); }

  openKbd(): void {
    if (this.kbd) return;
    this.closeClip();
    this.kbd = createSerialKeyboard((data) => this.view.sendText(data), () => this.closeKbd());
    this.panel.append(this.kbd);
    this.dock.renderBar();
  }

  closeKbd(): void {
    if (!this.kbd) return;
    this.kbd.remove();
    this.kbd = null;
    this.dock.renderBar();
  }

  toggleClip(): void { if (this.clip) this.closeClip(); else this.openClip(); }

  openClip(): void {
    if (this.clip) return;
    this.closeKbd();
    const head = document.createElement('div');
    head.className = KBD_HEAD;
    const title = document.createElement('b');
    title.textContent = t('Clipboard');
    const close = document.createElement('button');
    close.type = 'button';
    close.className = KBD_CLOSE;
    close.title = t('Close');
    close.textContent = '✕';
    close.onclick = () => this.closeClip();
    head.append(title, close);

    const textarea = document.createElement('textarea');
    textarea.className = CLIP_TEXT;
    textarea.spellcheck = false;
    textarea.placeholder = t('Text to paste into VM (multiple lines allowed)');
    textarea.value = this.view.selectedText() || this.clipText;

    const status = document.createElement('div');
    status.className = CLIP_STATUS;

    const row = document.createElement('div');
    row.className = KBD_ROW;
    const add = (label: string, titleText: string, action: () => void): void => {
      const element = document.createElement('button');
      element.type = 'button';
      element.textContent = label;
      element.title = titleText;
      element.onclick = action;
      row.append(element);
    };
    add(t('Paste'), t('Paste into VM'), () => {
      status.textContent = this.view.pasteText(textarea.value)
        ? t('Text inserted into terminal')
        : t('Paste is disabled by the administrator');
    });
    add(t('Copy'), t('Copy'), () => {
      void copyText(textarea.value).then((ok) => {
        status.textContent = ok ? t('Copied to browser clipboard') : t('Browser did not allow copying');
      });
    });
    add(t('Copy screen'), t('Copy screen'), () => {
      const text = this.view.screenText();
      textarea.value = text;
      status.textContent = text ? t('Screen copied to the field') : t('Nothing to copy');
    });
    add(t('Read clipboard'), t('Read clipboard'), () => {
      void readClipboardText().then((text) => {
        if (!text) { status.textContent = t('Could not read the clipboard'); return; }
        textarea.value = text;
        status.textContent = t('Text received from the browser');
      });
    });
    add(t('Clear'), t('Clear'), () => {
      textarea.value = '';
      status.textContent = '';
    });

    const panel = document.createElement('div');
    panel.className = CLIP_SERIAL_ROOT;
    panel.dataset['vclip'] = '';
    panel.append(head, textarea, row, status);
    this.clip = panel;
    this.panel.append(panel);
    textarea.focus({ preventScroll: true });
    this.dock.renderBar();
  }

  closeClip(): void {
    if (!this.clip) return;
    const textarea = this.clip.querySelector('textarea');
    if (textarea) this.clipText = textarea.value;
    this.clip.remove();
    this.clip = null;
    this.dock.renderBar();
  }

  reconnect(): void { this.socket.restart(); }
  dispose(): void {
    this.socket.stop();
    this.closeKbd();
    this.closeClip();
    this.view.destroy();
    this.panel.remove();
  }
}

class VNCSession implements Session {
  readonly kind = 'vnc' as const;
  readonly dev: ConsoleDevice;
  readonly panel = document.createElement('div');
  state: SessionState = 'ok';
  message = '';
  readonly view: VNCView;
  // The quality panel is a React island inside the imperative toolbar.
  // The node is created once: renderBar moves it without losing state.
  readonly qualityHost: HTMLDivElement;
  private readonly qualityRoot: Root;
  private readonly dock: ConsoleDock;
  private readonly socket: ReconnectSocket;

  constructor(dock: ConsoleDock, dev: ConsoleDevice) {
    this.dock = dock;
    this.dev = dev;
    this.panel.className = PANEL;
    this.panel.dataset['consolePanel'] = '';
    dock.panels.append(this.panel);
    // One VNCView lives for the whole session: reconnects replace only its transport,
    // keeping the framebuffer, zoom, keyboard and clipboard state in place.
    this.view = new VNCView(this.panel, () => undefined);
    this.view.onCapture = () => this.dock.renderBar();
    this.view.onKbdChange = () => this.dock.renderBar();
    this.qualityHost = document.createElement('div');
    this.qualityHost.className = 'inline-flex';
    this.qualityHost.dataset['vncQuality'] = '';
    this.qualityRoot = createRoot(this.qualityHost);
    this.qualityRoot.render(createElement(QualityControl, { rid: dev.rid, view: this.view }));
    this.socket = new ReconnectSocket({
      url: () => wsURL('vnc', this.dev.rid),
      // VNC must come back to life immediately after a drop, without a noticeable pause:
      // the first attempt is instant, then a short cycle of 50-150 ms.
      immediateRetry: true,
      retry: { baseMs: 50, maxMs: 150 },
      attach: (ws) => { this.view.attach(ws); },
      open: () => { this.state = 'ok'; this.dock.render(); },
      message: (_ws, event) => {
        if (event.data instanceof ArrayBuffer) this.view.receive(new Uint8Array(event.data));
      },
    });
    this.socket.start();
  }

  layout(): void { this.view.layout(); }
  focusInput(): void { if (this.view.canvas.isConnected) this.view.canvas.focus({ preventScroll: true }); }

  setClipboardAllowed(on: boolean): void {
    this.view.allowClipboard = on;
    if (!on) this.view.closeClip();
    this.dock.renderBar();
  }

  reconnect(): void { this.socket.restart(); }
  dispose(): void {
    this.socket.stop();
    this.qualityRoot.unmount();
    this.view.destroy();
    this.panel.remove();
  }
}

const h = <K extends keyof HTMLElementTagNameMap>(tag: K, className?: string, text?: string): HTMLElementTagNameMap[K] => {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined) element.textContent = text;
  return element;
};

const button = (title: string, text: string, action: (event: MouseEvent) => void, className = ''): HTMLButtonElement => {
  const element = h('button', className, text);
  element.type = 'button'; element.title = title; element.onclick = action;
  return element;
};

function overlap(a: { left: number; top: number; right: number; bottom: number }, b: { left: number; top: number; right: number; bottom: number }): number {
  const width = Math.min(a.right, b.right) - Math.max(a.left, b.left);
  const height = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
  return width > 0 && height > 0 ? width * height : 0;
}

export class ConsoleDock {
  font = 13;
  private page: boolean;
  private sessions: Session[] = [];
  activeId: number | null = null;
  private minimized = false;
  private userMoved = false;
  private sideOpen = false;
  private sideOutside: ((event: MouseEvent) => void) | null = null;
  private listEl: HTMLDivElement | null = null;
  private outside: ((event: MouseEvent) => void) | null = null;
  private regionEl: HTMLElement | null = null;
  private shellEl: HTMLElement | null = null;
  private readonly titleEl: HTMLDivElement;
  private readonly sideBtn: HTMLButtonElement;
  private readonly head: HTMLDivElement;
  private readonly tabs: HTMLDivElement;
  private readonly bar: HTMLDivElement;
  readonly panels: HTMLDivElement;
  private readonly side: HTMLDivElement;
  private readonly sideList: HTMLDivElement;
  private readonly placeholder: HTMLDivElement;
  private readonly el: HTMLDivElement;
  private readonly regionObserver: ResizeObserver;
  private readonly shellObserver: MutationObserver;

  constructor(page: boolean) {
    this.page = page;
    // On a separate tab the terminal takes the whole screen, so the font is slightly larger.
    if (page) this.font = 14;
    this.titleEl = h('div');
    this.sideBtn = button(t('All terminals'), '☰', () => this.toggleSide(), HEAD_BTN);
    this.head = h('div', `${HEAD} ${page ? HEAD_PAGE : HEAD_FLOAT}`);
    this.head.dataset['dockHead'] = '';
    this.head.append(this.titleEl, this.sideBtn, h('span', 'flex-1'),
      button(t('Reconnect current'), '↻', () => this.active()?.reconnect()),
      button(t('Full screen'), '⛶', () => this.fullscreen()),
      ...(page ? [] : [button(t('Current console in a new tab'), '⧉', () => this.popOutActive()), button(t('Minimize'), '–', () => this.minimize())]),
      button(page ? t('Close') : t('Close all'), '✕', () => { if (page) window.close(); else this.closeAll(); }));
    this.tabs = h('div', `${TABS}${page ? ' px-[18px]' : ''}`);
    this.bar = h('div', `${BAR}${page ? ' bg-[rgba(14,21,32,.72)] px-[18px] py-2' : ''}`);
    this.panels = h('div', `${PANELS}${page ? ' bg-transparent' : ''}`);
    this.panels.dataset['consolePanels'] = '';
    this.placeholder = h('div', PANEL_EMPTY);
    this.panels.append(this.placeholder);
    this.sideList = h('div', SIDE_LIST);
    this.side = h('div', SIDE);
    this.side.append(h('div', SIDE_HEAD), this.sideList);
    const sideHead = this.side.firstElementChild as HTMLElement;
    sideHead.append(h('b', '', t('All terminals')), h('span', 'flex-1'));
    this.el = h('div', `hidden ${DOCK_BASE} ${page ? DOCK_PAGE : DOCK_FLOAT}`);
    this.el.dataset['consoleDock'] = '';
    this.el.dataset['dock'] = '';
    this.el.dataset['page'] = String(page);
    this.el.append(this.head, this.tabs, this.bar, this.panels, this.side);
    document.body.append(this.el);
    if (!page) { this.makeDraggable(); this.addResizers(); }
    this.regionObserver = new ResizeObserver(() => this.fitToRegion(false));
    this.shellObserver = new MutationObserver(() => this.fitToRegion(false));
    this.panels.addEventListener('mouseenter', () => this.focusActive());
    this.panels.addEventListener('mousemove', () => {
      const session = this.active();
      if (session && !session.panel.contains(document.activeElement)) this.focusActive();
    });
    new ResizeObserver(() => this.fitActive()).observe(this.panels);
    // Resizing the browser redraws the content but does not move the terminal.
    window.addEventListener('resize', () => this.fitActive());
    window.addEventListener('storage', (event) => { if (event.key === 'netrave.consoles.v2') this.sync(); });
    window.addEventListener('pagehide', () => { this.releaseAll(); this.watchOutside(); });
    onLanguageChange(() => this.render());
    this.sync();
    this.refreshPermissions();
  }

  sessionOf(id: number): Session | null { return this.sessions.find((session) => session.dev.rid === id) ?? null; }
  active(): Session | null { return this.activeId === null ? null : this.sessionOf(this.activeId); }

  sync(): void {
    const list = readConsoles();
    for (const session of [...this.sessions]) {
      const entry = list.find((item) => item.id === session.dev.rid);
      if (!entry || entry.owner !== WINDOW_ID || entry.kind !== session.kind) this.drop(session);
    }
    for (const entry of list) {
      if (entry.owner === WINDOW_ID && !this.sessionOf(entry.id)) {
        this.sessions.push(this.make(entry));
      }
    }
    if (!list.some((entry) => entry.id === this.activeId)) this.activeId = list[0]?.id ?? null;
    this.applyActive();
    this.render();
  }

  private make(entry: ConsoleEntry): Session {
    const known = deviceSource().find((item) => item.rid === entry.id);
    const device: ConsoleDevice = { rid: entry.id, name: entry.name, lab_id: entry.lab, state: 'running', console: entry.kind, lab_perms: known?.lab_perms ?? {} };
    return entry.kind === 'vnc' ? new VNCSession(this, device) : new SerialSession(this, device);
  }

  private drop(session: Session): void {
    const index = this.sessions.indexOf(session);
    if (index >= 0) this.sessions.splice(index, 1);
    session.dispose();
  }

  private applyActive(): void {
    for (const session of this.sessions) session.panel.classList.toggle('hidden', session.dev.rid !== this.activeId);
    const foreign = this.activeId !== null && !this.sessionOf(this.activeId);
    this.placeholder.classList.toggle('hidden', !foreign);
    this.placeholder.classList.toggle('flex', foreign);
    if (foreign) {
      const entry = readConsoles().find((item) => item.id === this.activeId);
      this.placeholder.replaceChildren(
        h('div', '', `${t('Console')} «${entry?.name ?? this.activeId}» ${t('Open in another window')}`),
        button(t('Open here'), t('Open here'), () => { if (this.activeId !== null) this.claim(this.activeId); }, PRIMARY),
      );
    }
    this.fitActive();
  }

  activate(id: number): void {
    this.activeId = id; this.applyActive(); this.render(); this.focusActive();
  }

  focusActive(): void {
    if (this.minimized || this.el.classList.contains('hidden')) return;
    requestAnimationFrame(() => { const session = this.active(); if (session && canTakeKeyboard()) session.focusInput(); });
  }

  fitActive(): void { this.active()?.layout(); this.renderBar(); }

  // The "Console" button in the lab: restores the collapsed dock or
  // shows the machine list if no console is open yet.
  showLauncher(anchor?: HTMLElement): void {
    if (this.sessions.length) {
      this.closeList();
      if (this.minimized) this.restore();
      this.focusActive();
      return;
    }
    if (anchor) this.toggleList(anchor);
  }

  open(device: ConsoleDevice, force = false, kind?: 'serial' | 'vnc'): void {
    const entryKind = kind ?? (device.console === 'vnc' ? 'vnc' : 'serial');
    const list = readConsoles();
    const entry = list.find((item) => item.id === device.rid);
    if (!entry) list.push({ id: device.rid, name: device.name, lab: device.lab_id || 0, owner: WINDOW_ID, kind: entryKind });
    else {
      if (force || !entry.owner) entry.owner = WINDOW_ID;
      if (kind) entry.kind = kind;
      entry.name = device.name; entry.lab = device.lab_id || 0;
    }
    writeConsoles(list);
    this.activeId = device.rid;
    this.sync();
    this.refreshPermissions();
    this.render();
    if (this.minimized) this.restore();
    this.focusActive();
  }

  private claim(id: number): void {
    const list = readConsoles();
    const entry = list.find((item) => item.id === id);
    if (entry) { entry.owner = WINDOW_ID; writeConsoles(list); }
    this.activeId = id; this.sync();
  }

  closeEntry(id: number): void {
    writeConsoles(readConsoles().filter((entry) => entry.id !== id));
    this.sync();
  }

  closeAll(): void { writeConsoles([]); this.sync(); }

  private releaseOwn(ids?: number[]): void {
    const list = readConsoles();
    for (const entry of list) if (entry.owner === WINDOW_ID && (!ids || ids.includes(entry.id))) entry.owner = null;
    writeConsoles(list); this.sync();
  }

  private releaseAll(): void {
    const list = readConsoles(); let changed = false;
    for (const entry of list) if (entry.owner === WINDOW_ID) { entry.owner = null; changed = true; }
    if (changed) writeConsoles(list);
  }

  private popOutAll(): void {
    const list = readConsoles();
    if (!list.length) return;
    const ids = list.map((entry) => entry.id);
    const active = list.some((entry) => entry.id === this.activeId) ? this.activeId : ids[0];
    const lab = list.find((entry) => entry.id === active)?.lab ?? 0;
    window.open(`/console.html?lab=${lab}&ids=${ids.join(',')}&active=${active ?? ''}`, '_blank');
    this.releaseOwn(ids);
  }

  private popOutActive(): void {
    const session = this.active();
    if (!session) return;
    window.open(`/console.html?lab=${session.dev.lab_id || 0}&ids=${session.dev.rid}&active=${session.dev.rid}`, '_blank');
    this.releaseOwn([session.dev.rid]);
  }

  zoom(direction: number): void {
    this.font = Math.max(9, Math.min(26, this.font + direction));
    for (const session of this.sessions) if (session instanceof SerialSession) session.view.setFont(this.font);
    this.renderBar();
  }

  syncStates(): void {
    // Connection recovery is owned by each session and is intentionally invisible.
    // A machine state poll may lag behind the socket, so it must not stop/restart it.
    this.refreshPermissions();
    this.render();
  }

  private clipboardAllowed(session: Session): boolean {
    return Boolean(session.dev.lab_perms?.[CLIPBOARD_PERM]);
  }

  // Clipboard permissions arrive with the lab topology and can
  // change while the console is open: an administrator grants or revokes them.
  private refreshPermissions(): void {
    const devices = deviceSource();
    for (const session of this.sessions) {
      const device = devices.find((item) => item.rid === session.dev.rid);
      if (device?.lab_perms) session.dev.lab_perms = device.lab_perms;
      const allowed = this.clipboardAllowed(session);
      if (session instanceof SerialSession) session.setClipboardAllowed(allowed);
      else if (session instanceof VNCSession) session.setClipboardAllowed(allowed);
    }
  }

  private makeDraggable(): void {
    this.head.addEventListener('mousedown', (event) => {
      if (event.button !== 0) return;
      if ((event.target as HTMLElement).closest('button')) return;
      event.preventDefault();
      const rect = this.el.getBoundingClientRect();
      const startX = event.clientX, startY = event.clientY;
      // The window is switched to absolute coordinates once, then moves via transform and does not
      // reflow the layout. The shadow is disabled during the move: large shadows in Safari
      // repaint on every frame and cause stutter. Coordinates are not clamped to the window.
      Object.assign(this.el.style, { right: 'auto', bottom: 'auto', left: `${rect.left}px`, top: `${rect.top}px`, willChange: 'transform', boxShadow: 'none' });
      let dx = 0, dy = 0, frame = 0;
      const paint = () => { frame = 0; this.el.style.transform = `translate3d(${dx}px, ${dy}px, 0)`; };
      const move = (moveEvent: MouseEvent) => {
        dx = moveEvent.clientX - startX;
        dy = moveEvent.clientY - startY;
        this.userMoved = true;
        if (!frame) frame = requestAnimationFrame(paint);
      };
      const up = () => {
        window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up);
        if (frame) cancelAnimationFrame(frame);
        Object.assign(this.el.style, { left: `${rect.left + dx}px`, top: `${rect.top + dy}px`, transform: '', willChange: '', boxShadow: '' });
      };
      window.addEventListener('mousemove', move); window.addEventListener('mouseup', up);
    });
  }

  private addResizers(): void {
    for (const direction of ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']) {
      const handle = h('div', RESIZERS[direction] ?? '');
      handle.dataset['resizer'] = '';
      handle.dataset['dockResize'] = direction;
      handle.addEventListener('mousedown', (event) => this.startResize(event, direction));
      this.el.append(handle);
    }
  }

  private startResize(event: MouseEvent, direction: string): void {
    if (event.button !== 0) return;
    event.preventDefault(); event.stopPropagation();
    const rect = this.el.getBoundingClientRect();
    const startX = event.clientX, startY = event.clientY;
    // Only minimum sizes are constrained. The window's position and edges are not bound
    // to the viewport: the terminal can be stretched and moved off screen.
    const minW = 420, minH = 260;
    this.userMoved = true;
    this.el.dataset['resizing'] = '';
    let last: MouseEvent | null = null, frame = 0;
    const paint = () => {
      frame = 0;
      if (!last) return;
      const dx = last.clientX - startX, dy = last.clientY - startY;
      let left = rect.left, top = rect.top, right = rect.right, bottom = rect.bottom;
      if (direction.includes('e')) right = Math.max(left + minW, right + dx);
      if (direction.includes('w')) left = Math.min(right - minW, left + dx);
      if (direction.includes('s')) bottom = Math.max(top + minH, bottom + dy);
      if (direction.includes('n')) top = Math.min(bottom - minH, top + dy);
      Object.assign(this.el.style, { right: 'auto', bottom: 'auto', left: `${left}px`, top: `${top}px`, width: `${right - left}px`, height: `${bottom - top}px` });
    };
    // The size changes at most once per frame: the layout is not recomputed on every mouse move.
    const move = (moveEvent: MouseEvent) => {
      last = moveEvent;
      if (!frame) frame = requestAnimationFrame(paint);
    };
    const up = () => {
      window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up);
      if (frame) { cancelAnimationFrame(frame); paint(); }
      delete this.el.dataset['resizing']; this.fitActive();
    };
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up);
  }

  private observeRegion(): void {
    const canvas = document.querySelector<HTMLElement>('[data-editor-canvas]');
    const shell = document.querySelector<HTMLElement>('[data-editor]');
    if (canvas !== this.regionEl) {
      this.regionObserver.disconnect(); this.regionEl = canvas;
      if (canvas) this.regionObserver.observe(canvas);
    }
    if (shell !== this.shellEl) {
      this.shellObserver.disconnect(); this.shellEl = shell;
      if (shell) this.shellObserver.observe(shell, { attributes: true, attributeFilter: ['class', 'data-drawer'] });
    }
  }

  private freeRegion(): { left: number; top: number; right: number; bottom: number; width: number; height: number } {
    const canvas = document.querySelector('[data-editor-canvas]');
    const rect = canvas ? canvas.getBoundingClientRect() : { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
    const side = document.querySelector('[data-editor-side]');
    const left = side && document.querySelector('[data-editor][data-drawer="true"]') ? Math.max(rect.left, side.getBoundingClientRect().right) : rect.left;
    return { left, top: rect.top, right: rect.right, bottom: rect.bottom, width: rect.right - left, height: rect.bottom - rect.top };
  }

  private freeSpot(region: { left: number; top: number; right: number; bottom: number }, width: number, height: number): { x: number; y: number } {
    const margin = 8;
    const x0 = region.left + margin, x1 = Math.max(x0, region.right - width - margin);
    const y0 = region.top + margin, y1 = Math.max(y0, region.bottom - height - margin);
    const obstacles = [...document.querySelectorAll('[data-editor-canvas] .node')].map((element) => element.getBoundingClientRect());
    const candidates: Array<[number, number]> = [[x1, y1], [x0, y1], [x1, y0], [x0, y0]];
    for (let y = y0; y <= y1; y += 24) for (let x = x0; x <= x1; x += 24) candidates.push([x, y]);
    let best = { x: x1, y: y1 }, cost = Number.POSITIVE_INFINITY;
    for (const [x, y] of candidates) {
      const box = { left: x, top: y, right: x + width, bottom: y + height };
      let value = 0;
      for (const obstacle of obstacles) value += overlap(box, obstacle);
      if (value < cost) { best = { x, y }; cost = value; if (value === 0) break; }
    }
    return best;
  }

  private fitToRegion(findSpot: boolean): void {
    this.observeRegion();
    if (this.page || this.minimized || this.el.classList.contains('hidden') || document.fullscreenElement === this.el) return;
    this.el.style.right = 'auto'; this.el.style.bottom = 'auto';
    // After the first move or resize the terminal is never
    // positioned automatically again - neither on window resize nor when the
    // workspace changes. Coordinates are not clamped to the viewport.
    if (this.userMoved) return;
    if (this.el.style.left && this.el.style.top && !findSpot) return;
    const region = this.freeRegion();
    const spot = this.freeSpot(region, this.el.offsetWidth, this.el.offsetHeight);
    this.el.style.left = `${spot.x}px`;
    this.el.style.top = `${spot.y}px`;
  }

  private minimize(): void { this.minimized = true; this.render(); }
  private restore(): void { this.minimized = false; this.render(); this.fitActive(); }
  private fullscreen(): void { if (document.fullscreenElement) void document.exitFullscreen(); else if (this.el.requestFullscreen) void this.el.requestFullscreen().catch(() => undefined); }

  render(): void {
    const list = readConsoles();
    const active = this.active();
    // On a separate tab the name of the open console is more useful in the header than a counter.
    this.titleEl.textContent = this.page
      ? `${t('Console')}${active ? ` · ${active.dev.name}` : ''}`
      : `${t('Console')}${list.length ? `: ${list.length}` : ''}`;
    this.titleEl.title = this.titleEl.textContent;
    this.el.classList.toggle('hidden', list.length === 0 || this.minimized);
    this.renderTabs(list); this.renderBar(); this.renderSide();
    this.fitToRegion(list.length > 0 && this.el.classList.contains('hidden'));
  }

  private renderTabs(list: ConsoleEntry[]): void {
    const tabs: HTMLElement[] = list.map((entry) => {
      const session = this.sessionOf(entry.id);
      const state: SessionState = session?.state ?? 'ext';
      const tab = h('div', `${TAB}${entry.id === this.activeId ? ` ${TAB_ON}` : ''}`);
      tab.dataset['dockTab'] = '';
      tab.title = entry.name;
      tab.onclick = () => this.activate(entry.id);
      const icon = h('span', TAB_ICON, entry.kind === 'vnc' ? '🖥' : '⌨');
      const dot = h('span', `${DOT} ${DOT_ST[state]}`);
      const name = h('span', TAB_NAME, entry.name);
      const close = h('span', TAB_X, '✕'); close.title = t('Close');
      close.onclick = (event) => { event.stopPropagation(); this.closeEntry(entry.id); };
      tab.append(icon, dot, name, close);
      return tab;
    });
    if (list.length >= 2) tabs.push(button(t('All in a new tab'), `⧉ ${t('All in a new tab')} (${list.length})`, () => this.popOutAll(), TAB_ADD));
    this.tabs.replaceChildren(...tabs);
  }

  renderBar(): void {
    const session = this.active();
    if (!session) { this.bar.replaceChildren(); return; }
    const status = h('span', STATUS, session.message || '');
    if (session instanceof SerialSession) {
      const canClipboard = this.clipboardAllowed(session);
      const keyboard = button(t('Virtual keyboard'), '⌨', () => session.toggleKbd(), session.kbd ? BAR_ON : '');
      keyboard.onmousedown = (event) => event.preventDefault();
      // Without clipboard permission the button is not shown at all, rather than just disabled.
      const clipboard = canClipboard
        ? (() => {
            const element = button(t('Terminal clipboard'), '📋', () => session.toggleClip(), session.clip ? BAR_ON : '');
            element.onmousedown = (event) => event.preventDefault();
            return element;
          })()
        : null;
      this.bar.replaceChildren(
        button(t('Font')+' -', 'A−', () => this.zoom(-1)), button(t('Font')+' +', 'A+', () => this.zoom(1)),
        h('span', ZOOM, `${this.font}px`),
        keyboard, ...(clipboard ? [clipboard] : []), h('span', 'flex-1'), status,
      );
      return;
    }
    const vncSession = session instanceof VNCSession ? session : null;
    const vnc = vncSession?.view ?? null;
    if (!vnc) { this.bar.replaceChildren(status); return; }
    const canClipboard = this.clipboardAllowed(session);
    const mode = (value: 'fit' | 'actual' | 'auto', label: string) => button(label, label, () => vnc.setMode(value), vnc.mode === value ? BAR_ON : '');
    const combo = (label: string, key: keyof typeof VNC_COMBOS) => {
      const element = button(`${t('Send Ctrl+Alt+Del to VM')}`, label, () => vnc.sendCombo(VNC_COMBOS[key] ?? []));
      element.onmousedown = (event) => event.preventDefault();
      return element;
    };
    this.bar.replaceChildren(
      mode('fit', 'Fit'), button('50%', '50%', () => vnc.setZoom(0.5)), button('75%', '75%', () => vnc.setZoom(0.75)),
      mode('actual', '100%'), button(t('Zoom out'), '−', () => vnc.stepZoom(-1)), h('span', ZOOM, `${Math.round(vnc.scale * 100)}%`),
      button(t('Zoom in'), '+', () => vnc.stepZoom(1)), mode('auto', 'Auto'), ...(vncSession ? [vncSession.qualityHost] : []), combo('Ctrl+Alt+Del', 'ctrlAltDel'), combo('Alt+Tab', 'altTab'),
      combo('Ctrl+Esc', 'ctrlEsc'), combo('Alt+F4', 'altF4'),
      (() => { const element = button(t('Virtual keyboard (Ctrl+Alt+K)'), '⌨', () => vnc.toggleKbd(), vnc.kbd ? BAR_ON : ''); element.onmousedown = (event) => event.preventDefault(); return element; })(),
      ...(canClipboard
        ? [(() => {
            const element = button(t('VNC clipboard'), '📋', () => vnc.toggleClip(), vnc.clip ? BAR_ON : '');
            element.onmousedown = (event) => event.preventDefault();
            return element;
          })()]
        : []),
      h('span', 'flex-1'), status,
    );
  }

  /** Watches for clicks outside all terminals' panel while it is open. */
  private toggleSide(): void {
    this.sideOpen = !this.sideOpen;
    this.side.style.top = `${this.head.offsetHeight}px`;
    this.side.classList.toggle('translate-x-0', this.sideOpen);
    this.side.classList.toggle('visible', this.sideOpen);
    this.side.classList.toggle('-translate-x-[101%]', !this.sideOpen);
    this.side.classList.toggle('invisible', !this.sideOpen);
    this.sideBtn.className = `${HEAD_BTN}${this.sideOpen ? ` ${BAR_ON}` : ''}`;
    this.watchOutside();
    this.renderSide();
  }

  /** Closes the panel of all terminals: a click anywhere outside its area. */
  private closeSide(): void {
    if (this.sideOpen) this.toggleSide();
  }

  private watchOutside(): void {
    if (this.sideOutside) { document.removeEventListener('mousedown', this.sideOutside); this.sideOutside = null; }
    if (!this.sideOpen) return;
    this.sideOutside = (event) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      // A click inside the panel or on its toggle button does not close it.
      if (this.side.contains(target) || this.sideBtn.contains(target)) return;
      this.closeSide();
    };
    document.addEventListener('mousedown', this.sideOutside);
  }

  private renderSide(): void {
    if (!this.sideOpen) return;
    const list = readConsoles();
    const rows: Array<{ id: number; name: string; device: ConsoleDevice | null }> = deviceSource().map((device) => ({ id: device.rid, name: device.name, device }));
    for (const entry of list) if (!rows.some((row) => row.id === entry.id)) rows.push({ id: entry.id, name: entry.name, device: null });
    this.sideList.replaceChildren(...(rows.length ? rows.map((row) => {
      const entry = list.find((item) => item.id === row.id);
      const session = entry ? this.sessionOf(row.id) : null;
      const machine = row.device;
      const running = machine ? machine.state === 'running' || machine.state === 'paused' : false;
      const canOpen = Boolean(entry) || (running && machine && canConsole(machine));
      const item = h('button', `${SIDE_ITEM}${row.id === this.activeId ? ` ${SIDE_ITEM_ON}` : ''}`);
      item.disabled = !canOpen;
      item.title = row.name;
      if (machine) {
        const image = h('img', LIST_ICO); image.src = deviceIconSrc(machine); image.alt = ''; image.width = 18; image.height = 18; item.append(image);
      } else item.append(h('span', TAB_ICON, entry?.kind === 'vnc' ? '🖥' : '⌨'));
      const state = entry ? session?.state ?? 'ext' : 'off';
      item.append(h('span', `${DOT} ${DOT_ST[state]}`), h('span', SIDE_NAME, row.name), h('span', SIDE_META, entry ? t('Open') : running ? 'running' : 'stopped'));
      item.onclick = () => { if (entry) this.activate(row.id); else if (machine) this.open(machine); };
      return item;
    }) : [h('div', MUTED, t('No machines in the laboratory'))]));
  }

  private toggleList(anchor: HTMLElement): void {
    if (this.listEl) { this.closeList(); return; }
    const available = deviceSource();
    const open = new Set(readConsoles().map((entry) => entry.id));
    this.listEl = h('div', DOCK_LIST);
    if (available.length) {
      for (const device of available) {
        const running = device.state === 'running' || device.state === 'paused';
        const item = h('button', DOCK_LIST_BTN, `${device.name}${open.has(device.rid) ? ' · '+t('Open') : ''}${running ? '' : ' · '+t('Stopped')}`);
        item.disabled = !running || !canConsole(device);
        item.onclick = () => { this.closeList(); this.open(device); };
        this.listEl.append(item);
      }
    } else this.listEl.append(h('div', MUTED, t('No machines in the laboratory')));
    document.body.append(this.listEl);
    const rect = anchor.getBoundingClientRect();
    this.listEl.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - this.listEl.offsetWidth - 8))}px`;
    this.listEl.style.bottom = `${window.innerHeight - rect.top + 6}px`;
    this.outside = (event) => { if (this.listEl && !this.listEl.contains(event.target as Node) && event.target !== anchor) this.closeList(); };
    window.setTimeout(() => this.outside && document.addEventListener('mousedown', this.outside), 0);
  }

  private closeList(): void {
    this.listEl?.remove(); this.listEl = null;
    if (this.outside) document.removeEventListener('mousedown', this.outside);
    this.outside = null;
  }
}

let dock: ConsoleDock | null = null;
let deviceSource: () => ConsoleDevice[] = () => [];

export function setDeviceSource(source: () => ConsoleDevice[]): void {
  deviceSource = source;
  dock?.syncStates();
}

export function closeAllConsoleSessions(): void {
  dock?.closeAll();
}

export function getDock(): ConsoleDock {
  if (!dock) dock = new ConsoleDock(false);
  return dock;
}

export function mountConsoleDock(page: boolean): ConsoleDock {
  dock?.closeAll();
  dock = new ConsoleDock(page);
  return dock;
}

function canConsole(device: ConsoleDevice): boolean {
  return !device.lab_perms || Boolean(device.lab_perms[device.console === 'vnc' ? 'console.vnc' : 'console.serial']);
}

export function focusDeviceConsole(rid: number): void {
  if (!dock || !rid || !dock.sessionOf(rid)) return;
  if (dock.activeId !== rid) dock.activate(rid); else dock.focusActive();
}
