import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useNotificationsStore } from '../../stores/notifications';
import { catLabel, levelIcon } from './labels';
import { Icon } from '../../components/Icon';
import { fmtTime } from '../../lib/format';
import { t, translateMessage, useLanguageStore } from '../../i18n';
import type { NotificationItem } from '../../types/api';

const PAGE_SIZE = 20;

interface Position {
  left: number;
  top: number;
  width: number;
  maxHeight: number;
}

const INITIAL_POSITION: Position = { left: 8, top: 68, width: 400, maxHeight: 560 };

const levelTone: Record<string, string> = {
  success: 'bg-ok/14 text-ok',
  warning: 'bg-warn/14 text-warn',
  error: 'bg-bad/14 text-bad',
  info: 'bg-accent-2/14 text-accent-2',
};

export function NotificationsPanel() {
  const open = useNotificationsStore((state) => state.open);
  const items = useNotificationsStore((state) => state.items);
  const unread = useNotificationsStore((state) => state.unread);
  const categories = useNotificationsStore((state) => state.categories);
  const mark = useNotificationsStore((state) => state.mark);
  const remove = useNotificationsStore((state) => state.remove);
  const readAll = useNotificationsStore((state) => state.readAll);
  const clearRead = useNotificationsStore((state) => state.clearRead);
  const setOpen = useNotificationsStore((state) => state.setOpen);
  const wsLive = useNotificationsStore((state) => state.wsLive);
  const navigate = useNavigate();
  const [visible, setVisible] = useState(PAGE_SIZE);
  const [position, setPosition] = useState<Position>(INITIAL_POSITION);
  useLanguageStore((state) => state.language);

  const measure = useCallback(() => {
    const anchor = document.querySelector<HTMLElement>('[data-notification-bell]');
    const rect = anchor?.getBoundingClientRect();
    const width = Math.min(430, window.innerWidth - 16);
    const right = rect?.right ?? window.innerWidth - 16;
    const left = Math.min(Math.max(8, right - width), Math.max(8, window.innerWidth - width - 8));
    const top = Math.max(8, (rect?.bottom ?? 58) + 8);
    const maxHeight = Math.min(640, Math.max(200, window.innerHeight - top - 12));
    setPosition({ left, top, width, maxHeight });
  }, []);

  useLayoutEffect(() => {
    if (open) measure();
  }, [open, measure]);

  useEffect(() => {
    if (!open) return;
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [open, measure]);

  useEffect(() => {
    if (open) setVisible(PAGE_SIZE);
  }, [open]);

  const sorted = useMemo(
    () => [...items].sort((a, b) => b.created_at - a.created_at || b.id - a.id),
    [items],
  );
  const shown = sorted.slice(0, visible);
  const remaining = Math.max(0, sorted.length - shown.length);

  const openItem = (item: NotificationItem) => {
    if (!item.read) void mark(item.id, true);
    if (item.link) {
      navigate(item.link);
      setOpen(false);
    }
  };

  return (
    <div
      id="notifications-panel"
      data-open={open}
      className={`fixed z-[105] flex flex-col overflow-hidden rounded-app border border-line bg-panel shadow-[0_24px_60px_-20px_rgba(0,0,0,.6),0_2px_8px_rgba(0,0,0,.25)] transition-[opacity,transform,visibility] duration-150 ${open ? 'visible translate-y-0 scale-100 opacity-100' : 'invisible -translate-y-2 scale-[.985] opacity-0'}`}
      role="dialog"
      aria-label={t('Notifications')}
      aria-hidden={!open}
      style={{ left: position.left, top: position.top, width: position.width, maxHeight: position.maxHeight }}
    >
      <div className="flex flex-none flex-wrap items-center gap-2 border-b border-line px-3.5 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <b id="notifications-title" className="whitespace-nowrap text-[15px]">{t('Notifications')}</b>
          {unread > 0 && <span className="whitespace-nowrap rounded-full bg-accent-soft px-2 py-0.5 text-xs font-semibold text-accent">{unread} {t('Unread')}</span>}
        </div>
        <span className="flex-1" />
        <button type="button" className="min-h-7 gap-[5px] px-[9px] py-1 text-[12.5px]" disabled={unread === 0} onClick={() => void readAll()}>
          {t('Read all')}
        </button>
        <button type="button" className="min-h-7 gap-[5px] px-[9px] py-1 text-[12.5px] text-muted hover:text-bad" disabled={!items.some((item) => item.read)}
          title={t('Delete read notifications')} onClick={() => void clearRead()}>
          <Icon name="trash" size={14} /><span>{t('Clear read')}</span>
        </button>
      </div>

      {sorted.length ? (
        <ul className="m-0 min-h-0 flex-1 list-none overflow-auto overscroll-contain p-1.5">
          {shown.map((item) => (
            <li key={item.id} data-notification-item data-read={item.read} className={`group relative flex items-start gap-2.5 rounded-app-sm p-2.5 transition-colors [&+&]:before:absolute [&+&]:before:left-12 [&+&]:before:right-2.5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-line [&+&]:before:content-[''] ${item.read ? 'opacity-75 hover:bg-panel2 hover:opacity-100' : 'bg-accent/[.07] hover:bg-accent/[.12]'}`}>
              <span className={`mt-px grid size-7 flex-none place-items-center rounded-full ${levelTone[item.level]}`}><Icon name={levelIcon(item.level)} size={16} /></span>
              <button type="button" className="block min-h-0 min-w-0 flex-1 rounded-none border-0 bg-transparent p-0 text-left text-inherit hover:border-transparent hover:bg-transparent" onClick={() => openItem(item)}>
                <span data-notification-title className={`flex items-center gap-[7px] [overflow-wrap:anywhere] ${item.read ? 'font-semibold' : 'font-bold'}`}>
                  {!item.read && <i className="inline-block size-[7px] flex-none rounded-full bg-accent" aria-hidden="true" />}
                  {translateMessage(item.title, item.params)}
                </span>
                {item.body && <span className="mt-0.5 block text-[13px] text-muted [overflow-wrap:anywhere]">{translateMessage(item.body, item.params)}</span>}
                <span data-notification-meta className="mt-[5px] flex flex-wrap items-center gap-1.5 text-[11.5px] text-muted">
                  <span className="font-semibold text-accent-2">{catLabel(categories, item.category)}</span>
                  <span aria-hidden="true">·</span>
                  <time dateTime={new Date(item.created_at * 1000).toISOString()}>{fmtTime(item.created_at)}</time>
                  <span data-notification-state className={`whitespace-nowrap rounded-full px-[7px] py-[3px] text-[10.5px] font-semibold leading-none ${item.read ? 'bg-panel3 text-muted' : 'bg-accent-soft text-accent'}`}>{item.read ? t('Read') : t('Unread')}</span>
                </span>
              </button>
              <div className="flex flex-none gap-0.5 opacity-45 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
                <button type="button" className="size-7 min-h-7 border-transparent bg-transparent p-0 text-muted hover:bg-panel3 hover:text-text" title={item.read ? t('Mark as unread') : t('Mark as read')}
                  aria-label={item.read ? t('Mark as unread') : t('Mark as read')}
                  onClick={() => void mark(item.id, !item.read)}>
                  <Icon name={item.read ? 'unread' : 'read'} size={14} />
                </button>
                <button type="button" className="size-7 min-h-7 border-transparent bg-transparent p-0 text-muted hover:bg-panel3 hover:text-bad" title={t('Delete notification')}
                  aria-label={t('Delete notification')} onClick={() => void remove(item.id)}>
                  <Icon name="trash" size={14} />
                </button>
              </div>
            </li>
          ))}
          {remaining > 0 && (
            <li className="flex justify-center px-1.5 pb-1 pt-2">
              <button type="button" className="min-h-7 gap-[5px] px-[9px] py-1 text-[12.5px]" onClick={() => setVisible((value) => value + PAGE_SIZE)}>
                {t('Show more')} · {remaining}
              </button>
            </li>
          )}
        </ul>
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-[7px] px-5 py-[38px] text-center text-muted">
          <span className="mb-0.5 grid size-12 place-items-center rounded-full bg-panel2 text-muted"><Icon name="bell" size={26} /></span>
          <b>{t('There are no notifications yet')}</b>
          <small className="max-w-[260px] leading-[1.45] text-muted">{t('Events will appear here')}</small>
        </div>
      )}

      <div className="flex min-h-[34px] flex-none items-center gap-2 border-t border-line px-3.5 py-2">
        <span className={`inline-flex flex-none items-center gap-1.5 whitespace-nowrap text-[11px] before:size-[7px] before:rounded-full before:content-[''] ${wsLive ? 'text-ok before:bg-ok before:shadow-[0_0_0_3px_rgba(34,197,94,.18)] before:motion-safe:animate-[np-pulse_2.2s_ease-in-out_infinite]' : 'text-muted before:bg-warn'}`} title={wsLive ? t('Updates in real time') : t('Update on schedule')}>
          {wsLive ? t('Real time') : t('No connection')}
        </span>
      </div>
    </div>
  );
}
