import { useEffect, useState } from 'react';
import { labsApi } from '../../api/endpoints';
import type { LabStatus } from '../../types/api';
import { fmtDisk, fmtGB, fmtNum, fmtPct, levelOf } from '../../lib/format';
import { deviceIconSrc } from '../../lib/deviceIcon';
import { Modal } from '../../components/ui/Modal';
import { t } from '../../i18n';

const card = "relative overflow-hidden rounded-app border border-line bg-panel px-[18px] pb-4 pt-[18px] shadow-app before:absolute before:inset-x-0 before:top-0 before:h-[3px] before:bg-gradient-to-r before:from-accent before:to-accent-2 before:opacity-85 before:content-['']";
const pillTone = "data-[st=running]:bg-ok/14 data-[st=running]:text-ok data-[st=paused]:bg-warn/14 data-[st=paused]:text-warn data-[st=starting]:bg-accent-2/14 data-[st=starting]:text-accent-2 data-[st=stopping]:bg-accent-2/14 data-[st=stopping]:text-accent-2 data-[st=error]:bg-bad/14 data-[st=error]:text-bad";

const LABELS: Record<string, string> = { running: t('Running'), paused: t('Paused'), starting: t('Starting'), stopping: t('Stopping'), stopped: t('Stopped'), error: t('Error') };

export function StatusDialog({ labId, labName, instance, onClose }: { labId: string; labName: string; instance?: number | undefined; onClose: () => void }) {
  const [status, setStatus] = useState<LabStatus | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let stopped = false;
    let timer = 0;
    let busy = false;
    const tick = async () => {
      if (stopped) return;
      if (busy) return;
      busy = true;
      try {
        const value = await labsApi.status(labId, instance);
        if (!stopped) { setStatus(value); setError(''); }
      } catch (cause) {
        if (!stopped) setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        busy = false;
        if (!stopped) timer = window.setTimeout(() => void tick(), 2500);
      }
    };
    void tick();
    return () => { stopped = true; window.clearTimeout(timer); };
  }, [labId, instance]);
  const totals = status?.totals;
  const cpus = status?.host_cpus || 1;
  const cpuShare = totals ? Math.min(100, totals.cpu_pct / cpus) : 0;
  const ramShare = totals && status?.host_ram_bytes ? Math.min(100, totals.ram_bytes / status.host_ram_bytes * 100) : 0;
  return (
    <Modal title={t('Status')} onClose={onClose} wide actions={[{ label: t('Close'), onClick: onClose }]}>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2.5 text-[13px] text-muted"><span className="size-[7px] flex-none rounded-full bg-ok motion-safe:animate-[qlPulse_2s_infinite]" /><span>{t('Laboratory')} «{labName}» · {t('Only your instance')}</span><span className="flex-1" />
          <span className="text-muted">{status ? `${t('updated')} ${new Date().toLocaleTimeString()}` : t('Loading…')}</span></div>
        {error && <div className="rounded-app-sm border border-bad bg-bad/[.08] px-3 py-2 text-[13px] text-bad">{`${t('No data')}: ${error}`}</div>}
        <div className="mb-3 grid grid-cols-2 gap-2.5 max-sm:grid-cols-1">
          <div className={`grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1.5 rounded-app-sm border border-line bg-panel2 px-3.5 py-3 [grid-template-areas:'k_v'_'t_t'] data-[level=warn]:[&_.mon-track_i]:bg-gradient-to-r data-[level=warn]:[&_.mon-track_i]:from-warn data-[level=warn]:[&_.mon-track_i]:to-orange-500 data-[level=bad]:[&_.mon-track_i]:bg-gradient-to-r data-[level=bad]:[&_.mon-track_i]:from-bad data-[level=bad]:[&_.mon-track_i]:to-red-500 data-[level=bad]:[&_.mon-v]:text-bad data-[level=warn]:[&_.mon-v]:text-warn`} data-level={levelOf(cpuShare)}><span className="[grid-area:k] text-[11px] font-bold uppercase tracking-[.08em] text-muted">CPU</span><div className="min-w-0 justify-self-end text-right [grid-area:v]"><b className="block text-xl font-bold tracking-[-.02em] tabular-nums text-text">{fmtPct(cpuShare)}</b><span className="block whitespace-nowrap text-[11.5px] tabular-nums text-muted">{fmtNum(Number((totals?.cpu_pct ?? 0) / 100))} {t('of')} {cpus} {t('cores')}</span></div><div className="h-1.5 overflow-hidden rounded-full bg-panel3 [grid-area:t]"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${cpuShare}%` }} /></div></div>
          <div className={`grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1.5 rounded-app-sm border border-line bg-panel2 px-3.5 py-3 [grid-template-areas:'k_v'_'t_t'] data-[level=warn]:[&_.mon-track_i]:bg-gradient-to-r data-[level=warn]:[&_.mon-track_i]:from-warn data-[level=warn]:[&_.mon-track_i]:to-orange-500 data-[level=bad]:[&_.mon-track_i]:bg-gradient-to-r data-[level=bad]:[&_.mon-track_i]:from-bad data-[level=bad]:[&_.mon-track_i]:to-red-500 data-[level=bad]:[&_.mon-v]:text-bad data-[level=warn]:[&_.mon-v]:text-warn`} data-level={levelOf(ramShare)}><span className="[grid-area:k] text-[11px] font-bold uppercase tracking-[.08em] text-muted">{t('RAM')}</span><div className="min-w-0 justify-self-end text-right [grid-area:v]"><b className="block text-xl font-bold tracking-[-.02em] tabular-nums text-text">{fmtPct(ramShare)}</b><span className="block whitespace-nowrap text-[11.5px] tabular-nums text-muted">{fmtGB(totals?.ram_bytes ?? 0)} {t('of')} {fmtGB(status?.host_ram_bytes ?? 0)} {t('GB')}</span></div><div className="h-1.5 overflow-hidden rounded-full bg-panel3 [grid-area:t]"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${ramShare}%` }} /></div></div>
        </div>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-3 max-sm:grid-cols-2">
          <div className={card}><div className="mb-1.5 flex items-center gap-2"><span className="size-[7px] flex-none rounded-full bg-ok motion-safe:animate-[qlPulse_2s_infinite]" /><span className="text-[12.5px] text-muted">{t('Machines running')}</span></div><div className="text-2xl font-bold tracking-[-.02em] tabular-nums">{totals ? `${totals.running} ${t('of')} ${totals.machines}` : '—'}</div><div className="mt-1.5 text-[11.5px] text-muted">{totals?.machines ? t('Remaining machines are stopped') : t('No machines')}</div></div>
          <div className={card}><div className="mb-1.5 flex items-center gap-2"><span className="size-[7px] flex-none rounded-full bg-ok motion-safe:animate-[qlPulse_2s_infinite]" /><span className="text-[12.5px] text-muted">{t('Disk, total')}</span></div><div className="text-2xl font-bold tracking-[-.02em] tabular-nums">{fmtDisk(totals?.disk_bytes ?? 0)}</div><div className="mt-1.5 text-[11.5px] text-muted">{t('Occupied by all machines')}</div></div>
        </div>
        <div className="overflow-x-auto rounded-app-sm border border-line bg-panel2">
          <table className="w-full min-w-[640px] border-separate border-spacing-0 text-[13px] [&_th]:sticky [&_th]:top-0 [&_th]:border-b [&_th]:border-line [&_th]:bg-panel2 [&_th]:px-3 [&_th]:py-2.5 [&_th]:text-left [&_th]:text-[11px] [&_th]:font-semibold [&_th]:uppercase [&_th]:tracking-[.07em] [&_th]:text-muted [&_td]:whitespace-nowrap [&_td]:border-t [&_td]:border-line [&_td]:px-3 [&_td]:py-[11px] [&_td]:tabular-nums [&_tr:first-child_td]:border-t-0 [&_tbody_tr]:transition-colors [&_tbody_tr:hover]:bg-accent-soft"><thead><tr>{[t('Machine'), t('Status'), 'CPU', t('RAM'), t('Disk'), 'PID'].map((title) => <th key={title}>{title}</th>)}</tr></thead>
            <tbody>
              {(status?.machines ?? []).map((machine) => {
                const share = Math.min(100, machine.cpu_pct / cpus);
                return (
                  <tr key={machine.rid}>
                    <td><div className="flex items-center gap-2.5"><img className="size-7 flex-none rounded-md object-contain" src={deviceIconSrc({ icon_url: machine.icon_url, type: machine.type })} alt="" width={28} height={28} /><span><b className="block font-semibold text-text">{machine.name}</b><small className="block text-[11.5px] text-muted">{machine.type}</small></span></div></td>
                    <td><span className={`inline-flex items-center gap-1.5 rounded-full bg-panel3 px-2.5 py-[3px] text-xs font-semibold text-muted before:size-[7px] before:rounded-full before:bg-current before:content-[''] ${pillTone}`} data-st={machine.state}>{LABELS[machine.state] ?? machine.state}</span></td>
                    <td><div className="flex min-w-[170px] items-center gap-2.5"><div className={`my-0 h-1.5 flex-1 overflow-hidden rounded-full ${levelOf(share) === "bad" ? "bg-panel3" : "bg-panel3"}`} data-level={levelOf(share)}><i className={`block h-full rounded-full ${levelOf(share) === "bad" ? "bg-gradient-to-r from-bad to-red-500" : levelOf(share) === "warn" ? "bg-gradient-to-r from-warn to-orange-500" : "bg-gradient-to-r from-accent to-accent-2"}`} style={{ width: `${share}%` }} /></div><span className="min-w-[86px] text-right">{machine.running ? (machine.cpu_ready ? fmtPct(share) : t('Calculating…')) : '—'}</span></div></td>
                    <td>{machine.running ? `${fmtGB(machine.ram_bytes)} ${t('GB')}` : '—'}</td>
                    <td>{fmtDisk(machine.disk_bytes)}</td>
                    <td className="text-muted">{machine.pid > 0 ? machine.pid : '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <div className="text-xs text-muted">{t('CPU is the share of all host cores. RAM is the resident memory of QEMU processes.')}</div>
      </div>
    </Modal>
  );
}
