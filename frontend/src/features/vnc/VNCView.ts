import { t } from '../../i18n';
import { KEYSYM_SHIFT_L, latin1Payload, typingPlan } from './keyboardText';
import { KBD_ROOT, KBD_HEAD, KBD_CLOSE, KBD_ROW, KBD_KEY, KBD_ON, KBD_COMBO, CLIP_ROOT, CLIP_TEXT, CLIP_STATUS } from '../consoles/styles';
import { Inflater } from './inflate';
import { DEFAULT_QUALITY, effectiveQuality, resolutionSize, rfbEncodings } from './quality';
import type { EffectiveQuality } from './quality';
import type { VncQualitySettings } from '../../types/api';

const KEY_SPECIAL: Record<string, number> = {
  Enter: 0xff0d, Backspace: 0xff08, Tab: 0xff09, Escape: 0xff1b, Delete: 0xffff, Insert: 0xff63,
  Home: 0xff50, End: 0xff57, PageUp: 0xff55, PageDown: 0xff56,
  ArrowLeft: 0xff51, ArrowUp: 0xff52, ArrowRight: 0xff53, ArrowDown: 0xff54,
  Shift: 0xffe1, Control: 0xffe3, Alt: 0xffe9, Meta: 0xffeb, CapsLock: 0xffe5,
  ContextMenu: 0xff67, Pause: 0xff13, PrintScreen: 0xff61, ScrollLock: 0xff14, NumLock: 0xff7f, AltGraph: 0xfe03,
};
const VKBD_MODS: Array<[string, number]> = [['Ctrl', 0xffe3], ['Alt', 0xffe9], ['Shift', 0xffe1], ['Win', 0xffeb]];
const VKBD_KEYS: Array<[string, number]> = [['Esc', 0xff1b], ['Tab', 0xff09], ['Enter', 0xff0d], ['Bksp', 0xff08], ['Space', 0x20], ['Del', 0xffff], ['Ins', 0xff63], ['Home', 0xff50], ['End', 0xff57], ['PgUp', 0xff55], ['PgDn', 0xff56], ['←', 0xff51], ['↑', 0xff52], ['↓', 0xff54], ['→', 0xff53], ['PrtSc', 0xff61], ['Pause', 0xff13], ['Menu', 0xff67]];
const VKBD_FKEYS: Array<[string, number]> = Array.from({ length: 12 }, (_, index) => [`F${index + 1}`, 0xffbe + index]);
const VKBD_COMBOS: Array<[string, number[]]> = [['Ctrl+Alt+Del', [0xffe3, 0xffe9, 0xffff]], ['Alt+Tab', [0xffe9, 0xff09]], ['Ctrl+Esc', [0xffe3, 0xff1b]], ['Ctrl+Shift+Esc', [0xffe3, 0xffe1, 0xff1b]], ['Alt+F4', [0xffe9, 0xffc1]], ['Ctrl+Alt+Bksp', [0xffe3, 0xffe9, 0xff08]]];

const VNC_ROOT = 'flex h-full w-full items-start justify-center overflow-auto bg-black';
const VNC_CANVAS = 'm-auto block flex-none outline-none [image-rendering:auto] focus-visible:outline focus-visible:outline-1 focus-visible:-outline-offset-1 focus-visible:outline-line-2';

export const VNC_COMBOS: Record<string, number[]> = {
  ctrlAltDel: [0xffe3, 0xffe9, 0xffff], altTab: [0xffe9, 0xff09], ctrlEsc: [0xffe3, 0xff1b], altF4: [0xffe9, 0xffc1],
};

const encoder = new TextEncoder();
const bytes = (text: string): Uint8Array => encoder.encode(text);
const view = (buffer: Uint8Array): DataView => new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);

function pixelFormat(): Uint8Array {
  const buffer = new Uint8Array(20), data = view(buffer);
  buffer[0] = 0; buffer[4] = 32; buffer[5] = 24; buffer[6] = 0; buffer[7] = 1;
  data.setUint16(8, 255); data.setUint16(10, 255); data.setUint16(12, 255);
  buffer[14] = 16; buffer[15] = 8; buffer[16] = 0;
  return buffer;
}

function latin1Text(data: Uint8Array): string {
  let text = '';
  for (const byte of data) text += String.fromCharCode(byte);
  return text;
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(text); return true; }
  } catch { /* fallback */ }
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

function lockKeyboard(on: boolean): void {
  const keyboard = (navigator as Navigator & { keyboard?: { lock: () => Promise<void>; unlock: () => void } }).keyboard;
  if (!keyboard || typeof keyboard.lock !== 'function') return;
  if (on && document.fullscreenElement) void keyboard.lock().catch(() => undefined);
  else keyboard.unlock();
}

// The guest keyboard is a US layout. A key pressed on another host layout (Cyrillic, German, ...)
// produces a character the guest keymap cannot type, so such a key is sent as the US character of the
// same physical key: the guest receives the key that was pressed, whatever the host layout is.
const US_SYMBOLS: Record<string, [string, string]> = {
  Backquote: ['`', '~'], Digit1: ['1', '!'], Digit2: ['2', '@'], Digit3: ['3', '#'], Digit4: ['4', '$'],
  Digit5: ['5', '%'], Digit6: ['6', '^'], Digit7: ['7', '&'], Digit8: ['8', '*'], Digit9: ['9', '('],
  Digit0: ['0', ')'], Minus: ['-', '_'], Equal: ['=', '+'], BracketLeft: ['[', '{'], BracketRight: [']', '}'],
  Backslash: ['\\', '|'], Semicolon: [';', ':'], Quote: ["'", '"'], Comma: [',', '<'], Period: ['.', '>'],
  Slash: ['/', '?'],
};

export function usCharacter(event: KeyboardEvent): string | null {
  const letter = /^Key([A-Z])$/.exec(event.code);
  if (letter) {
    const upper = event.shiftKey !== event.getModifierState('CapsLock');
    return upper ? (letter[1] ?? null) : (letter[1] ?? '').toLowerCase();
  }
  const pair = US_SYMBOLS[event.code];
  if (!pair) return null;
  return event.shiftKey ? pair[1] : pair[0];
}

export function keysym(event: KeyboardEvent): number | null {
  if (event.key === 'Shift' && event.location === 2) return 0xffe2;
  if (Object.prototype.hasOwnProperty.call(KEY_SPECIAL, event.key)) return KEY_SPECIAL[event.key] ?? null;
  const functionKey = /^F([1-9]|1[0-2])$/.exec(event.key);
  if (functionKey) return 0xffbd + Number.parseInt(functionKey[1] ?? '0', 10);
  if (event.key.length === 1 && event.key.charCodeAt(0) >= 0x80) {
    const latin = usCharacter(event);
    if (latin) return latin.codePointAt(0) ?? null;
  }
  if (event.key.length === 1 || (event.key.length === 2 && (event.key.codePointAt(0) ?? 0) > 0xffff)) {
    const code = event.key.codePointAt(0) ?? 0;
    return code < 0x100 ? code : 0x01000000 + code;
  }
  return null;
}

function keyId(event: KeyboardEvent): string { return event.code || event.key; }

interface ImageJob {
  x: number;
  y: number;
  width: number;
  height: number;
  data: Uint8Array;
}

export class VNCView {
  readonly root: HTMLDivElement;
  readonly canvas: HTMLCanvasElement;
  private ws: WebSocket | null = null;
  private ctx: CanvasRenderingContext2D;
  private onStatus: (text: string, fatal: boolean) => void;
  private queue: Uint8Array[] = [];
  private queueLength = 0;
  private buttons = 0;
  mode: 'fit' | 'actual' | 'zoom' | 'auto' = 'fit';
  scale = 1;
  private autoTimer = 0;
  private autoSent: { w: number; h: number } | null = null;
  // Image settings are applied to the live RFB connection without reconnecting.
  private settings: VncQualitySettings = { ...DEFAULT_QUALITY };
  private effective: EffectiveQuality = effectiveQuality(DEFAULT_QUALITY);
  private ready = false;
  private updateTimer = 0;
  private readonly inflaters = [new Inflater(), new Inflater(), new Inflater(), new Inflater()];
  private imageJobs: ImageJob[] = [];
  private paints = 0;
  private pendingFullUpdate = false;
  private pendingResolution: { width: number; height: number } | null = null;
  private lastResolution = '';
  resolutionError = '';
  resolutionNotice = '';
  onQualityChange: (() => void) | null = null;
  private pressed = new Map<string, number>();
  // Keys the host handled itself (Ctrl+Shift+V); their key-up must not reach the guest.
  private swallowed = new Set<string>();
  captured = false;
  // The clipboard permission is granted by the administrator at the lab level.
  // Without it the panel does not open and text from the VM is not copied automatically.
  allowClipboard = true;
  onCapture: (() => void) | null = null;
  kbd: HTMLDivElement | null = null;
  onKbdChange: (() => void) | null = null;
  private kbdHeld = new Map<string, number>();
  private kbdButtons = new Map<string, HTMLButtonElement>();
  private hotkeyDown = false;
  clip: HTMLDivElement | null = null;
  private clipTa: HTMLTextAreaElement | null = null;
  private clipStatus: HTMLDivElement | null = null;
  private lastCut = '';
  private typing: { stop: boolean; timer: number } | null = null;
  private ro: ResizeObserver;
  private generator: Generator<number, void, Uint8Array>;
  private need: number | null;
  private onWindowBlur: () => void;
  private onHotkey: (event: KeyboardEvent) => void;
  private onFullscreen: () => void;

  constructor(container: HTMLElement, onStatus: (text: string, fatal: boolean) => void) {
    this.onStatus = onStatus;
    this.canvas = document.createElement('canvas');
    this.canvas.className = VNC_CANVAS;
    this.canvas.tabIndex = 0;
    const context = this.canvas.getContext('2d');
    if (!context) throw new Error(t('Canvas 2D is unavailable'));
    this.ctx = context;
    this.root = document.createElement('div');
    this.root.className = VNC_ROOT;
    this.root.dataset['vnc'] = '';
    this.root.append(this.canvas);
    container.append(this.root);
    this.ro = new ResizeObserver(() => this.layout());
    this.ro.observe(this.root);
    this.generator = this.protocol();
    this.need = this.generator.next().value ?? null;
    this.onWindowBlur = () => this.releaseAll();
    this.onHotkey = (event) => {
      if (this.root.offsetParent === null) return;
      const key = event.code === 'KeyK' || event.key === 'k' || event.key === 'K';
      if (event.type === 'keyup') {
        if (this.hotkeyDown && key) { this.hotkeyDown = false; event.preventDefault(); event.stopPropagation(); }
        return;
      }
      if (!(key && event.ctrlKey && event.altKey && !event.shiftKey && !event.metaKey)) return;
      event.preventDefault(); event.stopPropagation(); this.hotkeyDown = true;
      if (!event.repeat) this.toggleKbd();
    };
    this.onFullscreen = () => { if (this.captured) lockKeyboard(Boolean(document.fullscreenElement)); };
    this.bindInput();
  }

  // The screen, zoom, keyboard and clipboard live in this object. A reconnect
  // replaces only the transport and restarts the RFB conversation in place.
  attach(ws: WebSocket): void {
    this.ws = ws;
    this.queue = [];
    this.queueLength = 0;
    this.autoSent = null;
    this.ready = false;
    this.pendingResolution = null;
    this.imageJobs = [];
    this.paints = 0;
    this.pendingFullUpdate = false;
    window.clearTimeout(this.updateTimer);
    for (const inflater of this.inflaters) inflater.reset();
    this.generator = this.protocol();
    this.need = this.generator.next().value ?? null;
  }

  get quality(): VncQualitySettings { return { ...this.settings }; }
  get resolution(): string { return this.lastResolution || '—'; }
  get resolutionPending(): boolean { return this.pendingResolution !== null; }

  receive(chunk: Uint8Array): void { this.onData(chunk); }

  private send(data: Uint8Array): void {
    const ws = this.ws;
    if (ws && ws.readyState === 1) ws.send(data as Uint8Array<ArrayBuffer>);
  }

  private onData(chunk: Uint8Array): void {
    this.queue.push(chunk); this.queueLength += chunk.length;
    try {
      while (this.need !== null && this.queueLength >= this.need) {
        const result = this.generator.next(this.pull(this.need));
        if (result.done) { this.need = null; break; }
        this.need = result.value;
      }
    } catch (error) {
      this.need = null;
      this.onStatus(error instanceof Error ? error.message : String(error), true);
      try { this.ws?.close(); } catch { /* ignore */ }
    }
  }

  private pull(count: number): Uint8Array {
    const out = new Uint8Array(count);
    let offset = 0;
    while (offset < count) {
      const chunk = this.queue[0];
      if (!chunk) break;
      const size = Math.min(chunk.length, count - offset);
      out.set(chunk.subarray(0, size), offset);
      offset += size;
      if (size === chunk.length) this.queue.shift(); else this.queue[0] = chunk.subarray(size);
    }
    this.queueLength -= count;
    return out;
  }

  private *protocol(): Generator<number, void, Uint8Array> {
    const version = yield 12;
    if (new TextDecoder().decode(version).slice(0, 4) !== 'RFB ') throw new Error(t('Error'));
    this.send(bytes('RFB 003.008\n'));
    const count = (yield 1)[0] ?? 0;
    if (count === 0) {
      const length = view(yield 4).getUint32(0);
      throw new Error(new TextDecoder().decode(yield length) || t('Error'));
    }
    const types = yield count;
    if (!types.includes(1)) throw new Error(t('Error'));
    this.send(Uint8Array.of(1));
    if (view(yield 4).getUint32(0) !== 0) {
      const length = view(yield 4).getUint32(0);
      throw new Error(new TextDecoder().decode(yield length) || t('Error'));
    }
    this.send(Uint8Array.of(1));
    const server = view(yield 24);
    const width = server.getUint16(0), height = server.getUint16(2), nameLength = server.getUint32(20);
    yield nameLength;
    this.setSize(width, height);
    this.send(pixelFormat());
    this.ready = true;
    this.sendEncodings();
    this.applyResolution(false);
    this.requestUpdate(false);
    this.onStatus(t('Connected'), false);
    for (;;) {
      const type = (yield 1)[0] ?? 0;
      if (type === 0) {
        const countRects = view(yield 3).getUint16(1);
        for (let index = 0; index < countRects; index++) {
          const header = view(yield 12);
          const x = header.getUint16(0), y = header.getUint16(2), w = header.getUint16(4), h = header.getUint16(6), encoding = header.getInt32(8);
          if (encoding === 0) this.blit(x, y, w, h, yield w * h * 4);
          else if (encoding === 5) yield* this.hextileRect(x, y, w, h);
          else if (encoding === 7) yield* this.tightRect(x, y, w, h);
          else if (encoding === -308) {
            const screens = view(yield 4).getUint8(0);
            if (screens) yield screens * 16;
            // QEMU replies x=1 with the reason in y: 4 - the request was passed to the guest,
            // 3 - the display adapter does not support resolution changes.
            if (x === 1) {
              this.pendingResolution = null;
              this.autoSent = null;
              if (y === 4) {
                this.resolutionError = '';
                this.resolutionNotice = t('The resize request was forwarded to the guest');
              } else {
                this.resolutionNotice = '';
                this.resolutionError = y === 3
                  ? t('The display adapter does not support changing the resolution')
                  : `${t('The server rejected the requested resolution')} (${y})`;
              }
              this.onQualityChange?.();
            } else {
              this.resolutionError = '';
              this.resolutionNotice = '';
            }
            this.setSize(w, h);
          } else if (encoding === -223) this.setSize(w, h);
          else throw new Error(`${t('Unsupported encoding')}: ${encoding}`);
        }
        this.finishUpdate();
      } else if (type === 1) {
        const header = view(yield 5);
        yield header.getUint16(3) * 6;
      } else if (type === 2) {
        // Bell
      } else if (type === 3) {
        yield 3;
        const length = view(yield 4).getUint32(0);
        this.receiveCutText(latin1Text(yield length));
      } else {
        throw new Error(`${t('Unknown server message')}: ${type}`);
      }
    }
  }

  private setSize(width: number, height: number): void {
    if (this.canvas.width !== width) this.canvas.width = width;
    if (this.canvas.height !== height) this.canvas.height = height;
    const current = `${width}×${height}`;
    if (current !== this.lastResolution) {
      this.lastResolution = current;
      if (this.pendingResolution?.width === width && this.pendingResolution.height === height) {
        this.pendingResolution = null;
        this.resolutionError = '';
        this.resolutionNotice = '';
      }
      this.onQualityChange?.();
    }
    this.layout();
  }

  private requestUpdate(incremental: boolean): void {
    const buffer = new Uint8Array(10), data = view(buffer);
    buffer[0] = 3; buffer[1] = incremental ? 1 : 0;
    data.setUint16(2, 0); data.setUint16(4, 0);
    data.setUint16(6, Math.max(1, this.canvas.width)); data.setUint16(8, Math.max(1, this.canvas.height));
    this.send(buffer);
  }

  private blit(x: number, y: number, width: number, height: number, pixels: Uint8Array): void {
    if (!width || !height) return;
    const image = this.ctx.createImageData(width, height);
    const out = image.data;
    for (let index = 0, count = width * height; index < count; index++) {
      const source = index * 4;
      out[source] = pixels[source + 2] ?? 0;
      out[source + 1] = pixels[source + 1] ?? 0;
      out[source + 2] = pixels[source] ?? 0;
      out[source + 3] = 255;
    }
    this.ctx.putImageData(image, x, y);
  }

  setMode(mode: 'fit' | 'actual' | 'zoom' | 'auto'): void {
    this.mode = mode;
    if (mode === 'actual') this.scale = 1;
    this.layout();
  }

  setZoom(value: number): void {
    this.mode = 'zoom';
    this.scale = Math.min(4, Math.max(0.1, value));
    this.layout();
  }

  stepZoom(direction: number): void {
    const steps = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 2, 3, 4];
    const target = direction > 0
      ? (steps.find((step) => step > this.scale + 0.001) ?? steps[steps.length - 1] ?? 1)
      : ([...steps].reverse().find((step) => step < this.scale - 0.001) ?? steps[0] ?? 1);
    this.setZoom(target);
  }

  layout(): void {
    const cw = this.canvas.width, ch = this.canvas.height;
    const vw = this.root.clientWidth, vh = this.root.clientHeight;
    if (!cw || !ch || !vw || !vh) return;
    if (this.mode === 'fit' || this.mode === 'auto') this.scale = Math.min(vw / cw, vh / ch);
    // The resolution is fitted only in Auto mode and only when the user has not chosen
    // a fixed size in the quality settings.
    if (this.mode === 'auto' && this.settings.resolution === 'auto') this.requestAutoSize(vw, vh);
    this.canvas.style.width = `${Math.round(cw * this.scale)}px`;
    this.canvas.style.height = `${Math.round(ch * this.scale)}px`;
  }

  private requestAutoSize(vw: number, vh: number): void {
    const w = Math.max(640, Math.min(1920, Math.floor(vw / 8) * 8));
    const h = Math.max(480, Math.min(1080, Math.floor(vh / 8) * 8));
    if (this.autoSent?.w === w && this.autoSent.h === h) return;
    window.clearTimeout(this.autoTimer);
    this.autoTimer = window.setTimeout(() => {
      this.autoSent = { w, h };
      this.sendDesktopSize(w, h);
    }, 400);
  }

  private sendDesktopSize(w: number, h: number): void {
    const buffer = new Uint8Array(24), data = view(buffer);
    buffer[0] = 251; data.setUint16(2, w); data.setUint16(4, h); buffer[6] = 1;
    data.setUint32(8, 0); data.setUint16(12, 0); data.setUint16(14, 0); data.setUint16(16, w); data.setUint16(18, h); data.setUint32(20, 0);
    this.send(buffer);
  }

  // Applies settings to an already connected console: SetEncodings and SetDesktopSize
  // do not break the RFB session. Before the handshake completes, values are remembered
  // and used in the first SetEncodings.
  applyQuality(next: VncQualitySettings, options: { fitAuto?: boolean } = {}): void {
    this.settings = { ...next };
    this.effective = effectiveQuality(this.settings);
    this.resolutionError = '';
    this.resolutionNotice = '';
    if (!this.ready) {
      this.onQualityChange?.();
      return;
    }
    this.sendEncodings();
    this.applyResolution(options.fitAuto === true);
    // A full frame is requested immediately, except when a JPEG is currently being drawn:
    // then the request is sent after rendering completes, so frames are not mixed up.
    if (this.paints > 0) this.pendingFullUpdate = true;
    else this.requestUpdate(false);
    this.onQualityChange?.();
  }

  private sendEncodings(): void {
    const encodings = rfbEncodings(this.effective);
    const buffer = new Uint8Array(4 + encodings.length * 4), data = view(buffer);
    buffer[0] = 2;
    data.setUint16(2, encodings.length);
    encodings.forEach((encoding, index) => data.setInt32(4 + index * 4, encoding));
    this.send(buffer);
  }

  private applyResolution(fitAuto: boolean): void {
    const size = resolutionSize(this.settings.resolution);
    if (!size) {
      this.autoSent = null;
      this.pendingResolution = null;
      // "Auto" fits the guest to the window only on an explicit Apply command,
      // so restoring settings does not change the resolution on connect.
      if (fitAuto) {
        const vw = this.root.clientWidth, vh = this.root.clientHeight;
        if (vw && vh) this.requestAutoSize(vw, vh);
      }
      return;
    }
    if (this.canvas.width === size.width && this.canvas.height === size.height) {
      this.pendingResolution = null;
      return;
    }
    this.pendingResolution = size;
    this.autoSent = { w: size.width, h: size.height };
    this.sendDesktopSize(size.width, size.height);
    this.onQualityChange?.();
  }

  // Requesting the next incremental update limits FPS. This is real
  // RFB frame rate control, not a simulated network limit.
  private scheduleUpdate(): void {
    window.clearTimeout(this.updateTimer);
    if (!this.ws) return;
    const fps = this.effective.fps;
    if (!fps) { this.requestUpdate(true); return; }
    this.updateTimer = window.setTimeout(() => this.requestUpdate(true), Math.max(16, Math.round(1000 / fps)));
  }

  private finishUpdate(): void {
    if (!this.imageJobs.length) {
      // While a JPEG is being decoded, no new request is sent: otherwise a fresher
      // frame could be overwritten by an asynchronously rendered older one.
      if (this.paints === 0) this.scheduleUpdate();
      return;
    }
    const jobs = this.imageJobs;
    this.imageJobs = [];
    this.paints += jobs.length;
    void Promise.all(jobs.map(async (job) => {
      const blob = new Blob([job.data as unknown as BlobPart], { type: 'image/jpeg' });
      const bitmap = await createImageBitmap(blob);
      this.ctx.drawImage(bitmap, job.x, job.y, job.width, job.height);
      bitmap.close();
    })).catch(() => {
      this.onStatus(t('Could not decode the image update'), false);
    }).finally(() => {
      this.paints -= jobs.length;
      if (this.paints > 0) return;
      // A deferred full frame (Apply during decoding) is more important than a regular step.
      if (this.pendingFullUpdate) {
        this.pendingFullUpdate = false;
        this.requestUpdate(false);
      } else {
        this.scheduleUpdate();
      }
    });
  }

  private *readCompact(): Generator<number, number, Uint8Array> {
    let byte = (yield 1)[0] ?? 0;
    let length = byte & 0x7f;
    if (byte & 0x80) {
      byte = (yield 1)[0] ?? 0;
      length |= (byte & 0x7f) << 7;
      if (byte & 0x80) length |= ((yield 1)[0] ?? 0) << 14;
    }
    return length;
  }

  private *tightData(stream: number, expected: number): Generator<number, Uint8Array, Uint8Array> {
    if (!expected) return new Uint8Array(0);
    if (expected < 12) return yield expected;
    const length = yield* this.readCompact();
    // Zero length per the Tight spec means uncompressed data.
    if (!length) return yield expected;
    const compressed = yield length;
    return this.inflaters[stream]!.inflate(compressed, expected);
  }

  private *tightRect(x: number, y: number, width: number, height: number): Generator<number, void, Uint8Array> {
    const control = (yield 1)[0] ?? 0;
    for (let stream = 0; stream < 4; stream++) {
      if (control & (1 << stream)) this.inflaters[stream]!.reset();
    }
    const kind = control >> 4;
    if (kind === 8) {
      this.fillRGB(x, y, width, height, yield 3);
      return;
    }
    if (kind === 9) {
      const length = yield* this.readCompact();
      this.imageJobs.push({ x, y, width, height, data: yield length });
      return;
    }
    if (kind === 10) throw new Error(`${t('Unsupported encoding')}: Tight PNG`);
    let filter = 0;
    if (kind & 4) filter = (yield 1)[0] ?? 0;
    const stream = kind & 3;
    if (filter === 1) {
      const colors = ((yield 1)[0] ?? 0) + 1;
      const palette = yield colors * 3;
      const bits = colors <= 2 ? 1 : 8;
      const pixels = yield* this.tightData(stream, Math.ceil(width * bits / 8) * height);
      this.blitPalette(x, y, width, height, pixels, palette, colors);
    } else if (filter === 2) {
      const pixels = yield* this.tightData(stream, width * height * 3);
      this.blitGradient(x, y, width, height, pixels);
    } else if (filter === 0) {
      this.blitRGB(x, y, width, height, yield* this.tightData(stream, width * height * 3));
    } else {
      throw new Error(`${t('Unsupported encoding')}: Tight filter ${filter}`);
    }
  }

  private *hextileRect(x: number, y: number, width: number, height: number): Generator<number, void, Uint8Array> {
    let background: Uint8Array = new Uint8Array(4);
    let foreground: Uint8Array = new Uint8Array(4);
    for (let tileY = y; tileY < y + height; tileY += 16) {
      for (let tileX = x; tileX < x + width; tileX += 16) {
        const tileWidth = Math.min(16, x + width - tileX);
        const tileHeight = Math.min(16, y + height - tileY);
        const sub = (yield 1)[0] ?? 0;
        if (sub & 1) {
          // Raw tile: all pixels are transmitted in the client format.
          this.putPixelTile(tileX, tileY, tileWidth, tileHeight, yield tileWidth * tileHeight * 4);
          continue;
        }
        if (sub & 2) background = yield 4;
        if (sub & 4) foreground = yield 4;
        this.fillTile(tileX, tileY, tileWidth, tileHeight, background);
        if (!(sub & 8)) continue;
        const count = (yield 1)[0] ?? 0;
        for (let index = 0; index < count; index++) {
          const pixel = sub & 16 ? yield 4 : foreground;
          const position = (yield 1)[0] ?? 0;
          const size = (yield 1)[0] ?? 0;
          const sx = (position >> 4) & 0x0f, sy = position & 0x0f;
          const sw = ((size >> 4) & 0x0f) + 1, sh = (size & 0x0f) + 1;
          this.fillTile(tileX + sx, tileY + sy, Math.min(sw, 16 - sx), Math.min(sh, 16 - sy), pixel);
        }
      }
    }
  }

  private rgb(pixel: Uint8Array): [number, number, number] {
    return [pixel[2] ?? 0, pixel[1] ?? 0, pixel[0] ?? 0];
  }

  private fillTile(x: number, y: number, width: number, height: number, pixel: Uint8Array): void {
    if (width <= 0 || height <= 0) return;
    const [r, g, b] = this.rgb(pixel);
    this.ctx.fillStyle = `rgb(${r},${g},${b})`;
    this.ctx.fillRect(x, y, width, height);
  }

  private fillRGB(x: number, y: number, width: number, height: number, pixel: Uint8Array): void {
    if (!width || !height) return;
    this.ctx.fillStyle = `rgb(${pixel[0] ?? 0},${pixel[1] ?? 0},${pixel[2] ?? 0})`;
    this.ctx.fillRect(x, y, width, height);
  }

  private putPixelTile(x: number, y: number, width: number, height: number, pixels: Uint8Array): void {
    if (!width || !height) return;
    const image = this.ctx.createImageData(width, height);
    const out = image.data;
    for (let index = 0, count = width * height; index < count; index++) {
      const source = index * 4, target = index * 4;
      out[target] = pixels[source + 2] ?? 0;
      out[target + 1] = pixels[source + 1] ?? 0;
      out[target + 2] = pixels[source] ?? 0;
      out[target + 3] = 255;
    }
    this.ctx.putImageData(image, x, y);
  }

  private blitRGB(x: number, y: number, width: number, height: number, pixels: Uint8Array): void {
    if (!width || !height) return;
    const image = this.ctx.createImageData(width, height);
    const out = image.data;
    for (let index = 0, count = width * height; index < count; index++) {
      const source = index * 3, target = index * 4;
      out[target] = pixels[source] ?? 0;
      out[target + 1] = pixels[source + 1] ?? 0;
      out[target + 2] = pixels[source + 2] ?? 0;
      out[target + 3] = 255;
    }
    this.ctx.putImageData(image, x, y);
  }

  private blitPalette(x: number, y: number, width: number, height: number, pixels: Uint8Array, palette: Uint8Array, colors: number): void {
    if (!width || !height) return;
    const image = this.ctx.createImageData(width, height);
    const out = image.data;
    if (colors <= 2) {
      const rowSize = Math.ceil(width / 8);
      for (let row = 0; row < height; row++) {
        for (let column = 0; column < width; column++) {
          const byte = pixels[row * rowSize + (column >> 3)] ?? 0;
          const color = (byte >> (7 - (column & 7))) & 1;
          const source = color * 3, target = (row * width + column) * 4;
          out[target] = palette[source] ?? 0;
          out[target + 1] = palette[source + 1] ?? 0;
          out[target + 2] = palette[source + 2] ?? 0;
          out[target + 3] = 255;
        }
      }
    } else {
      for (let index = 0, count = width * height; index < count; index++) {
        const source = (pixels[index] ?? 0) * 3, target = index * 4;
        out[target] = palette[source] ?? 0;
        out[target + 1] = palette[source + 1] ?? 0;
        out[target + 2] = palette[source + 2] ?? 0;
        out[target + 3] = 255;
      }
    }
    this.ctx.putImageData(image, x, y);
  }

  // The Tight gradient filter predicts a pixel from the left and top neighbors.
  private blitGradient(x: number, y: number, width: number, height: number, data: Uint8Array): void {
    if (!width || !height) return;
    const image = this.ctx.createImageData(width, height);
    const out = image.data;
    let source = 0, target = 0;
    const left = [0, 0, 0];
    for (let column = 0; column < width; column++) {
      for (let channel = 0; channel < 3; channel++) {
        left[channel] = ((left[channel] ?? 0) + (data[source++] ?? 0)) & 0xff;
        out[target++] = left[channel] ?? 0;
      }
      out[target++] = 255;
    }
    let upperIndex = 0;
    const upperLeft: number[] = [0, 0, 0];
    for (let row = 1; row < height; row++) {
      left.fill(0);
      upperLeft.fill(0);
      for (let column = 0; column < width; column++) {
        for (let channel = 0; channel < 3; channel++) {
          const above = out[upperIndex++] ?? 0;
          let prediction = (left[channel] ?? 0) + above - (upperLeft[channel] ?? 0);
          prediction = Math.max(0, Math.min(255, prediction));
          const value = ((data[source++] ?? 0) + prediction) & 0xff;
          out[target++] = value;
          upperLeft[channel] = above;
          left[channel] = value;
        }
        upperIndex++; // alpha of the top row
        out[target++] = 255;
      }
    }
    this.ctx.putImageData(image, x, y);
  }

  destroy(): void {
    window.clearTimeout(this.autoTimer);
    window.clearTimeout(this.updateTimer);
    this.imageJobs = [];
    this.releaseAll();
    this.setCaptured(false);
    this.ro.disconnect();
    window.removeEventListener('blur', this.onWindowBlur);
    window.removeEventListener('keydown', this.onHotkey, true);
    window.removeEventListener('keyup', this.onHotkey, true);
    this.closeKbd(); this.closeClip();
    document.removeEventListener('fullscreenchange', this.onFullscreen);
    this.ws = null;
    this.root.remove();
  }

  toggleKbd(): void { if (this.kbd) this.closeKbd(); else this.openKbd(); }

  openKbd(): void {
    if (this.kbd) return;
    this.kbdHeld.clear(); this.kbdButtons.clear();
    const modButtons = VKBD_MODS.map(([name, code]) => {
      const button = document.createElement('button');
      button.type = 'button'; button.className = KBD_KEY; button.textContent = name;
      button.onclick = () => this.toggleMod(name, code);
      this.kbdButtons.set(name, button);
      return button;
    });
    const keyButton = (label: string, code: number): HTMLButtonElement => {
      const button = document.createElement('button');
      button.type = 'button'; button.className = KBD_KEY; button.textContent = label;
      button.onclick = () => this.pressKey(code);
      return button;
    };
    const head = document.createElement('div');
    head.className = KBD_HEAD;
    const title = document.createElement('b'); title.textContent = t('Virtual machine keyboard');
    const hint = document.createElement('span'); hint.className = 'text-muted'; hint.textContent = 'Ctrl+Alt+K';
    const close = document.createElement('button'); close.type = 'button'; close.className = KBD_CLOSE; close.title = t('Close'); close.textContent = '✕'; close.onclick = () => this.closeKbd();
    head.append(title, hint, close);
    const row = (children: HTMLElement[]): HTMLDivElement => { const div = document.createElement('div'); div.className = KBD_ROW; div.append(...children); return div; };
    this.kbd = document.createElement('div');
    this.kbd.className = KBD_ROOT;
    this.kbd.dataset['vkbd'] = '';
    this.kbd.onmousedown = (event) => event.preventDefault();
    this.kbd.append(head, row(modButtons), row(VKBD_FKEYS.map(([label, code]) => keyButton(label, code))), row(VKBD_KEYS.map(([label, code]) => keyButton(label, code))), row(VKBD_COMBOS.map(([label, codes]) => {
      const button = document.createElement('button'); button.type = 'button'; button.className = `${KBD_KEY} ${KBD_COMBO}`; button.textContent = label; button.onclick = () => this.sendCombo(codes); return button;
    })));
    this.root.append(this.kbd);
    this.onKbdChange?.();
  }

  closeKbd(): void {
    if (!this.kbd) return;
    for (const code of this.kbdHeld.values()) this.sendKey(code, 0);
    this.kbdHeld.clear();
    this.kbd.remove(); this.kbd = null;
    this.onKbdChange?.();
  }

  private toggleMod(name: string, code: number): void {
    if (this.kbdHeld.has(name)) { this.kbdHeld.delete(name); this.sendKey(code, 0); }
    else { this.kbdHeld.set(name, code); this.sendKey(code, 1); }
    const held = this.kbdButtons.get(name);
    if (held) held.className = this.kbdHeld.has(name) ? `${KBD_KEY} ${KBD_ON}` : KBD_KEY;
  }

  private pressKey(code: number): void {
    const modifiers = [...this.kbdHeld.values()];
    this.sendCombo([...modifiers, code]);
    this.kbdHeld.clear();
    for (const button of this.kbdButtons.values()) button.className = KBD_KEY;
  }

  receiveCutText(text: string): void {
    if (!this.allowClipboard) return;
    this.lastCut = text;
    if (this.clipTa) { this.clipTa.value = text; if (this.clipStatus) this.clipStatus.textContent = t('Text received from VM'); }
    void copyToClipboard(text).catch(() => undefined);
  }

  // Sends text to the guest clipboard (RFB ClientCutText, ISO 8859-1). Returns false
  // and sends nothing if the text has characters that Latin-1 cannot carry.
  sendCutText(text: string): boolean {
    const { bytes, unsupported } = latin1Payload(text);
    if (unsupported > 0) return false;
    const buffer = new Uint8Array(8 + bytes.length), viewData = view(buffer);
    buffer[0] = 6; viewData.setUint32(4, bytes.length); buffer.set(bytes, 8); this.send(buffer);
    return true;
  }

  // Types text into the guest as key presses. Shifted symbols are sent with an
  // explicit Shift press (see keyboardText.ts). Characters the guest cannot receive
  // are counted and reported instead of being dropped without a trace.
  // Reads the browser clipboard and types it into the active VM. Opens the clipboard
  // panel so the text and the typing progress are visible. If the browser refuses
  // to read the clipboard, the panel lets the user paste manually.
  async pasteFromBrowser(): Promise<void> {
    if (!this.allowClipboard) return;
    this.openClip();
    let text = '';
    try {
      text = await navigator.clipboard.readText();
    } catch {
      if (this.clipStatus) this.clipStatus.textContent = t('Browser did not allow reading the clipboard. Paste the text here and press Into VM.');
      return;
    }
    if (!text) { if (this.clipStatus) this.clipStatus.textContent = t('Clipboard is empty'); return; }
    if (this.clipTa) this.clipTa.value = text;
    this.typeText(text, (status) => { if (this.clipStatus) this.clipStatus.textContent = status; });
  }

  typeText(text: string, onStatus?: (text: string) => void): void {
    this.stopTyping();
    const { steps, skipped } = typingPlan(text);
    const job = { stop: false, timer: 0 };
    this.typing = job;
    let index = 0;
    const finish = () => {
      this.typing = null;
      const characters = steps.filter((step) => step.down && step.keysym !== KEYSYM_SHIFT_L).length;
      const typed = `${t('Characters typed:')} ${characters}`;
      onStatus?.(skipped ? `${typed} · ${t('Not typed (unsupported on US layout):')} ${skipped}` : typed);
    };
    const step = () => {
      if (job.stop) return;
      if (index >= steps.length) { finish(); return; }
      const current = steps[index++];
      if (current) this.sendKey(current.keysym, current.down ? 1 : 0);
      job.timer = window.setTimeout(step, 8);
    };
    onStatus?.(t('Typing…'));
    step();
  }

  stopTyping(): void {
    if (!this.typing) return;
    this.typing.stop = true; window.clearTimeout(this.typing.timer); this.typing = null;
  }

  toggleClip(): void { if (!this.allowClipboard) return; if (this.clip) this.closeClip(); else this.openClip(); }

  openClip(): void {
    if (!this.allowClipboard || this.clip) return;
    const ta = document.createElement('textarea');
    ta.className = CLIP_TEXT; ta.spellcheck = false; ta.placeholder = t('Text to paste into VM (multiple lines allowed)'); ta.value = this.lastCut;
    const status = document.createElement('div'); status.className = CLIP_STATUS; status.textContent = this.lastCut ? t('Text received from VM') : '';
    const button = (label: string, title: string, action: () => void, cls = ''): HTMLButtonElement => {
      const element = document.createElement('button'); element.type = 'button'; element.title = title; element.className = cls; element.textContent = label; element.onclick = action; return element;
    };
    const head = document.createElement('div'); head.className = KBD_HEAD;
    const title = document.createElement('b'); title.textContent = t('Clipboard');
    const close = document.createElement('button'); close.type = 'button'; close.className = KBD_CLOSE; close.title = t('Close'); close.textContent = '✕'; close.onclick = () => this.closeClip();
    head.append(title, close);
    const row = document.createElement('div'); row.className = KBD_ROW;
    row.append(
      button(t('Into VM'), t('Into VM'), () => this.typeText(ta.value, (text) => { status.textContent = text; }), 'font-semibold'),
      button(t('Stop'), t('Stop'), () => { this.stopTyping(); status.textContent = t('Typing stopped'); }),
      button(t('Into VNC clipboard'), t('Into VNC clipboard'), () => { status.textContent = this.sendCutText(ta.value) ? t('Sent to VNC clipboard') : t('Only Latin-1 characters can be sent to the VM clipboard. Use Into VM for ASCII text.'); }),
      button(t('From VM'), t('From VM'), () => { if (this.lastCut) ta.value = this.lastCut; else status.textContent = t('No text received from VM'); }),
      button(t('Copy'), t('Copy'), () => { void copyToClipboard(ta.value).then((ok) => { status.textContent = ok ? t('Copied to browser clipboard') : t('Browser did not allow copying'); }); }),
    );
    this.clip = document.createElement('div'); this.clip.className = CLIP_ROOT;
    this.clip.dataset['vclip'] = '';
    this.clip.append(head, ta, row, status);
    this.clipTa = ta; this.clipStatus = status;
    this.root.append(this.clip);
    ta.focus({ preventScroll: true });
    this.onKbdChange?.();
  }

  closeClip(): void {
    if (!this.clip) return;
    this.stopTyping(); this.clip.remove(); this.clip = null; this.clipTa = null; this.clipStatus = null;
    this.onKbdChange?.();
  }

  private bindInput(): void {
    const canvas = this.canvas;
    canvas.addEventListener('keydown', (event) => this.keyDown(event));
    canvas.addEventListener('keyup', (event) => this.keyUp(event));
    canvas.addEventListener('focus', () => this.setCaptured(true));
    canvas.addEventListener('blur', () => this.setCaptured(false));
    canvas.addEventListener('mouseenter', () => { if (this.captured === false) canvas.focus({ preventScroll: true }); });
    window.addEventListener('blur', this.onWindowBlur);
    window.addEventListener('keydown', this.onHotkey, true);
    window.addEventListener('keyup', this.onHotkey, true);
    document.addEventListener('fullscreenchange', this.onFullscreen);
    canvas.addEventListener('mousedown', (event) => { canvas.focus(); this.button(event, true); event.preventDefault(); });
    canvas.addEventListener('mouseup', (event) => this.button(event, false));
    canvas.addEventListener('mousemove', (event) => this.pointer(event, 0));
    canvas.addEventListener('wheel', (event) => { event.preventDefault(); const bit = event.deltaY < 0 ? 8 : 16; this.pointer(event, bit); this.pointer(event, 0); }, { passive: false });
    canvas.addEventListener('contextmenu', (event) => event.preventDefault());
  }

  private keyDown(event: KeyboardEvent): void {
    event.stopPropagation();
    if (!this.captured) return;
    // Ctrl+Shift+V is the terminal paste shortcut. It is handled here and not sent to the
    // guest. Plain Ctrl+V is left to the guest, because guest applications use it.
    if (event.ctrlKey && event.shiftKey && !event.altKey && !event.metaKey && event.code === 'KeyV') {
      event.preventDefault();
      this.swallowed.add(keyId(event));
      if (!event.repeat) void this.pasteFromBrowser();
      return;
    }
    const code = keysym(event);
    if (code === null) return;
    event.preventDefault();
    this.pressed.set(keyId(event), code);
    this.sendKey(code, 1);
  }

  private keyUp(event: KeyboardEvent): void {
    event.stopPropagation();
    const id = keyId(event);
    if (this.swallowed.delete(id)) { event.preventDefault(); return; }
    const code = this.pressed.get(id) ?? keysym(event);
    if (code === null) return;
    event.preventDefault();
    this.pressed.delete(id);
    this.sendKey(code, 0);
  }

  sendKey(code: number, down: boolean | number): void {
    const buffer = new Uint8Array(8), data = view(buffer);
    buffer[0] = 4; buffer[1] = down ? 1 : 0; data.setUint32(4, code); this.send(buffer);
  }

  private releaseAll(): void {
    for (const code of this.pressed.values()) this.sendKey(code, 0);
    this.pressed.clear();
  }

  sendCombo(codes: number[]): void {
    for (const code of codes) this.sendKey(code, 1);
    for (const code of [...codes].reverse()) this.sendKey(code, 0);
  }

  setCaptured(on: boolean): void {
    if (this.captured === on) return;
    this.captured = on;
    if (!on) this.releaseAll();
    lockKeyboard(on && Boolean(document.fullscreenElement));
    this.onCapture?.();
  }

  private button(event: MouseEvent, down: boolean): void {
    const bit = [1, 2, 4][event.button] ?? 0;
    if (!bit) return;
    if (down) this.buttons |= bit; else this.buttons &= ~bit;
    this.pointer(event, 0);
  }

  private pointer(event: MouseEvent, extra: number): void {
    const rect = this.canvas.getBoundingClientRect();
    const x = Math.min(this.canvas.width - 1, Math.max(0, Math.round((event.clientX - rect.left) * this.canvas.width / rect.width)));
    const y = Math.min(this.canvas.height - 1, Math.max(0, Math.round((event.clientY - rect.top) * this.canvas.height / rect.height)));
    const buffer = new Uint8Array(6), data = view(buffer);
    buffer[0] = 5; buffer[1] = (this.buttons | extra) & 0xff; data.setUint16(2, x); data.setUint16(4, y); this.send(buffer);
  }
}
