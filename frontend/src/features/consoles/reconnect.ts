// Silent WebSocket recovery for device consoles. A console must survive network
// outages, backend restarts and QEMU restarts without changing what the user sees.
// Every live socket owns at most one retry timer, one heartbeat and one connect
// timeout; close/dispose detaches handlers before closing the socket so a late
// event can never schedule a second connection.

export const CONTROL_PING = 'netrave:ping';
export const CONTROL_PONG = 'netrave:pong';

const HEARTBEAT_MS = 10_000;
const HEARTBEAT_TIMEOUT_MS = 30_000;
const CONNECT_TIMEOUT_MS = 12_000;
const STABLE_MS = 5_000;
const BACKOFF_BASE_MS = 500;
const BACKOFF_MAX_MS = 15_000;

interface ReconnectHandlers {
  /** Called for every attempt. The URL may change (for example to carry a replay offset). */
  url: () => string;
  /** Called immediately after the socket is created, before it opens. */
  attach?: (ws: WebSocket) => void;
  /** Called when the socket reaches OPEN. */
  open?: (ws: WebSocket) => void;
  /** Called for every message from the current socket. */
  message?: (ws: WebSocket, event: MessageEvent) => void;
  /** Called when the current socket is gone, before the retry is scheduled. */
  down?: (ws: WebSocket) => void;
  /**
   * Reconnect immediately after the first drop instead of waiting for the first
   * backoff step. Repeated failures still back off to protect the server.
   */
  immediateRetry?: boolean;
  /** Optional backoff override, for consoles that must come back very quickly. */
  retry?: { baseMs?: number; maxMs?: number };
}

export class ReconnectSocket {
  private readonly handlers: ReconnectHandlers;
  private ws: WebSocket | null = null;
  private retryTimer = 0;
  private connectTimer = 0;
  private heartbeatTimer = 0;
  private stableTimer = 0;
  private attempts = 0;
  private openedAt = 0;
  private lastSeen = 0;
  private running = false;

  private readonly wake = () => {
    if (!this.running || this.ws) return;
    if (document.visibilityState === 'hidden' && navigator.onLine) return;
    this.attempts = 0;
    this.clearRetry();
    this.connect();
  };

  private readonly offline = () => {
    if (!this.running) return;
    if (this.ws) this.drop(false);
    this.attempts = 0;
    this.schedule();
  };

  constructor(handlers: ReconnectHandlers) {
    this.handlers = handlers;
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    window.addEventListener('online', this.wake);
    window.addEventListener('offline', this.offline);
    document.addEventListener('visibilitychange', this.wake);
    this.connect();
  }

  stop(): void {
    if (!this.running) return;
    this.running = false;
    window.removeEventListener('online', this.wake);
    window.removeEventListener('offline', this.offline);
    document.removeEventListener('visibilitychange', this.wake);
    this.drop(false);
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): boolean {
    const ws = this.ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    try {
      ws.send(data as string | Blob | BufferSource);
      return true;
    } catch {
      return false;
    }
  }

  private connect(): void {
    if (!this.running || this.ws) return;
    this.clearRetry();
    let ws: WebSocket;
    try {
      ws = new WebSocket(this.handlers.url());
    } catch {
      this.schedule();
      return;
    }
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    this.handlers.attach?.(ws);

    ws.onopen = () => {
      if (this.ws !== ws) return;
      window.clearTimeout(this.connectTimer);
      this.connectTimer = 0;
      this.openedAt = Date.now();
      this.lastSeen = this.openedAt;
      this.startHeartbeat(ws);
      this.stableTimer = window.setTimeout(() => {
        if (this.ws === ws) this.attempts = 0;
      }, STABLE_MS);
      this.handlers.open?.(ws);
    };
    ws.onmessage = (event) => {
      if (this.ws !== ws) return;
      this.lastSeen = Date.now();
      this.handlers.message?.(ws, event);
    };
    ws.onerror = () => {
      // The browser follows an error with close. Recovery is handled there.
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.finish(ws, true);
    };
    this.connectTimer = window.setTimeout(() => {
      if (this.ws !== ws) return;
      this.finish(ws, true);
    }, CONNECT_TIMEOUT_MS);
  }

  private startHeartbeat(ws: WebSocket): void {
    window.clearInterval(this.heartbeatTimer);
    this.heartbeatTimer = window.setInterval(() => {
      if (this.ws !== ws || ws.readyState !== WebSocket.OPEN) return;
      if (Date.now() - this.lastSeen > HEARTBEAT_TIMEOUT_MS) {
        this.finish(ws, true);
        return;
      }
      try {
        ws.send(CONTROL_PING);
      } catch {
        this.finish(ws, true);
      }
    }, HEARTBEAT_MS);
  }

  private finish(ws: WebSocket, retry: boolean): void {
    if (this.ws !== ws) return;
    const stable = this.openedAt > 0 && Date.now() - this.openedAt >= STABLE_MS;
    this.ws = null;
    if (stable) this.attempts = 0;
    this.openedAt = 0;
    this.clearTimers();
    this.detach(ws);
    this.handlers.down?.(ws);
    if (this.running && retry) this.schedule();
  }

  private drop(retry: boolean): void {
    const ws = this.ws;
    if (!ws) {
      this.clearRetry();
      return;
    }
    this.ws = null;
    this.openedAt = 0;
    this.clearTimers();
    this.detach(ws);
    this.handlers.down?.(ws);
    if (this.running && retry) this.schedule();
  }

  private detach(ws: WebSocket): void {
    ws.onopen = null;
    ws.onmessage = null;
    ws.onerror = null;
    ws.onclose = null;
    try { ws.close(); } catch { /* already closed */ }
  }

  private schedule(): void {
    if (!this.running || this.ws || this.retryTimer) return;
    this.attempts++;
    // The first reconnect attempt can be immediate (used by VNC so the picture
    // comes back without a noticeable pause). Later attempts still back off.
    const immediate = this.handlers.immediateRetry && this.attempts === 1;
    const baseMs = this.handlers.retry?.baseMs ?? BACKOFF_BASE_MS;
    const maxMs = this.handlers.retry?.maxMs ?? BACKOFF_MAX_MS;
    const base = Math.min(maxMs, baseMs * 2 ** Math.min(this.attempts - 1, 10));
    const delay = immediate ? 0 : Math.round(base + base * 0.2 * Math.random());
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = 0;
      this.connect();
    }, delay);
  }

  private clearRetry(): void {
    if (this.retryTimer) window.clearTimeout(this.retryTimer);
    this.retryTimer = 0;
  }

  private clearTimers(): void {
    this.clearRetry();
    if (this.connectTimer) window.clearTimeout(this.connectTimer);
    if (this.heartbeatTimer) window.clearInterval(this.heartbeatTimer);
    if (this.stableTimer) window.clearTimeout(this.stableTimer);
    this.connectTimer = 0;
    this.heartbeatTimer = 0;
    this.stableTimer = 0;
  }
}
