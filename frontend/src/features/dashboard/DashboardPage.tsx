import { useQuery } from '@tanstack/react-query';
import { systemApi } from '../../api/endpoints';
import { fmtBytes, fmtUptime, levelOf } from '../../lib/format';
import { Spinner } from '../../components/ui/Spinner';
import { t, useLanguageStore } from '../../i18n';

const cardClass = "relative overflow-hidden rounded-app border border-line bg-panel px-[18px] pb-4 pt-[18px] shadow-app transition-[transform,border-color] duration-200 before:absolute before:inset-x-0 before:top-0 before:h-[3px] before:bg-gradient-to-r before:from-accent before:to-accent-2 before:opacity-85 before:content-[''] hover:-translate-y-0.5 hover:border-line-2";
const boxClass = 'mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5';
const boxTitleClass = 'mb-3 mt-0 text-[13px] font-semibold uppercase tracking-[.06em] text-muted';
const barTone: Record<'ok' | 'warn' | 'bad', string> = {
  ok: 'bg-gradient-to-r from-accent to-accent-2',
  warn: 'bg-gradient-to-r from-warn to-orange-500',
  bad: 'bg-gradient-to-r from-bad to-red-500',
};

export function DashboardPage() {
  const system = useQuery({ queryKey: ['system'], queryFn: systemApi.info });
  useLanguageStore((state) => state.language);
  const live = useQuery({ queryKey: ['system-live'], queryFn: systemApi.live, refetchInterval: 2500 });
  if (system.isLoading) return <Spinner />;
  if (system.error) return <div className="py-2 text-bad">{system.error.message}</div>;
  const data = system.data;
  if (!data) return null;
  const memUsed = data.mem_total - data.mem_avail;
  const diskUsed = data.disk_total - data.disk_free;
  // The memory saving is the sum of both mechanisms; only one is active at a time.
  const saved = { bytes: (data.ksm_saved ?? 0) + (data.uksm_saved ?? 0), kind: data.uksm_enabled ? 'UKSM' : data.ksm_enabled ? 'KSM' : '' };
  const cpu = live.data;
  const cpuPct = Math.max(0, Math.min(100, cpu?.cpu_percent ?? 0));
  return (
    <>
      <h2>{t('Dashboard')}</h2>
      <div className="mb-[22px] grid grid-cols-[repeat(auto-fill,minmax(190px,1fr))] gap-3.5 max-sm:grid-cols-2">
        {[
          [t('Laboratories'), data.counts.labs],
          [t('Device'), `${data.counts.devices_running} / ${data.counts.devices}`],
          [t('Images'), data.counts.images],
          [t('Users'), data.counts.users],
        ].map(([title, value]) => (
          <div className={cardClass} key={String(title)}>
            <div className="text-[28px] font-bold tracking-[-.02em] tabular-nums max-sm:text-2xl">{value}</div>
            <div className="mt-0.5 text-[12.5px] text-muted">{title}</div>
          </div>
        ))}
        <div className={cardClass}>
          <div className="mb-1.5 flex items-center gap-2">
            <span className="size-[7px] flex-none rounded-full bg-ok motion-safe:animate-[qlPulse_2s_infinite]" />
            <span className="mt-0 text-[12.5px] text-muted">{t('Running machines')}</span>
          </div>
          <div className="text-[28px] font-bold tracking-[-.02em] tabular-nums max-sm:text-2xl">{cpu?.running_vms ?? '—'}</div>
          <div className="mt-1.5 text-[11.5px] text-muted">{cpu?.running_vms ? t('all laboratories and users') : t('nothing is running now')}</div>
        </div>
        <div className={cardClass} data-level={levelOf(cpuPct)}>
          <div className="mb-1.5 flex items-center gap-2">
            <span className={`size-[7px] flex-none rounded-full motion-safe:animate-[qlPulse_2s_infinite] ${levelOf(cpuPct) === 'bad' ? 'bg-bad' : levelOf(cpuPct) === 'warn' ? 'bg-warn' : 'bg-ok'}`} />
            <span className="mt-0 text-[12.5px] text-muted">{t('Total CPU load')}</span>
          </div>
          <div className="text-[28px] font-bold tracking-[-.02em] tabular-nums max-sm:text-2xl">{cpu?.cpu_ready ? cpuPct.toFixed(1) + '%' : '—'}</div>
          <div className="my-0 mb-0 mt-2.5 h-[7px] overflow-hidden rounded-full bg-panel3"><i className={`block h-full rounded-full transition-[width] duration-500 ${barTone[levelOf(cpuPct)]}`} style={{ width: `${cpuPct}%` }} /></div>
          <div className="mt-1.5 text-[11.5px] text-muted">{cpu ? `${cpu.cpus} ${t('cores')} · ${t('updated')} ${new Date().toLocaleTimeString()}` : t('Loading…')}</div>
        </div>
      </div>
      <div className={boxClass}>
        <h3 className={boxTitleClass}>{t('Host resources')}</h3>
        <div>{t('Memory:')} {fmtBytes(memUsed)} / {fmtBytes(data.mem_total)}</div>
        <div className="my-1.5 h-[7px] overflow-hidden rounded-full bg-panel3"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${data.mem_total ? memUsed / data.mem_total * 100 : 0}%` }} /></div>
        <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-app-sm border border-line bg-bg-2 px-3 py-2.5 text-[13px]">
          <span className="text-muted">{t('Saved by page merging')}</span>
          <b className="tabular-nums text-ok">{saved.bytes > 0 || saved.kind ? fmtBytes(saved.bytes) : '—'}</b>
          <span className="text-xs text-muted">{saved.kind || t('Page merging is not available on this host')}</span>
        </div>
        <div className="mt-3">{t('Disk')} ({data.data_dir}): {fmtBytes(data.disk_free)} / {fmtBytes(data.disk_total)}</div>
        <div className="my-1.5 h-[7px] overflow-hidden rounded-full bg-panel3"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${data.disk_total ? diskUsed / data.disk_total * 100 : 0}%` }} /></div>
        <div className="text-muted">{t('Version')} {data.version} · {t('uptime')} {fmtUptime(data.uptime_sec)} · {data.listen_addr}</div>
      </div>
      <div className={boxClass}>
        <h3 className={boxTitleClass}>{t('Environment checks')}</h3>
        <table><tbody>
          {data.checks.map((check) => (
            <tr key={check.name}>
              <td className={check.ok ? 'text-ok' : 'text-bad'}>{check.ok ? '✔' : '✖'}</td>
              <td>{check.name}</td><td className="text-muted">{check.detail}</td>
            </tr>
          ))}
        </tbody></table>
      </div>
    </>
  );
}
