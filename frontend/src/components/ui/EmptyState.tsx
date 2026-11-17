import type { ReactNode } from 'react';
import { Icon } from '../Icon';

export function EmptyState({ title, text, action }: { title: string; text?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-1.5 rounded-app border border-dashed border-line-2 bg-panel px-[18px] py-10 text-center text-muted [grid-column:1/-1]">
      <div className="mb-1.5 grid size-14 place-items-center rounded-2xl bg-panel2 text-accent"><Icon name="folder" size={28} /></div>
      <b>{title}</b>
      {text && <div className="text-muted">{text}</div>}
      {action}
    </div>
  );
}
