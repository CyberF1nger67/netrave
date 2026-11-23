import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { messagesApi, usersApi } from '../../api/endpoints';
import { Spinner } from '../../components/ui/Spinner';
import { guard, toast } from '../../stores/toast';
import { t, useLanguageStore } from '../../i18n';

const MESSAGE_MAX_LENGTH = 2000;
const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const box = 'mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5';
const boxTitle = 'mb-3 mt-0 text-[13px] font-semibold uppercase tracking-[.06em] text-muted';
const hint = 'mt-2.5 text-xs leading-relaxed text-muted';
const recipientRow = 'flex cursor-pointer items-center gap-3 rounded-md px-1 py-[6px] text-[13.5px] hover:bg-bg-2';
const checkbox = 'h-4 w-4 shrink-0 cursor-pointer accent-accent';

/**
 * Administrator messages: choose recipients (every active user or selected accounts), write the text
 * and send. Each message is delivered as a notification in the notification centre.
 */
export function MessagesPage() {
  useLanguageStore((state) => state.language);
  const query = useQuery({ queryKey: ['users'], queryFn: usersApi.list });
  const [allActive, setAllActive] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [text, setText] = useState('');
  const [level, setLevel] = useState<'info' | 'warning'>('info');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const users = useMemo(() => query.data ?? [], [query.data]);
  const active = useMemo(() => users.filter((user) => !user.disabled), [users]);
  const recipientCount = allActive ? active.length : selected.length;

  // "All active users" and single accounts are alternatives: choosing one of them clears the other.
  const chooseAll = (on: boolean) => {
    setAllActive(on);
    setSelected([]);
    if (error) setError('');
  };

  const toggle = (id: number, on: boolean) => {
    setAllActive(false);
    setSelected((current) => (on ? [...current, id] : current.filter((item) => item !== id)));
    if (error) setError('');
  };

  const send = () => {
    if (busy) return;
    const body = text.trim();
    if (!body) {
      setError(t('Enter the message text'));
      return;
    }
    if (!allActive && selected.length === 0) {
      setError(t('Choose the recipients'));
      return;
    }
    setError('');
    setBusy(true);
    void guard(async () => {
      try {
        const result = await messagesApi.send({
          text: body,
          level,
          all: allActive,
          user_ids: allActive ? [] : selected,
        });
        toast.success(t('Message sent to {count} users', { count: result.sent }));
        setText('');
      } finally {
        setBusy(false);
      }
    });
  };

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;

  return (
    <>
      <h2>{t('Messages')}</h2>
      <div className={box}>
        <h3 className={boxTitle}>{t('Recipients')}</h3>
        <label className={recipientRow}>
          <input type="checkbox" className={checkbox} checked={allActive} onChange={(event) => chooseAll(event.target.checked)} />
          <span>{t('All active users')}</span>
        </label>
        <div className="mt-2 max-h-[280px] overflow-auto">
          {users.map((user) => (
            <label key={user.id} className={`${recipientRow} ${user.disabled ? 'cursor-not-allowed opacity-60' : ''}`}>
              <input type="checkbox" className={checkbox} checked={!allActive && selected.includes(user.id)} disabled={user.disabled}
                onChange={(event) => toggle(user.id, event.target.checked)} />
              <span>{user.username}</span>
              {user.disabled && <span className="text-muted">{t('Disabled')}</span>}
            </label>
          ))}
        </div>
        <div className={hint}>{t('Recipients: {count}', { count: recipientCount })}</div>
      </div>

      <div className={box}>
        <h3 className={boxTitle}>{t('Message')}</h3>
        <label htmlFor="messages-page-text">{t('Message text')}</label>
        <textarea
          id="messages-page-text"
          rows={5}
          maxLength={MESSAGE_MAX_LENGTH}
          disabled={busy}
          value={text}
          onChange={(event) => { setText(event.target.value); if (error) setError(''); }}
        />
        <div className={hint}>{text.length}/{MESSAGE_MAX_LENGTH}</div>
        <label htmlFor="messages-page-level">{t('Level')}</label>
        <select id="messages-page-level" value={level} disabled={busy} onChange={(event) => setLevel(event.target.value as 'info' | 'warning')}>
          <option value="info">{t('Information')}</option>
          <option value="warning">{t('Warning')}</option>
        </select>
        <p className={hint}>{t('Users will see the message in the notification centre.')}</p>
        {error && <div role="alert" aria-live="polite" className="mt-2.5 text-xs leading-relaxed text-bad">{error}</div>}
        <div className="mt-4 flex justify-end">
          <button type="button" className={primary} disabled={busy} onClick={send}>
            {busy ? t('Sending…') : t('Send')}
          </button>
        </div>
      </div>
    </>
  );
}
