import { useEffect, useRef, useState } from 'react';
import { iconsApi } from '../../api/endpoints';
import type { IconItem } from '../../types/api';
import { iconFor, STANDARD_ICONS } from '../../lib/deviceIcon';
import { validateIcon } from '../icons/validate';
import { toast } from '../../stores/toast';
import { t } from '../../i18n';

export interface IconSelection {
  icon: string;
  custom_icon: string;
}

const checker = 'bg-[repeating-conic-gradient(rgba(128,128,128,.22)_0_25%,transparent_0_50%)] [background-size:16px_16px]';
const choice = 'flex min-h-0 flex-none flex-col items-center gap-0.5 rounded-lg border-2 border-transparent bg-transparent p-1.5 text-xs text-text hover:border-line';
const hint = 'mt-2.5 text-xs leading-relaxed text-muted';

const VISIBLE_STANDARD = STANDARD_ICONS.filter((item) => item.key !== '');

export function IconPicker({ value, onChange, type = 'generic', customUrl = '' }: {
  value: IconSelection;
  onChange: (value: IconSelection) => void;
  type?: string;
  customUrl?: string;
}) {
  const [items, setItems] = useState<IconItem[] | null>(null);
  const [preview, setPreview] = useState(customUrl);
  // A new picture (e.g. from a second template) must change the preview instead of keeping the old one.
  useEffect(() => { setPreview(customUrl); }, [customUrl]);
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = () => iconsApi.list().then(setItems).catch(() => setItems([]));
  useEffect(() => { void load(); }, []);

  const selectCustom = (item: IconItem) => {
    setPreview(item.url);
    onChange({ icon: 'custom', custom_icon: item.hash });
  };

  const upload = async (file: File) => {
    setBusy(true);
    try {
      await validateIcon(file);
      const result = await iconsApi.upload(file, file.name.replace(/\.[^.]+$/, ''));
      setPreview(result.url);
      onChange({ icon: 'custom', custom_icon: result.hash });
      await load();
      toast.success(result.created ? t('Icon uploaded to the library') : t('This icon already exists and was selected'));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };

  const shownPreview = value.icon === 'custom' && value.custom_icon ? preview : iconFor(value.icon || type);
  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      <div className="flex flex-wrap items-center gap-4">
        <div className="flex min-w-[112px] flex-col items-center gap-1.5">
          <img className={`${checker} size-24 rounded-[10px] border border-line object-contain p-1.5`} src={shownPreview} alt="" />
          <div className="text-center text-xs text-muted">{value.icon === 'custom' ? t('Custom') : value.icon === '' ? t('Auto') : `${t('Standard:')} ${t((STANDARD_ICONS.find((item) => item.key === value.icon)?.label ?? 'Auto') as Parameters<typeof t>[0])}`}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <button type="button" className={`${choice}${value.icon === '' ? ' border-accent' : ''}`}
            title={t('Auto')} onClick={() => { onChange({ icon: '', custom_icon: '' }); setPreview(''); }}>
            <img className={`${checker} size-12 object-contain p-1`} src={iconFor(type)} alt="" width={48} height={48} /><span>{t('Auto')}</span>
          </button>
          {VISIBLE_STANDARD.map((item) => (
            <button key={item.key} type="button" className={`${choice}${value.icon === item.key ? ' border-accent' : ''}`}
              title={t(item.label as Parameters<typeof t>[0])} onClick={() => { onChange({ icon: item.key, custom_icon: '' }); setPreview(''); }}>
              <img className={`${checker} size-12 object-contain p-1`} src={item.src} alt="" width={48} height={48} /><span>{t(item.label as Parameters<typeof t>[0])}</span>
            </button>
          ))}
          <button type="button" className={`${choice}${value.icon === 'custom' ? ' border-accent' : ''}`} title={t('Custom')}
            onClick={() => { if (value.custom_icon) setPreview(preview || customUrl); else fileRef.current?.click(); }}>
            {value.custom_icon ? <img className={`${checker} size-12 object-contain p-1`} src={preview || customUrl} alt="" width={48} height={48} /> : <span className="grid size-12 place-items-center text-[26px] leading-none text-muted">+</span>}
            <span>{t('Custom')}</span>
          </button>
        </div>
      </div>
      <div className="flex flex-wrap gap-2">
        <button type="button" disabled={busy} onClick={() => fileRef.current?.click()}>{t('Upload image')}</button>
        {value.custom_icon && <button type="button" disabled={busy} onClick={() => { onChange({ icon: '', custom_icon: '' }); setPreview(''); }}>{t('Delete')}</button>}
      </div>
      <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml,.png,.jpg,.jpeg,.webp,.svg" hidden onChange={(event) => {
        const file = event.target.files?.[0]; event.target.value = '';
        if (file) void upload(file);
      }} />
      <div className="mt-1 text-xs text-muted">{t('Icons')}</div>
      {items === null ? <div className={hint}>{t('Loading…')}</div> : items.length ? (
        <div className="grid max-h-60 grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-2 overflow-auto p-0.5">{items.map((item) => (
          <button key={item.id} type="button" className={`${choice} w-full min-w-0${value.icon === 'custom' && value.custom_icon === item.hash ? ' border-accent' : ''}`}
            title={item.name} onClick={() => selectCustom(item)}>
            <img className={`${checker} size-12 object-contain p-1`} src={item.url} alt="" width={48} height={48} /><span className="block max-w-full overflow-hidden text-ellipsis whitespace-nowrap">{item.name}</span>
          </button>
        ))}</div>
      ) : <div className={hint}>{t('Icon library is empty')}</div>}
      <div className={hint}>{t('PNG, JPEG, WEBP or SVG')} · 512 KB · 16–512 px</div>
    </div>
  );
}
