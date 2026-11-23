import { askConfirm } from '../../stores/dialogs';
import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { logsApi } from '../../api/endpoints';
import { can, useAuthStore } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import type { AuditEntry } from '../../types/api';
import { fmtTime } from '../../lib/format';
import { Dropdown } from '../../components/ui/Dropdown';
import { Switch } from '../../components/ui/Switch';
import { Spinner } from '../../components/ui/Spinner';
import { t, translateMessage, useLanguageStore } from '../../i18n';

const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const bar = 'mb-3.5 flex flex-wrap items-end gap-2.5 [&>label]:mb-0 [&>input]:max-w-[320px]';

type Period = 'all' | 'hour' | 'day' | 'week' | 'month';
type Sort = 'new' | 'old';

/** The record text in the interface language: keyed records carry their key and parameters (see database.AuditKey). */
function recordText(entry: AuditEntry): string {
  const key = entry.data?.msg_key;
  if (typeof key !== 'string') return entry.detail;
  const params: Record<string, string> = {};
  const raw = entry.data?.msg_params;
  if (raw && typeof raw === 'object') for (const [name, value] of Object.entries(raw)) params[name] = String(value);
  return translateMessage(key, params);
}

/** Readable action group: "lab_export" -> "lab". */
function actionGroup(action: string): string {
  const cut = action.search(/[_.:]/);
  return cut > 0 ? action.slice(0, cut) : action;
}

export function LogsPage() {
  const user = useAuthStore((state) => state.user);
  const canClear = can(user, 'logs');
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['logs'], queryFn: () => logsApi.list(1000) });
  useLanguageStore((state) => state.language);
  const [text, setText] = useState('');
  const [userFilter, setUserFilter] = useState('');
  const [action, setAction] = useState('');
  const [period, setPeriod] = useState<Period>('all');
  const [sort, setSort] = useState<Sort>('new');
  const [onlyErrors, setOnlyErrors] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState('50');
  const entries = query.data ?? [];
  // All hooks are called before any returns: otherwise the number of hooks changes between
  // the first render (loading) and the second (data), and React aborts the page with an error.
  const users = useMemo(() => [...new Set(entries.map((entry) => entry.username).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'ru')), [entries]);
  const actions = useMemo(() => [...new Set(entries.map((entry) => actionGroup(entry.action)))].sort((a, b) => a.localeCompare(b, 'ru')), [entries]);

  const periodStart = useMemo(() => {
    if (period === 'all') return 0;
    const now = Date.now() / 1000;
    return now - ({ hour: 3600, day: 86400, week: 604800, month: 2592000 }[period] ?? 0);
  }, [period]);

  const filtered = useMemo(() => entries
    .filter((entry) => {
      if (periodStart && entry.ts < periodStart) return false;
      if (userFilter && entry.username !== userFilter) return false;
      if (action && actionGroup(entry.action) !== action) return false;
      if (onlyErrors && !/error|fail/i.test(`${entry.action} ${entry.detail}`)) return false;
      if (text) {
        const haystack = `${entry.username} ${entry.action} ${entry.detail} ${entry.ip}`.toLowerCase();
        if (!haystack.includes(text.toLowerCase())) return false;
      }
      return true;
    })
    .sort((a, b) => (sort === 'new' ? b.ts - a.ts || b.id - a.id : a.ts - b.ts || a.id - b.id)), [entries, periodStart, userFilter, action, onlyErrors, text, sort]);

  const size = Math.max(10, Number.parseInt(pageSize, 10) || 50);
  const pages = Math.max(1, Math.ceil(filtered.length / size));
  const current = Math.min(page, pages);
  const visible = filtered.slice((current - 1) * size, current * size);
  const resetPage = () => setPage(1);
  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;

  const clear = async () => {
    if (!(await askConfirm({ title: t('All journal entries will be deleted. This is irreversible.'), danger: true }))) return;
    void guard(async () => {
      await logsApi.clear();
      toast.success(t('Journal cleared'));
      await queryClient.invalidateQueries({ queryKey: ['logs'] });
    });
  };
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2>{t('Journal')}</h2>
        {canClear && (
          <div className="mb-5 flex items-center gap-2">
            <button type="button" className={danger} disabled={!entries.length} onClick={clear}>{t('Clear journal')}</button>
          </div>
        )}
      </div>
      <div className={`${bar}`}>
        <label className="min-w-[220px] flex-1">
          <span className="mb-1.5 block text-xs text-muted">{t('Search')}</span>
          <input type="search" placeholder={t('Search by user, action, details or IP…')} value={text}
            onChange={(event) => { setText(event.target.value); resetPage(); }} />
        </label>
        <label className="w-full max-w-[220px]">
          <span className="mb-1.5 block text-xs text-muted">{t('User')}</span>
          <Dropdown value={userFilter} onChange={(value) => { setUserFilter(value); resetPage(); }} options={[
            { value: '', label: t('All users') },
            ...users.map((name) => ({ value: name, label: name })),
          ]} />
        </label>
        <label className="w-full max-w-[220px]">
          <span className="mb-1.5 block text-xs text-muted">{t('Action')}</span>
          <Dropdown value={action} onChange={(value) => { setAction(value); resetPage(); }} options={[
            { value: '', label: t('All actions') },
            ...actions.map((name) => ({ value: name, label: name })),
          ]} />
        </label>
        <label className="w-full max-w-[200px]">
          <span className="mb-1.5 block text-xs text-muted">{t('Period')}</span>
          <Dropdown value={period} onChange={(value) => { setPeriod(value as Period); resetPage(); }} options={[
            { value: 'all', label: t('All time') },
            { value: 'hour', label: t('Last hour') },
            { value: 'day', label: t('Last 24 hours') },
            { value: 'week', label: t('Last 7 days') },
            { value: 'month', label: t('Last 30 days') },
          ]} />
        </label>
        <label className="w-full max-w-[170px]">
          <span className="mb-1.5 block text-xs text-muted">{t('Sort')}</span>
          <Dropdown value={sort} onChange={(value) => { setSort(value as Sort); resetPage(); }} options={[
            { value: 'new', label: t('Newest first') },
            { value: 'old', label: t('Oldest first') },
          ]} />
        </label>
        <label className="w-full max-w-[150px]">
          <span className="mb-1.5 block text-xs text-muted">{t('Rows per page')}</span>
          <Dropdown value={pageSize} onChange={(value) => { setPageSize(value); resetPage(); }} options={[
            { value: '25', label: '25' }, { value: '50', label: '50' }, { value: '100', label: '100' }, { value: '200', label: '200' },
          ]} />
        </label>
        <div className="flex min-h-9 items-center pb-1.5">
          <Switch checked={onlyErrors} label={t('Problems only')} onChange={(checked) => { setOnlyErrors(checked); resetPage(); }} />
        </div>
      </div>
      <div className="mb-2.5 text-xs text-muted">{t('Shown')}: {filtered.length} {t('of')} {entries.length}</div>
      <div className="mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5">
        {visible.length ? (
          <table>
            <thead><tr>{[t('Time'), t('User'), t('Action'), t('Details'), t('IP')].map((title) => <th key={title}>{title}</th>)}</tr></thead>
            <tbody>
              {visible.map((entry) => (
                <tr key={entry.id}>
                  <td className="whitespace-nowrap text-muted">{fmtTime(entry.ts)}</td><td>{entry.username || '—'}</td>
                  <td>{entry.action}</td><td>{recordText(entry)}</td><td className="text-muted">{entry.ip}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : <div className="py-6 text-center text-muted">{entries.length ? t('Nothing found') : t('Journal is empty')}</div>}
      </div>
      {pages > 1 && (
        <div className="flex flex-wrap items-center justify-center gap-2">
          <button type="button" disabled={current <= 1} onClick={() => setPage(current - 1)}>{t('Back')}</button>
          <span className="text-muted">{current} / {pages}</span>
          <button type="button" disabled={current >= pages} onClick={() => setPage(current + 1)}>{t('Next')}</button>
        </div>
      )}
    </>
  );
}
