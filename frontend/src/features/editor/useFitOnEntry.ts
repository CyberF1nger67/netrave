import { useLayoutEffect, useRef, type RefObject } from 'react';

interface FitOnEntryOptions<T> {
  /** Identity of one visit to a laboratory (lab + instance). A new key is a new entry. */
  sessionKey: string;
  /** True once the topology for `sessionKey` is in component state and the canvas is mounted. */
  ready: boolean;
  /** Changes whenever the loaded topology changes. Not a trigger to refit, only to re-check readiness. */
  version: unknown;
  target: RefObject<Element | null>;
  compute: (rect: DOMRect) => T;
  apply: (value: T) => void;
}

function measurable(rect: DOMRect | undefined): rect is DOMRect {
  return Boolean(rect && rect.width > 0 && rect.height > 0);
}

/**
 * Runs `compute` + `apply` exactly once per `sessionKey`, after the loaded topology has been
 * rendered and the canvas has a measurable size.
 *
 * The first fit is applied synchronously in a layout effect, before the browser paints, so
 * machines never appear at their raw coordinates for a frame. A single animation-frame check
 * afterwards catches a canvas whose final CSS size was not known during the layout pass.
 * If the canvas is not measurable yet (hidden tab, drawer animation, zero-sized flex item),
 * a `ResizeObserver` plus animation frames wait for the real size; nothing is recorded until
 * the fit actually happens. Once fitted, the observer is disconnected and later renders,
 * polling and edits never reset the user's camera.
 */
export function useFitOnEntry<T>({ sessionKey, ready, version, target, compute, apply }: FitOnEntryOptions<T>): void {
  const fittedKey = useRef<string | null>(null);

  useLayoutEffect(() => {
    if (!ready || fittedKey.current === sessionKey) return;

    let frame = 0;
    let observer: ResizeObserver | null = null;
    let cancelled = false;

    const stopWaiting = () => {
      if (frame) { cancelAnimationFrame(frame); frame = 0; }
      observer?.disconnect();
      observer = null;
    };

    const measure = (): DOMRect | undefined => {
      const rect = target.current?.getBoundingClientRect();
      return measurable(rect) ? rect : undefined;
    };

    const fit = (): boolean => {
      const rect = measure();
      if (!rect) return false;
      fittedKey.current = sessionKey;
      stopWaiting();
      apply(compute(rect));
      return true;
    };

    const first = measure();
    if (!first) {
      // Wait for the workspace to become visible/measurable. Each callback either fits
      // once and stops the loop, or schedules one more frame — never an endless refit.
      const retry = () => {
        if (cancelled || fit()) return;
        frame = requestAnimationFrame(retry);
      };
      frame = requestAnimationFrame(retry);
      if (typeof ResizeObserver !== 'undefined') {
        observer = new ResizeObserver(() => { if (!cancelled) fit(); });
        if (target.current) observer.observe(target.current);
      }
      return () => { cancelled = true; stopWaiting(); };
    }

    fittedKey.current = sessionKey;
    apply(compute(first));

    // A layout effect runs before paint, but a canvas that is sized by CSS or by a
    // just-mounted flex parent may still settle on the next frame. Recompute only if
    // the measured size really changed; this is a one-shot check, not a resize loop.
    frame = requestAnimationFrame(() => {
      frame = 0;
      if (cancelled || fittedKey.current !== sessionKey) return;
      const rect = measure();
      if (!rect || (rect.width === first.width && rect.height === first.height)) return;
      apply(compute(rect));
    });

    return () => { cancelled = true; stopWaiting(); };
    // `compute`/`apply` are read from the render that changed `ready`/`version`; they are not triggers.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionKey, ready, version]);
}
