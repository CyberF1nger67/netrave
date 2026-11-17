import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CONTROL_PING, ReconnectSocket } from './reconnect';

type Handler = ((event: Event) => void) | null;

class FakeSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeSocket[] = [];

  readyState = FakeSocket.CONNECTING;
  binaryType = 'blob';
  sent: Array<string | ArrayBufferLike | Blob | ArrayBufferView> = [];
  onopen: Handler = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: Handler = null;
  onclose: Handler = null;

  constructor(readonly url: string) { FakeSocket.instances.push(this); }

  open(): void {
    this.readyState = FakeSocket.OPEN;
    this.onopen?.(new Event('open'));
  }

  message(data: unknown): void { this.onmessage?.({ data } as MessageEvent); }

  drop(): void {
    this.readyState = FakeSocket.CLOSED;
    this.onclose?.(new Event('close'));
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    if (this.readyState !== FakeSocket.OPEN) throw new Error('not open');
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeSocket.CLOSED;
    this.onclose?.(new Event('close'));
  }
}

const make = (events: string[] = []) => new ReconnectSocket({
  url: () => `ws://test/${FakeSocket.instances.length}`,
  message: (_ws, event) => { events.push(String(event.data)); },
});

describe('ReconnectSocket', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    FakeSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('reconnects after a close without leaving a second timer behind', async () => {
    const socket = make();
    socket.start();
    expect(FakeSocket.instances).toHaveLength(1);
    FakeSocket.instances[0]?.open();
    FakeSocket.instances[0]?.drop();

    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);

    // A late event from the old socket must not spawn another connection.
    FakeSocket.instances[0]?.drop();
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);
  });

  it('uses exponential backoff for repeated failed attempts', async () => {
    const socket = make();
    socket.start();
    FakeSocket.instances[0]?.drop(); // initial attempt failed
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);
    FakeSocket.instances[1]?.drop();
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2); // next delay is 1000ms
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(3);
  });

  it('can reconnect immediately after the first drop for VNC', async () => {
    const socket = new ReconnectSocket({
      url: () => `ws://test/${FakeSocket.instances.length}`,
      immediateRetry: true,
    });
    socket.start();
    FakeSocket.instances[0]?.drop();
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeSocket.instances).toHaveLength(2);
  });

  it('keeps the reconnect interval short with a fast retry override', async () => {
    const socket = new ReconnectSocket({
      url: () => `ws://test/${FakeSocket.instances.length}`,
      immediateRetry: true,
      retry: { baseMs: 50, maxMs: 150 },
    });
    socket.start();
    FakeSocket.instances[0]?.drop();
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeSocket.instances).toHaveLength(2);

    for (let attempt = 0; attempt < 4; attempt++) {
      FakeSocket.instances[FakeSocket.instances.length - 1]?.drop();
      await vi.advanceTimersByTimeAsync(150);
    }
    // Backoff never exceeds 150 ms: all four attempts happened.
    expect(FakeSocket.instances).toHaveLength(6);
  });

  it('sends application heartbeats and replaces a hung connection', async () => {
    const socket = make();
    socket.start();
    FakeSocket.instances[0]?.open();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(FakeSocket.instances[0]?.sent).toContain(CONTROL_PING);

    // No pong or other message for 40 seconds: the old transport is closed and retried.
    await vi.advanceTimersByTimeAsync(30_000);
    expect(FakeSocket.instances[0]?.readyState).toBe(FakeSocket.CLOSED);
    await vi.advanceTimersByTimeAsync(500);
    expect(FakeSocket.instances).toHaveLength(2);
  });

  it('stop clears every timer and handler', async () => {
    const socket = make();
    socket.start();
    FakeSocket.instances[0]?.open();
    socket.stop();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.instances).toHaveLength(1);
    expect(FakeSocket.instances[0]?.onclose).toBeNull();
  });
});
