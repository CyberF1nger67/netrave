import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

interface ModalAction {
  label: string;
  primary?: boolean;
  danger?: boolean;
  closeOnClick?: boolean;
  onClick: () => void | Promise<void>;
}

interface ModalProps {
  title: string;
  children: ReactNode;
  actions?: ModalAction[];
  wide?: boolean;
  onClose: () => void;
}

export function Modal({ title, children, actions = [], wide, onClose }: ModalProps) {
  const boxRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const previous = document.activeElement;
    if (previous instanceof HTMLElement) previous.blur();
    const box = boxRef.current;
    const first = box?.querySelector<HTMLElement>('input,select,textarea,button');
    if (first) first.focus();
    else box?.focus();
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const host = document.fullscreenElement ?? document.body;
  return createPortal(
    <div className="fixed inset-0 z-[120] flex items-center justify-center bg-[rgba(4,8,14,.6)] p-4 backdrop-blur-[5px] animate-fade-in" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className={`max-h-[90vh] w-[min(560px,100%)] overflow-auto rounded-[18px] border border-line bg-panel p-6 shadow-[0_30px_80px_-20px_rgba(0,0,0,.6)] animate-modal-in max-sm:p-[18px] ${wide ? 'w-[min(1000px,100%)]' : ''}`} role="dialog" aria-modal="true" aria-label={title} ref={boxRef} tabIndex={-1}>
        <h3 className="mb-2.5 mt-0 text-lg font-bold tracking-[-.01em] text-text">{title}</h3>
        <div>{children}</div>
        {actions.length > 0 && (
          <div className="mt-5 flex flex-wrap justify-end gap-2 max-sm:[&>button]:flex-auto">
            {actions.map((action) => (
              <button
                key={action.label}
                type="button"
                className={`${action.primary ? 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110' : ''}${action.danger ? ' text-bad hover:border-bad hover:bg-bad/10' : ''}`}
                onClick={() => {
                  void Promise.resolve(action.onClick()).then(() => {
                    if (action.closeOnClick !== false) onClose();
                  });
                }}
              >
                {action.label}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>,
    host,
  );
}
