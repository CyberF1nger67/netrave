import { describe, expect, it } from 'vitest';
import { DockBus, PAGE_TIMEOUT_MS, type BusChannel } from './bus';

// Channels of one origin deliver each message to the other channels, not to the sender.
function network() {
  const channels: Array<BusChannel & { deliver(data: unknown): void }> = [];
  const make = (): BusChannel & { deliver(data: unknown): void } => {
    const channel = {
      onmessage: null as ((event: { data: unknown }) => void) | null,
      postMessage(data: unknown) {
        for (const other of channels) if (other !== channel) other.deliver(JSON.parse(JSON.stringify(data)));
      },
      deliver(data: unknown) { channel.onmessage?.({ data }); },
      close() { /* nothing */ },
    };
    channels.push(channel);
    return channel;
  };
  return { make };
}

const instant = async () => undefined;
const req = { rid: 5 };

describe('DockBus routing', () => {
  it('finds no peer when no terminal is alive', async () => {
    const net = network();
    const bus = new DockBus(net.make(), 'main', 'float', () => undefined, 0, instant);
    expect(await bus.findPeer()).toBeNull();
  });

  it('prefers an application tab over a floating window', async () => {
    const net = network();
    const main = new DockBus(net.make(), 'main', 'float', () => undefined, 0, instant);
    new DockBus(net.make(), 'float2', 'float', () => undefined, 0, instant);
    new DockBus(net.make(), 'page1', 'page', () => undefined, 0, instant);
    const peer = await main.findPeer();
    expect(peer).toEqual({ id: 'page1', mode: 'page' });
  });

  it('delivers an open request only to the chosen terminal', async () => {
    const net = network();
    const received: string[] = [];
    const main = new DockBus(net.make(), 'main', 'float', () => undefined, 0, instant);
    new DockBus(net.make(), 'page1', 'page', (request: { rid: number }) => received.push(`page1:${request.rid}`), 0, instant);
    new DockBus(net.make(), 'float2', 'float', (request: { rid: number }) => received.push(`float2:${request.rid}`), 0, instant);
    const peer = await main.findPeer();
    main.send(peer!, req);
    expect(received).toEqual(['page1:5']);
  });

  it('does not answer its own probe', async () => {
    const net = network();
    const onlyMe = new DockBus(net.make(), 'main', 'float', () => undefined, 0, instant);
    expect(await onlyMe.findPeer()).toBeNull();
  });
});

describe('DockBus without a terminal in this window', () => {
  it('does not answer probes or accept open requests until a terminal is declared', async () => {
    const net = network();
    const received: unknown[] = [];
    const idle = new DockBus(net.make(), 'idle', null, (request) => received.push(request), 0, instant);
    const caller = new DockBus(net.make(), 'caller', null, () => undefined, 0, instant);
    expect(await caller.findPeer()).toBeNull();
    caller.send({ id: 'idle', mode: 'float' }, req);
    expect(received).toEqual([]);

    idle.setMode('float');
    expect(await caller.findPeer()).toEqual({ id: 'idle', mode: 'float' });
  });
});

describe('DockBus window modes', () => {
  it('reports when an application tab appears and disappears', () => {
    const net = network();
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const seen: boolean[] = [];
    floating.onPageChange((open) => seen.push(open));
    const page = new DockBus(net.make(), 'page', null, () => undefined, 0, instant);
    page.setMode('page');
    page.setMode('float');
    expect(seen).toEqual([true, false]);
  });

  it('stays open while any application tab is open', () => {
    const net = network();
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const seen: boolean[] = [];
    floating.onPageChange((open) => seen.push(open));
    const first = new DockBus(net.make(), 'page1', null, () => undefined, 0, instant);
    const second = new DockBus(net.make(), 'page2', null, () => undefined, 0, instant);
    first.setMode('page');
    second.setMode('page');
    first.setMode('float');
    expect(seen).toEqual([true]);
    second.setMode('float');
    expect(seen).toEqual([true, false]);
  });

  it('reports the owner of a window that says goodbye', () => {
    const net = network();
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const gone: string[] = [];
    floating.onWindowGone((owner) => gone.push(owner));
    const page = new DockBus(net.make(), 'page', null, () => undefined, 0, instant);
    page.setMode('page');
    page.bye();
    expect(gone).toEqual(['page']);
  });

  it('frees consoles only when a window closes, not when it changes mode', () => {
    const net = network();
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const gone: string[] = [];
    floating.onWindowGone((owner) => gone.push(owner));
    const page = new DockBus(net.make(), 'page', 'page', () => undefined, 0, instant);
    page.setMode('float');
    expect(gone).toEqual([]);
  });

  it('treats a window that stops heartbeating as gone', () => {
    const net = network();
    let clock = 0;
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant, () => clock);
    const seen: boolean[] = [];
    const gone: string[] = [];
    floating.onPageChange((open) => seen.push(open));
    floating.onWindowGone((owner) => gone.push(owner));
    const page = new DockBus(net.make(), 'page', 'page', () => undefined, 0, instant, () => clock);
    page.heartbeat();
    clock = PAGE_TIMEOUT_MS + 1;
    floating.sweep();
    expect(gone).toEqual(['page']);
    expect(seen).toEqual([true, false]);
  });

  it('keeps a window that keeps heartbeating', () => {
    const net = network();
    let clock = 0;
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant, () => clock);
    const seen: boolean[] = [];
    floating.onPageChange((open) => seen.push(open));
    const page = new DockBus(net.make(), 'page', 'page', () => undefined, 0, instant, () => clock);
    page.heartbeat();
    clock = PAGE_TIMEOUT_MS - 1;
    page.heartbeat();
    clock = PAGE_TIMEOUT_MS + 1000;
    floating.sweep();
    expect(seen).toEqual([true]);
  });

  it('learns about application tabs that were open before this window started', async () => {
    const net = network();
    new DockBus(net.make(), 'page', 'page', () => undefined, 0, instant);
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const seen: boolean[] = [];
    floating.onPageChange((open) => seen.push(open));
    await floating.query();
    expect(seen).toEqual([true]);
  });

  it('ignores floating windows when looking for tabs', async () => {
    const net = network();
    new DockBus(net.make(), 'other', 'float', () => undefined, 0, instant);
    const floating = new DockBus(net.make(), 'float', 'float', () => undefined, 0, instant);
    const seen: boolean[] = [];
    floating.onPageChange((open) => seen.push(open));
    await floating.query();
    expect(seen).toEqual([]);
  });
});

describe('DockBus raise (sidebar Console button)', () => {
  it('asks the application tab to show its terminal, not a floating one', async () => {
    const net = network();
    const raised: string[] = [];
    const main = new DockBus(net.make(), 'main', null, () => undefined, 0, instant);
    const page = new DockBus(net.make(), 'page1', 'page', () => undefined, 0, instant);
    const float = new DockBus(net.make(), 'float2', 'float', () => undefined, 0, instant);
    page.onRaise(() => raised.push('page1'));
    float.onRaise(() => raised.push('float2'));
    const peer = await main.findPeer();
    expect(peer).toEqual({ id: 'page1', mode: 'page' });
    main.raise(peer!);
    expect(raised).toEqual(['page1']);
  });

  it('does not raise anything in a window without a terminal', async () => {
    const net = network();
    const raised: string[] = [];
    const main = new DockBus(net.make(), 'main', null, () => undefined, 0, instant);
    const quiet = new DockBus(net.make(), 'quiet', null, () => undefined, 0, instant);
    quiet.onRaise(() => raised.push('quiet'));
    main.raise({ id: 'quiet', mode: 'float' });
    expect(raised).toEqual([]);
  });
});
