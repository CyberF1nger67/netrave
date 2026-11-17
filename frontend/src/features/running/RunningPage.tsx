import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { labPath, paths } from '../../routing/paths';
import { folderNameOf } from '../../routing/labLookup';
import { devicesApi, foldersApi, labsApi } from '../../api/endpoints';
import type { MachineStatus, RunningLab } from '../../types/api';
import { useAuthStore } from '../../stores/auth';
import { guard } from '../../stores/toast';
import { fmtBytes, fmtNum, fmtPct, fmtTime, fmtUptime } from '../../lib/format';
import { openConsole } from '../consoles/runtime';
import { WipeLabDialog } from '../labs/LabDialogs';
import { Icon } from '../../components/Icon';
import { EmptyState } from '../../components/ui/EmptyState';
import { Spinner } from '../../components/ui/Spinner';
import { Dropdown } from '../../components/ui/Dropdown';
import { t, useLanguageStore } from '../../i18n';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const cardTone: Record<string, string> = { ok: 'before:bg-ok', warn: 'before:bg-warn', bad: 'before:bg-bad', muted: 'before:bg-line-2' };
const badgeTone: Record<string, string> = { ok: 'bg-ok/14 text-ok', warn: 'bg-warn/14 text-warn', bad: 'bg-bad/14 text-bad', muted: 'bg-panel3 text-muted' };
const textTone: Record<string, string> = { ok: 'text-ok', warn: 'text-warn', bad: 'text-bad', muted: 'text-muted' };
const segmentTone: Record<string, string> = { ok: 'bg-ok', stale: 'bg-[repeating-linear-gradient(45deg,var(--bad)_0_4px,transparent_4px_8px)]', busy: 'bg-warn', error: 'bg-bad', off: 'bg-line-2' };
const primaryBtn = primary;
const dangerBtn = danger;
const acts = 'flex flex-wrap items-center gap-1.5 [&>button]:w-[132px] [&>button]:min-h-[30px] [&>button]:justify-center [&>button]:px-2.5 [&>button]:py-[5px] [&>button]:text-[12.5px]';
const kpi = "relative overflow-hidden rounded-app border border-line bg-panel px-4 pb-[13px] pt-3.5 shadow-app before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-gradient-to-b before:from-accent before:to-accent-2 before:content-['']";

// State filters: running, stopped, wiped and with problems.
const FILTERS = [['all', 'All'], ['running', 'Running'], ['stopped', 'Stopped'], ['wiped', 'Wiped'], ['problem', 'Problems'], ['mine', 'Mine']] as const;
const PROBLEM = ['error', 'stale', 'busy'];
const STATUS: Record<string, [string, string]> = {
  running: ['Running', 'ok'], partial: ['Partial', 'warn'], busy: ['Partial', 'warn'],
  error: ['Error', 'bad'], stale: ['Error', 'bad'], stopped: ['Stopped', 'muted'], wiped: ['Wiped', 'muted'],
};
// Stopped and wiped labs can be started again: the actions repeat the launch panel.
const CAN_START = ['stopped', 'partial', 'stale', 'wiped'];
const MSTATE: Record<string, [string, string]> = {
  running: ['works', 'ok'], paused: ['paused', 'warn'], starting: ['starting', 'warn'],
  stopping: ['stopping', 'warn'], stopped: ['stopped', 'muted'], error: ['error', 'bad'],
};

function MachineRow({ row, machine }: { row: RunningLab; machine: MachineStatus }) {
  const queryClient = useQueryClient();
  const [labelKey, cls] = MSTATE[machine.state] ?? [machine.state, 'muted'];
  const label = t(labelKey as Parameters<typeof t>[0]);
  const live = machine.running;
  const moving = machine.state === 'starting' || machine.state === 'stopping';
  const act = (action: string, question?: string) => {
    if (question && !confirm(question)) return;
    void guard(async () => {
      await devicesApi.action(machine.rid, action);
      await queryClient.invalidateQueries({ queryKey: ['lab-status', row.instance_id] });
      await queryClient.invalidateQueries({ queryKey: ['running'] });
    });
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2 border-t border-line py-[9px] text-[13px]">
      <div className="flex min-w-[170px] flex-1 items-center gap-[9px] max-sm:w-full">
        {machine.icon_url ? <img className="size-7 flex-none rounded-lg bg-panel2 object-contain" src={machine.icon_url} alt="" /> : <span className="inline-block size-7 flex-none rounded-lg border border-dashed border-line-2 bg-panel2" />}
        <div className="min-w-0"><b className="[overflow-wrap:anywhere]">{machine.name}</b><div className="text-[11.5px] text-muted">{machine.type}{machine.pid ? ` · PID ${machine.pid}` : ''}</div></div>
      </div>
      <div className={`inline-flex flex-none items-center gap-1.5 whitespace-nowrap text-[12.5px] font-semibold ${textTone[cls] ?? 'text-muted'}`}><i className="size-[7px] rounded-full bg-current" />{label}</div>
      <div className="flex flex-none flex-col tabular-nums"><span className="text-[10.5px] uppercase tracking-[.05em] text-muted">CPU</span><b>{live ? (machine.cpu_ready ? fmtPct(machine.cpu_pct) : '…') : '—'}</b></div>
      <div className="flex flex-none flex-col tabular-nums"><span className="text-[10.5px] uppercase tracking-[.05em] text-muted">{t('RAM')}</span><b>{live ? fmtBytes(machine.ram_bytes) : '—'}</b></div>
      <div className="flex flex-none flex-col tabular-nums"><span className="text-[10.5px] uppercase tracking-[.05em] text-muted">{t('Disk')}</span><b>{fmtBytes(machine.disk_bytes)}</b></div>
      <div className="flex flex-1 flex-wrap justify-end gap-[5px] max-sm:justify-start [&>button]:w-[132px] [&>button]:min-h-[30px] [&>button]:justify-center [&>button]:px-2.5 [&>button]:py-[5px] [&>button]:text-[12.5px]">
        {row.own && !moving ? (
          <>
            {live && <button type="button" title={t('Console')} onClick={() => openConsole({ rid: machine.rid, name: machine.name, lab_id: row.id, state: machine.state, console: machine.console, lab_perms: {} }, machine.console === 'vnc' ? 'vnc' : 'serial')}><Icon name="term" size={14} />{t('Console')}</button>}
            {live && <button type="button" onClick={() => act('stop')}><Icon name="stop" size={14} />{t('Stop')}</button>}
            {live && <button type="button" onClick={() => act('restart')}><Icon name="restart" size={14} />{t('Restart')}</button>}
            {machine.state === 'running' && <button type="button" onClick={() => act('suspend')}><Icon name="pause" size={14} />{t('Pause')}</button>}
            {machine.state === 'paused' && <button type="button" onClick={() => act('resume')}><Icon name="play" size={14} />{t('Resume')}</button>}
            {!live && <button type="button" onClick={() => act('start')}><Icon name="play" size={14} />{t('Start')}</button>}
            <button type="button" className={dangerBtn} onClick={() => act('wipe', `${t('Reset disk of machine?')} (${machine.name})`)}><Icon name="trash" size={14} />{t('Wipe')}</button>
          </>
        ) : <span className="text-muted">—</span>}
      </div>
    </div>
  );
}

function MachinesPanel({ row }: { row: RunningLab }) {
  const query = useQuery({
    queryKey: ['lab-status', row.instance_id],
    queryFn: () => labsApi.status(row.id, row.instance_id),
    refetchInterval: 2500,
  });
  if (query.isLoading) return <div className="py-2.5 text-[13px] text-muted">{t('Loading machines…')}</div>;
  if (query.error) return <div className="py-2.5 text-[13px] text-bad">{t('Failed to load instance machines')}</div>;
  return (
    <div className="mt-3.5 border-t border-line pt-2.5">
      <div className="mb-0.5 flex flex-wrap justify-between gap-2.5 text-[11.5px] font-semibold uppercase tracking-[.05em] text-muted"><span>{t('Machines of the instance')}</span><span>{t('CPU and RAM are measured per QEMU process')}</span></div>
      {!query.data?.machines.length ? <div className="py-2.5 text-[13px] text-muted">{t('No machines in the instance')}</div> : query.data.machines.map((machine) => (
        <MachineRow key={machine.rid} row={row} machine={machine} />
      ))}
    </div>
  );
}

function RunningCard({ row, onWipe, editorPath }: { row: RunningLab; onWipe: (row: RunningLab) => void; editorPath: (row: RunningLab) => string }) {
  const to = editorPath(row);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const isAdmin = useAuthStore((state) => state.user?.role === 'admin');
  const [open, setOpen] = useState(false);
  const [labelKey, cls] = STATUS[row.status] ?? [row.status, 'muted'];
  const label = t(labelKey as Parameters<typeof t>[0]);
  const busy = row.status === 'busy';
  const canStart = row.can_start && CAN_START.includes(row.status);
  const canStop = row.can_stop && ['running', 'partial', 'error', 'stale'].includes(row.status);
  const canRestart = row.own && row.can_start && row.can_stop && row.running > 0 && !busy;
  const share = row.host_cpus && row.cpu_ready ? Math.min(100, row.cpu_pct / row.host_cpus) : 0;
  const act = (start: boolean) => void guard(async () => {
    await (start ? labsApi.startAll(row.id, row.instance_id) : labsApi.stopAll(row.id, row.instance_id));
    await queryClient.invalidateQueries({ queryKey: ['running'] });
  });
  const restart = () => {
    if (!confirm(`${t('Restart')} (${row.name}, ${row.instance_user})?`)) return;
    void guard(async () => {
      const status = await labsApi.status(row.id, row.instance_id);
      for (const machine of status.machines.filter((item) => item.running)) await devicesApi.action(machine.rid, 'restart');
      await queryClient.invalidateQueries({ queryKey: ['running'] });
    });
  };
  const parts = [
    ['ok', row.running - row.stale], ['stale', row.stale], ['busy', row.busy],
    ['error', row.errors], ['off', row.devices - row.running - row.busy - row.errors],
  ] as const;
  return (
    <article className={`relative overflow-hidden rounded-app border border-line bg-panel py-3.5 pl-5 pr-[18px] shadow-app transition-colors before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-line-2 before:content-[''] hover:border-line-2 ${cardTone[cls] ?? ''}`}>
      <header className="flex items-start justify-between gap-2.5">
        <div className="min-w-0 break-words text-base font-bold [&>a]:text-text [&>a]:no-underline [&>a:hover]:text-accent"><Link to={to}>{row.name}</Link><span className="text-[12.5px] font-medium text-muted"> #{row.id}</span></div>
        <span className={`inline-flex flex-none items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-[3px] text-xs font-semibold ${badgeTone[cls] ?? badgeTone['muted']}`}><i className="size-[7px] rounded-full bg-current" />{label}</span>
      </header>
      <div className="mt-2 flex flex-wrap gap-x-3.5 gap-y-1 text-[13px] text-muted">
        <span>{t('Owner:')} <b className="font-semibold text-text">{row.owner}</b>{row.owner_id === useAuthStore.getState().user?.id ? <span className="ml-1.5 inline-block rounded bg-accent-soft px-1.5 py-px text-[10.5px] font-semibold uppercase tracking-[.05em] text-accent">{t('You')}</span> : null}{row.owner_role === 'admin' ? <span className="ml-1.5 inline-block rounded bg-accent-soft px-1.5 py-px text-[10.5px] font-semibold uppercase tracking-[.05em] text-accent">{t('Administrator')}</span> : null}</span>
        <span>{t('Started')}: <b className="font-semibold text-text">{row.instance_user}</b>{row.mine ? <span className="ml-1.5 inline-block rounded bg-accent-soft px-1.5 py-px text-[10.5px] font-semibold uppercase tracking-[.05em] text-accent">{t('You')}</span> : null}</span>
      </div>
      <div className="mt-[3px] text-[12.5px] text-muted">{row.started_at ? `${fmtTime(row.started_at)} · ${fmtUptime(Math.max(0, Date.now() / 1000 - row.started_at))}` : t('unknown start time')}</div>
      <div className="mt-3.5">
        <div className="mb-[7px] flex flex-wrap items-center gap-2.5 text-[13px] text-muted"><span>{t('Machines')}</span><b className="tabular-nums text-text">{row.running} {t('of')} {row.devices}</b>
          {row.errors > 0 && <span className="text-[12.5px] text-bad">{t('errors:')} {row.errors}</span>}
          {row.stale > 0 && <span className="text-[12.5px] text-bad">{t('without process:')} {row.stale}</span>}
          {row.busy > 0 && <span className="text-[12.5px] text-warn">{t('transition:')} {row.busy}</span>}
        </div>
        <div className="flex h-2 gap-0.5 overflow-hidden rounded-full bg-panel3" title={`${t('machines')}: ${row.devices}`}>
          {parts.filter(([, count]) => count > 0).map(([className, count]) => <i key={className} className={`block h-full min-w-1 rounded-full ${segmentTone[className]}`} style={{ width: `${count / Math.max(1, row.devices) * 100}%` }} />)}
        </div>
      </div>
      <div className="mt-3.5 grid grid-cols-2 gap-3.5 border-t border-line pt-3">
        <div><div className="text-[11px] font-semibold uppercase tracking-[.06em] text-muted">CPU</div><div className="mt-0.5 text-sm font-semibold tabular-nums">{row.running && row.cpu_ready ? `${fmtNum(Number((row.cpu_pct / 100).toFixed(2)))} ${t('cores')} · ${fmtPct(share)} ${t('all host cores')}` : row.running ? t('Loading…') : '—'}</div>
          <div className="mt-[7px] h-1.5 overflow-hidden rounded-full bg-panel3"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${share}%` }} /></div></div>
        <div><div className="text-[11px] font-semibold uppercase tracking-[.06em] text-muted">{t('RAM')}</div><div className="mt-0.5 text-sm font-semibold tabular-nums">{row.running ? fmtBytes(row.ram_bytes) : '—'}</div><div className="mt-1 text-xs text-muted">{row.running ? t('QEMU processes') : t('machines are stopped')}</div></div>
      </div>
      <div className={`mt-3.5 ${acts}`}>
        {row.can_open && <button type="button" className={primaryBtn} onClick={() => navigate(to)}><Icon name="open" size={15} />{t('Open')}</button>}
        {canStart && <button type="button" onClick={() => act(true)}><Icon name="play" size={15} />{t('Start')}</button>}
        {canStop && <button type="button" onClick={() => act(false)}><Icon name="stop" size={15} />{t('Stop')}</button>}
        {canRestart && <button type="button" onClick={restart}><Icon name="restart" size={15} />{t('Restart')}</button>}
        {(row.own || isAdmin) && <button type="button" onClick={() => setOpen((value) => !value)}><Icon name={open ? 'up' : 'down'} size={15} />{open ? t('Hide machines') : t('Machines')}</button>}
        {row.can_wipe && !busy && <button type="button" className={dangerBtn} onClick={() => onWipe(row)}><Icon name="trash" size={15} />{t('Wipe')}</button>}
      </div>
      {open && <MachinesPanel row={row} />}
    </article>
  );
}

export function RunningPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [owner, setOwner] = useState('');
  const [filter, setFilter] = useState('all');
  const [wipe, setWipe] = useState<RunningLab | null>(null);
  useLanguageStore((state) => state.language);
  const query = useQuery({ queryKey: ['running'], queryFn: () => labsApi.running(), refetchInterval: 4000 });
  // The editor URL is built from the lab's folder: `/labs/<folder>/<lab>`.
  const folders = useQuery({ queryKey: ['folders'], queryFn: foldersApi.list, staleTime: 60_000 });
  const labs = useQuery({ queryKey: ['labs'], queryFn: () => labsApi.list(), staleTime: 60_000 });
  const editorPath = (row: RunningLab) => {
    const lab = (labs.data ?? []).find((item) => item.id === row.id);
    const folderId = lab?.folder_id ?? 0;
    return labPath({ name: row.name, folder_id: folderId }, folderNameOf(folders.data ?? [], folderId), row.instance_id);
  };
  const rows = query.data ?? [];
  const owners = useMemo(() => [...new Set(rows.map((row) => row.owner))].sort((a, b) => a.localeCompare(b, 'ru')), [rows]);
  const filtered = rows.filter((row) => {
    if (search && !row.name.toLowerCase().includes(search.toLowerCase())) return false;
    if (owner && row.owner !== owner) return false;
    if (filter === 'running') return row.status === 'running' || row.status === 'partial';
    if (filter === 'stopped') return row.status === 'stopped';
    if (filter === 'wiped') return row.status === 'wiped';
    if (filter === 'partial') return row.status === 'partial';
    if (filter === 'problem') return PROBLEM.includes(row.status);
    if (filter === 'mine') return row.mine;
    return true;
  });
  const sum = (key: keyof RunningLab) => filtered.reduce((total, row) => total + (typeof row[key] === 'number' ? row[key] : 0), 0);
  const problems = filtered.filter((row) => row.status === 'error' || row.status === 'stale').length;
  const cpuReady = filtered.some((row) => row.cpu_ready);
  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{t('Failed to load running laboratories')}</div>;
  return (
    <>
      <div className="mb-[18px] flex flex-wrap items-end justify-between gap-3.5">
        <div><h2>{t('Running laboratories')}</h2><div className="text-[13.5px] text-muted">{t('Instances of laboratories with running machines. The list refreshes every 4 seconds.')}</div></div>
        <span className="inline-flex items-center gap-2 whitespace-nowrap text-[12.5px] text-muted"><i className="size-2 rounded-full bg-ok motion-safe:animate-[rlPulse_2s_infinite]" />{t('updated')} {new Date().toLocaleTimeString()}</span>
      </div>
      <div className="mb-4 flex flex-wrap items-center gap-2.5 [&>input[type=search]]:min-w-[200px] [&>input[type=search]]:max-w-[380px] [&>input[type=search]]:flex-1 [&>select]:min-w-[170px] max-sm:[&>input[type=search]]:w-full max-sm:[&>input[type=search]]:max-w-none max-sm:[&>select]:w-full">
        <input type="search" placeholder={t('Search by laboratory name…')} value={search} onChange={(event) => setSearch(event.target.value)} />
        <Dropdown className="w-full max-w-[240px]" value={owner} onChange={setOwner} options={[
          { value: '', label: t('Owner') },
          ...owners.map((name) => ({ value: name, label: name })),
        ]} />
        <div className="flex flex-wrap gap-1.5">
          {FILTERS.map(([id, label]) => {
            const count = rows.filter((row) => id === 'all' || (id === 'problem' ? PROBLEM.includes(row.status) : id === 'mine' ? row.mine : row.status === id)).length;
            return <button key={id} type="button" className={`min-h-[34px] rounded-full border px-3 py-[5px] text-[13px] ${filter === id ? 'border-transparent bg-accent-soft font-semibold text-text' : 'border-line bg-panel text-muted'}`} aria-pressed={filter === id} onClick={() => setFilter(id)}>{t(label as Parameters<typeof t>[0])}<b className="ml-1.5 font-semibold tabular-nums opacity-80">{count}</b></button>;
          })}
        </div>
      </div>
      <div className="mb-[18px] grid grid-cols-[repeat(auto-fill,minmax(170px,1fr))] gap-3 max-sm:grid-cols-2">
        <div className={kpi}><div className="text-[11.5px] font-semibold uppercase tracking-[.06em] text-muted">{t('Laboratories')}</div><div className="mt-[3px] text-2xl font-bold tracking-[-.02em] tabular-nums max-sm:text-xl">{new Set(filtered.map((row) => row.id)).size}</div><div className="mt-px text-xs text-muted">{filtered.length} {t('instances')}</div></div>
        <div className={kpi}><div className="text-[11.5px] font-semibold uppercase tracking-[.06em] text-muted">{t('Machines running')}</div><div className="mt-[3px] text-2xl font-bold tracking-[-.02em] tabular-nums max-sm:text-xl">{sum('running')}</div><div className="mt-px text-xs text-muted">{t('of')} {sum('devices')}</div></div>
        <div className={kpi}><div className="text-[11.5px] font-semibold uppercase tracking-[.06em] text-muted">{t('CPU load')}</div><div className="mt-[3px] text-2xl font-bold tracking-[-.02em] tabular-nums max-sm:text-xl">{cpuReady ? fmtNum(Number((sum('cpu_pct') / 100).toFixed(2))) : '—'}</div><div className="mt-px text-xs text-muted">{t('cores')}</div></div>
        <div className={kpi}><div className="text-[11.5px] font-semibold uppercase tracking-[.06em] text-muted">{t('Process RAM')}</div><div className="mt-[3px] text-2xl font-bold tracking-[-.02em] tabular-nums max-sm:text-xl">{fmtBytes(sum('ram_bytes'))}</div><div className="mt-px text-xs text-muted">{t('QEMU processes')}</div></div>
        <div className={`${kpi} ${problems ? 'before:bg-bad' : ''}`}><div className="k">{t('Problems')}</div><div className="mt-[3px] text-2xl font-bold tracking-[-.02em] tabular-nums max-sm:text-xl">{problems}</div><div className="mt-px text-xs text-muted">{problems ? t('needs attention') : t('no errors')}</div></div>
      </div>
      {filtered.length ? <div className="grid grid-cols-[repeat(auto-fill,minmax(540px,1fr))] items-start gap-4 max-sm:grid-cols-1">{filtered.map((row) => <RunningCard key={row.instance_id} row={row} onWipe={setWipe} editorPath={editorPath} />)}</div> : (
        <EmptyState title={t('Nothing is running')} text={t('Start a laboratory and it will appear here.')}
          action={<button type="button" className={primaryBtn} onClick={() => navigate(paths.labs)}>{t('Back to laboratories')}</button>} />
      )}
      {wipe && <WipeLabDialog lab={{ id: wipe.id, name: wipe.name }} instance={wipe.instance_id}
        onClose={() => setWipe(null)} onDone={() => { void queryClient.invalidateQueries({ queryKey: ['running'] }); }} />}
    </>
  );
}
