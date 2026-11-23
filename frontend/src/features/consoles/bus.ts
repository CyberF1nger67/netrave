// Routing of "open this machine's console" between browser windows and tabs of the same origin.
//
// There must be one terminal instance at a time. When a machine is opened, a live terminal in another
// window is asked first: an application tab with the console page is preferred, then a floating window.
// The request is sent to that terminal, which opens the machine there. Only when no live terminal answers
// does the caller create its own one. The channel is injected, so the logic is testable without a browser.
//
// Every window with a terminal says which mode it is in ('page' or 'float') when the mode changes, on every
// heartbeat, and says goodbye when it closes. A window that stops heartbeating for PAGE_TIMEOUT_MS is gone.
// The window's id is also its owner id in the console registry, so the consoles of a gone window can be freed.

type DockMode = 'page' | 'float';

/** How often a window repeats its announcement. */
export const PAGE_HEARTBEAT_MS = 20_000;
/** A window not heard from for this long is considered gone. Background tabs may throttle timers to about once a minute. */
export const PAGE_TIMEOUT_MS = 150_000;

export interface BusChannel {
  postMessage(data: unknown): void;
  onmessage: ((event: { data: unknown }) => void) | null;
  close(): void;
}

type Message<T> =
  | { t: 'probe'; from: string }
  | { t: 'alive'; from: string; to: string; mode: DockMode }
  | { t: 'open'; from: string; to: string; request: T }
  | { t: 'hello'; from: string; mode: DockMode }
  | { t: 'bye'; from: string }
  | { t: 'query'; from: string }
  | { t: 'raise'; from: string; to: string };

interface DockPeer {
  id: string;
  mode: DockMode;
}

interface WindowRecord {
  mode: DockMode;
  seen: number;
}

export class DockBus<T = unknown> {
  private replies: DockPeer[] | null = null;
  // null while this window has no terminal: it must not answer probes or receive open requests.
  private mode: DockMode | null;
  // Other windows with a terminal: window id -> last announcement.
  private readonly windows = new Map<string, WindowRecord>();
  private pageListener: ((open: boolean) => void) | null = null;
  private goneListener: ((owner: string) => void) | null = null;
  private raiseListener: (() => void) | null = null;

  constructor(
    private readonly channel: BusChannel,
    readonly id: string,
    mode: DockMode | null,
    private readonly onOpen: (request: T) => void,
    private readonly probeMs = 150,
    private readonly wait: (ms: number) => Promise<void> = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    private readonly now: () => number = () => Date.now(),
  ) {
    this.mode = mode;
    this.channel.onmessage = (event) => this.handle(event.data as Message<T>);
  }

  /** Declares this window's terminal mode (or none, which says goodbye). Other windows hear about changes at once. */
  setMode(mode: DockMode | null): void {
    if (mode === this.mode) return;
    if (mode === null) this.bye();
    this.mode = mode;
    this.heartbeat();
  }

  /** Repeats this window's mode so that others keep it alive. Does nothing without a terminal. */
  heartbeat(): void {
    if (this.mode !== null) this.channel.postMessage({ t: 'hello', from: this.id, mode: this.mode } satisfies Message<T>);
  }

  /** Tells other windows that this one is closing. */
  bye(): void {
    if (this.mode !== null) this.channel.postMessage({ t: 'bye', from: this.id } satisfies Message<T>);
  }

  /** Finds a live terminal in another window: a page first, then a floating window. */
  async findPeer(): Promise<DockPeer | null> {
    this.replies = [];
    this.channel.postMessage({ t: 'probe', from: this.id } satisfies Message<T>);
    await this.wait(this.probeMs);
    const found = this.replies;
    this.replies = null;
    return found?.find((peer) => peer.mode === 'page') ?? found?.[0] ?? null;
  }

  /** Sends an open request to a peer terminal. */
  send(peer: DockPeer, request: T): void {
    this.channel.postMessage({ t: 'open', from: this.id, to: peer.id, request } satisfies Message<T>);
  }

  /** Asks a peer terminal to show its active console (the sidebar "Console" button without a machine). */
  raise(peer: DockPeer): void {
    this.channel.postMessage({ t: 'raise', from: this.id, to: peer.id } satisfies Message<T>);
  }

  /** Called when another window asks this window's terminal to show itself. */
  onRaise(listener: () => void): void {
    this.raiseListener = listener;
  }

  /** Asks other windows to announce their modes, then waits for the answers. */
  async query(): Promise<void> {
    this.channel.postMessage({ t: 'query', from: this.id } satisfies Message<T>);
    await this.wait(this.probeMs);
  }

  /** Forgets windows that have stopped heartbeating. */
  sweep(): void {
    const cutoff = this.now() - PAGE_TIMEOUT_MS;
    for (const [id, record] of [...this.windows]) if (record.seen < cutoff) this.forget(id);
  }

  /** Called with true when the first application tab appears in another window and false when the last one is gone. */
  onPageChange(listener: (open: boolean) => void): void {
    this.pageListener = listener;
  }

  /** Called with the id of a window that said goodbye or timed out, before onPageChange. */
  onWindowGone(listener: (owner: string) => void): void {
    this.goneListener = listener;
  }

  private pageOpen(): boolean {
    for (const record of this.windows.values()) if (record.mode === 'page') return true;
    return false;
  }

  private notify(before: boolean): void {
    const after = this.pageOpen();
    if (before !== after) this.pageListener?.(after);
  }

  private remember(id: string, mode: DockMode): void {
    const before = this.pageOpen();
    this.windows.set(id, { mode, seen: this.now() });
    this.notify(before);
  }

  private forget(id: string): void {
    const before = this.pageOpen();
    if (!this.windows.delete(id)) return;
    this.goneListener?.(id);
    this.notify(before);
  }

  private handle(message: Message<T>): void {
    if (!message || typeof message !== 'object') return;
    if (message.t === 'probe' && message.from !== this.id) {
      if (this.mode === null) return;
      this.channel.postMessage({ t: 'alive', from: this.id, to: message.from, mode: this.mode } satisfies Message<T>);
      return;
    }
    if (message.t === 'alive' && message.to === this.id && this.replies) {
      if (!this.replies.some((peer) => peer.id === message.from)) this.replies.push({ id: message.from, mode: message.mode });
      return;
    }
    if (message.t === 'open' && message.to === this.id && this.mode !== null) this.onOpen(message.request);
    if (message.t === 'raise' && message.to === this.id && this.mode !== null) this.raiseListener?.();
    if (message.t === 'hello' && message.from !== this.id) this.remember(message.from, message.mode);
    if (message.t === 'bye' && message.from !== this.id) this.forget(message.from);
    if (message.t === 'query' && message.from !== this.id) this.heartbeat();
  }
}

/** Creates the bus on the browser channel, or returns null where BroadcastChannel is unavailable. */
export function createBrowserChannel(name: string): BusChannel | null {
  if (typeof BroadcastChannel === 'undefined') return null;
  return new BroadcastChannel(name) as unknown as BusChannel;
}
