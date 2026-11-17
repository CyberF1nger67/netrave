import { describe, expect, it, vi, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { RefObject } from 'react';
import { useFitOnEntry } from './useFitOnEntry';

function setup(initial: { sessionKey: string; ready: boolean; version: unknown; width: number }) {
  const size = { width: initial.width, height: 600 };
  const target = { current: { getBoundingClientRect: () => ({ width: size.width, height: size.height, left: 0, top: 0, right: size.width, bottom: size.height }) as DOMRect } } as RefObject<Element | null>;
  const compute = vi.fn((_rect: DOMRect) => ({ fitted: true }));
  const apply = vi.fn();
  const hook = renderHook((props: typeof initial) => useFitOnEntry({ ...props, target, compute, apply }), { initialProps: initial });
  return { hook, size, compute, apply };
}

describe('useFitOnEntry', () => {
  afterEach(() => { vi.unstubAllGlobals(); });

  it('does nothing until the topology for this entry is ready', () => {
    const { apply } = setup({ sessionKey: 'l1:0', ready: false, version: 1, width: 800 });
    expect(apply).not.toHaveBeenCalled();
  });

  it('fits once when the entry becomes ready and never again on later renders', () => {
    const { hook, compute, apply } = setup({ sessionKey: 'l1:0', ready: true, version: 1, width: 800 });
    expect(apply).toHaveBeenCalledTimes(1);
    expect(compute).toHaveBeenCalledTimes(1);
    hook.rerender({ sessionKey: 'l1:0', ready: true, version: 2, width: 800 });
    hook.rerender({ sessionKey: 'l1:0', ready: true, version: 3, width: 800 });
    expect(apply).toHaveBeenCalledTimes(1);
  });

  it('fits again for a new entry (different lab or instance)', () => {
    const { hook, apply } = setup({ sessionKey: 'l1:0', ready: true, version: 1, width: 800 });
    hook.rerender({ sessionKey: 'l2:0', ready: false, version: 1, width: 800 });
    expect(apply).toHaveBeenCalledTimes(1);
    hook.rerender({ sessionKey: 'l2:0', ready: true, version: 2, width: 800 });
    expect(apply).toHaveBeenCalledTimes(2);
  });

  it('does not fit while the canvas has no size, and retries on a later update', () => {
    const { hook, size, apply } = setup({ sessionKey: 'l1:0', ready: true, version: 1, width: 0 });
    expect(apply).not.toHaveBeenCalled();
    size.width = 800;
    hook.rerender({ sessionKey: 'l1:0', ready: true, version: 2, width: 0 });
    expect(apply).toHaveBeenCalledTimes(1);
  });

  it('passes the measured rectangle to compute', () => {
    const { compute } = setup({ sessionKey: 'l1:0', ready: true, version: 1, width: 640 });
    expect(compute.mock.calls[0]?.[0]).toMatchObject({ width: 640, height: 600 });
  });

  it('waits for the canvas to get a real size even when the topology version does not change', () => {
    const callbacks: ResizeObserverCallback[] = [];
    const observed: Element[] = [];
    class TestResizeObserver {
      constructor(callback: ResizeObserverCallback) { callbacks.push(callback); }
      observe(target: Element) { observed.push(target); }
      unobserve() { /* not needed for this test */ }
      disconnect() { /* not needed for this test */ }
    }
    vi.stubGlobal('ResizeObserver', TestResizeObserver);

    const { size, apply } = setup({ sessionKey: 'l1:0', ready: true, version: 1, width: 0 });
    expect(apply).not.toHaveBeenCalled();
    expect(observed).toHaveLength(1);

    size.width = 800;
    act(() => { for (const callback of callbacks) callback([], {} as ResizeObserver); });
    expect(apply).toHaveBeenCalledTimes(1);
  });
});
