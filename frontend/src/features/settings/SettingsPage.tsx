import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ksmApi, settingsApi } from '../../api/endpoints';
import type { PageMergingStatus } from '../../api/endpoints';
import { guard, toast } from '../../stores/toast';
import { fmtBytes } from '../../lib/format';
import { Spinner } from '../../components/ui/Spinner';
import { Switch } from '../../components/ui/Switch';
import { t, useLanguageStore } from '../../i18n';

const card = 'mb-[18px] max-w-[560px] rounded-app border border-line bg-panel p-5 shadow-app';
const primarySave = 'border-transparent bg-gradient-to-r from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';

export function SettingsPage() {
  const query = useQuery({ queryKey: ['settings'], queryFn: settingsApi.get });
  const merging = useQuery({ queryKey: ['ksm'], queryFn: ksmApi.get });
  const queryClient = useQueryClient();
  useLanguageStore((state) => state.language);
  const [ttl, setTtl] = useState('');
  const [upload, setUpload] = useState('');
  useEffect(() => {
    if (query.data) {
      setTtl(query.data.session_ttl_hours);
      setUpload(query.data.max_upload_mb);
    }
  }, [query.data]);

  // Enabling one mechanism on the server disables the other: take the state from the response.
  const toggle = (kind: 'ksm' | 'uksm', enabled: boolean) => void guard(async () => {
    const status = await ksmApi.set(kind, enabled);
    queryClient.setQueryData(['ksm'], status);
    void queryClient.invalidateQueries({ queryKey: ['system'] });
    toast.success(enabled ? `${kind.toUpperCase()} ${t('enabled')}` : `${kind.toUpperCase()} ${t('disabled')}`);
  });

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;
  const status: PageMergingStatus | undefined = merging.data;
  const row = (kind: 'ksm' | 'uksm', label: string, help: string) => {
    const item = status?.[kind];
    return (
      <div className="rounded-app-sm border border-line bg-bg-2 p-3.5" data-mechanism={kind} data-testid={`mechanism-${kind}`}>
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div className="min-w-0">
            <b className="text-[14px]">{label}</b>
            <div className="text-xs leading-relaxed text-muted">{help}</div>
          </div>
          {item?.available ? (
            <Switch checked={item.enabled} label={item.enabled ? t('On') : t('Off')} onChange={(value) => toggle(kind, value)} />
          ) : <span className="text-xs text-muted">{t('Unavailable')}</span>}
        </div>
        <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted">
          {item?.available && item.enabled && <span>{t('Saved by page merging')}: <b className="tabular-nums text-ok">{fmtBytes(item.saved_bytes)}</b></span>}
          {item?.reason && <span>{item.reason}</span>}
        </div>
      </div>
    );
  };

  return (
    <>
      <h2>{t('System settings')}</h2>
      <div className={card}>
        <div className="mb-4">
          <label className="mt-0" htmlFor="settings-session-ttl">{t('Session lifetime, hours')}</label>
          <input id="settings-session-ttl" type="number" min={1} max={720} value={ttl} onChange={(event) => setTtl(event.target.value)} />
        </div>
        <div className="mb-4">
          <label className="mt-0" htmlFor="settings-upload-max">{t('Maximum image upload size, MB')}</label>
          <input id="settings-upload-max" type="number" min={1} value={upload} onChange={(event) => setUpload(event.target.value)} />
        </div>
        <div>
          <button type="button" className={primarySave} onClick={() => void guard(async () => {
            await settingsApi.put({ session_ttl_hours: ttl, max_upload_mb: upload });
            toast.success(t('Settings are saved'));
          })}>{t('Save')}</button>
        </div>
      </div>
      <div className={card}>
        <h3 className="mb-1.5 mt-0 text-[13px] font-semibold uppercase tracking-[.06em] text-muted">{t('Page merging (KSM/UKSM)')}</h3>
        <div className="text-xs leading-relaxed text-muted">{t('Memory page merging: identical pages of virtual machines share one physical copy, saving RAM.')}</div>
        <div className="text-xs leading-relaxed text-warn">{t('KSM and UKSM solve the same task: enabling one turns the other off automatically.')}</div>
        <div className="mt-3 flex flex-col gap-2.5">
          {merging.isLoading ? <Spinner /> : (
            <>
              {row('ksm', 'KSM', t('Standard kernel page merging: always available, uses some CPU.'))}
              {row('uksm', 'UKSM', t('Page merging with low CPU load; requires UKSM support in the kernel.'))}
            </>
          )}
        </div>
      </div>
    </>
  );
}
