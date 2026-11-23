import { useMemo } from 'react';
import { useNotificationsStore } from '../../stores/notifications';
import { Modal } from '../../components/ui/Modal';
import { Icon } from '../../components/Icon';
import { t, translateMessage, useLanguageStore } from '../../i18n';
import { levelIcon } from './labels';
import { fmtTime } from '../../lib/format';

/**
 * Shows unread messages from administrators as a dialog, oldest first, one at a time. Closing the
 * dialog marks the message as read, so it is not shown again; the message stays in the notification
 * centre as a record.
 */
export function MessagePopup() {
  useLanguageStore((state) => state.language);
  const items = useNotificationsStore((state) => state.items);
  const mark = useNotificationsStore((state) => state.mark);

  const current = useMemo(
    () => items
      .filter((item) => item.category === 'message' && !item.read)
      .sort((a, b) => a.created_at - b.created_at || a.id - b.id)[0],
    [items],
  );

  if (!current) return null;

  const acknowledge = () => { void mark(current.id, true); };

  return (
    <Modal
      title={translateMessage(current.title, current.params)}
      // Only the button closes this dialog: Escape and a click outside are ignored, so nothing is marked read by accident.
      dismissible={false}
      onClose={() => undefined}
      actions={[{ label: t('Got it'), primary: true, closeOnClick: false, onClick: acknowledge }]}
    >
      <div className="flex items-start gap-3">
        <span className="mt-0.5 text-accent-2"><Icon name={levelIcon(current.level)} size={22} /></span>
        <div className="min-w-0 flex-1">
          <p className="m-0 whitespace-pre-wrap text-[15px] leading-relaxed text-text [overflow-wrap:anywhere]">
            {translateMessage(current.body, current.params)}
          </p>
          <div className="mt-3 text-xs text-muted">{fmtTime(current.created_at)}</div>
        </div>
      </div>
    </Modal>
  );
}
