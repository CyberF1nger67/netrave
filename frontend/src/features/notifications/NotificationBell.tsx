import { useEffect, useRef } from 'react';
import { useNotificationsStore } from '../../stores/notifications';
import { Icon } from '../../components/Icon';
import { t, useLanguageStore } from '../../i18n';

export function NotificationBell() {
  const unread = useNotificationsStore((state) => state.unread);
  const open = useNotificationsStore((state) => state.open);
  const toggle = useNotificationsStore((state) => state.toggle);
  const setOpen = useNotificationsStore((state) => state.setOpen);
  const ref = useRef<HTMLButtonElement>(null);
  useLanguageStore((state) => state.language);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (ref.current?.contains(target)) return;
      if (target instanceof Element && target.closest('#notifications-panel')) return;
      setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, setOpen]);

  return (
    <button
      ref={ref}
      type="button"
      data-notification-bell
      className={`relative size-[38px] min-h-[38px] rounded-app-sm border-transparent bg-transparent p-0 ${unread ? 'text-text [&>svg]:motion-safe:animate-[bell-ring_2.4s_var(--ease)_1]' : 'text-muted'} hover:bg-panel2 hover:text-text`}
      title={unread ? `${t('Notifications')}: ${unread}` : t('Notifications')}
      aria-label={t('Notifications')}
      aria-haspopup="dialog"
      aria-controls="notifications-panel"
      aria-expanded={open}
      onClick={(event) => {
        event.stopPropagation();
        toggle();
      }}
    >
      <Icon name="bell" size={18} />
      <span data-notification-count className="pointer-events-none absolute right-0 top-0.5 h-[18px] min-w-[18px] rounded-full bg-bad px-[5px] text-center text-[11px] font-bold leading-[18px] text-white shadow-[0_0_0_2px_var(--panel)]" hidden={unread <= 0} aria-live="polite">{unread > 99 ? '99+' : unread}</span>
    </button>
  );
}
