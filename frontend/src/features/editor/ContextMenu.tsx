import { useEffect, useRef, useState } from 'react';

export interface MenuItem {
  label?: string;
  separator?: boolean;
  disabled?: boolean;
  sub?: MenuItem[];
  run?: () => void;
}

const SUB_WIDTH = 220;
const ROW_HEIGHT = 35;

interface OpenSub { item: MenuItem; top: number; side: 'right' | 'left' }

/**
 * Right-click menu. The list is the scroll container (max-height + overflow), and the submenu is a
 * sibling of it, not a child: a child positioned outside the list would be clipped by the list's
 * overflow and the submenu would never be visible.
 */
export function ContextMenu({ x, y, items, onClose }: { x: number; y: number; items: MenuItem[]; onClose: () => void }) {
  const [sub, setSub] = useState<OpenSub | null>(null);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const down = (event: MouseEvent) => {
      if (!(event.target instanceof Node) || !root.current?.contains(event.target)) onClose();
    };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose(); };
    document.addEventListener('mousedown', down);
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key); };
  }, [onClose]);

  const openSub = (item: MenuItem, row: HTMLElement) => {
    const frame = root.current?.getBoundingClientRect();
    if (!frame) return;
    const rowRect = row.getBoundingClientRect();
    const height = Math.min(window.innerHeight * 0.8, (item.sub?.length ?? 0) * ROW_HEIGHT + 12);
    const top = Math.max(4 - frame.top, Math.min(rowRect.top - frame.top, window.innerHeight - frame.top - height - 4));
    const side = frame.right + SUB_WIDTH > window.innerWidth && frame.left - SUB_WIDTH >= 0 ? 'left' : 'right';
    setSub({ item, top, side });
  };

  const left = Math.min(x, window.innerWidth - 240);
  const top = Math.min(y, window.innerHeight - Math.min(520, items.length * ROW_HEIGHT + 20));
  const sideStyle = sub?.side === 'left' ? { right: '100%' } : { left: '100%' };

  return (
    <div ref={root} data-ctx-menu style={{ position: 'fixed', left, top, zIndex: 110 }} onClick={(event) => event.stopPropagation()}>
      <div data-ctx-list className="relative max-h-[80vh] min-w-[210px] overflow-auto rounded-app-sm border border-line bg-panel p-1.5 shadow-app" style={{ position: 'relative' }}>
        {items.map((item, index) => {
          if (item.separator) return <hr key={index} className="mx-0 my-1 border-0 border-t border-line" />;
          return (
            <button key={index} type="button" disabled={item.disabled}
              className={`block w-full min-h-0 justify-start whitespace-nowrap rounded-lg border-0 bg-transparent px-[11px] py-2 text-left font-medium hover:bg-panel2 ${item.sub ? 'flex items-center justify-between gap-3.5' : ''}`}
              onMouseEnter={(event) => { if (item.sub) openSub(item, event.currentTarget); else setSub(null); }}
              onClick={(event) => {
                if (item.sub) { openSub(item, event.currentTarget); return; }
                item.run?.(); onClose();
              }}>
              {item.label}{item.sub && <span className="opacity-60">▸</span>}
            </button>
          );
        })}
      </div>
      {sub && (
        <div data-ctx-sub className="absolute z-[111] min-w-[210px] rounded-app-sm border border-line bg-panel p-1.5 shadow-app" role="menu" style={{ position: 'absolute', top: sub.top, ...sideStyle, maxHeight: '80vh', overflow: 'auto' }}>
          {sub.item.sub?.map((item, index) => item.separator ? <hr key={index} className="mx-0 my-1 border-0 border-t border-line" /> : (
            <button key={index} type="button" disabled={item.disabled} className="block w-full min-h-0 justify-start whitespace-nowrap rounded-lg border-0 bg-transparent px-[11px] py-2 text-left font-medium hover:bg-panel2" onClick={() => { item.run?.(); onClose(); }}>{item.label}</button>
          ))}
        </div>
      )}
    </div>
  );
}
