import { useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { iconsApi } from '../../api/endpoints';
import type { IconItem } from '../../types/api';
import { guard, toast } from '../../stores/toast';
import { validateIcon } from './validate';
import { folderInputProps } from '../../lib/desktop';
import { RoundCheck } from '../../components/ui/RoundCheck';
import { Modal } from '../../components/ui/Modal';
import { EmptyState } from '../../components/ui/EmptyState';
import { Spinner } from '../../components/ui/Spinner';
import { t, useLanguageStore } from '../../i18n';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const checker = 'bg-[repeating-conic-gradient(rgba(128,128,128,.22)_0_25%,transparent_0_50%)] [background-size:16px_16px]';
const box = 'mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5';

export function IconsPage() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['icons'], queryFn: iconsApi.list });
  const fileRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState('');
  const [rename, setRename] = useState<IconItem | null>(null);
  useLanguageStore((state) => state.language);

  const items = query.data ?? [];
  const reload = () => void queryClient.invalidateQueries({ queryKey: ['icons'] });
  const toggle = (id: number, on: boolean) => {
    setSelected((current) => { const next = new Set(current); if (on) next.add(id); else next.delete(id); return next; });
  };
  const toggleAll = (on: boolean) => setSelected(on ? new Set(items.map((item) => item.id)) : new Set());

  const importFiles = async (files: File[]) => {
    const images = files.filter((file) => /\.(png|jpe?g|webp|svg)$/i.test(file.name) && !(file.webkitRelativePath || '').split('/').some((part) => part.startsWith('.')));
    if (!images.length) { setStatus(t('No image files selected')); return; }
    setBusy(true);
    let added = 0, existing = 0;
    const failed: string[] = [];
    for (let index = 0; index < images.length; index++) {
      const file = images[index]!;
      setStatus(`${t('Uploading')} ${index + 1} / ${images.length}: ${file.name}…`);
      try {
        await validateIcon(file);
        const result = await iconsApi.upload(file, file.name.replace(/\.[^.]+$/, ''));
        if (result.created) added++; else existing++;
      } catch (error) {
        failed.push(`${file.name} — ${error instanceof Error ? error.message : String(error)}`);
      }
    }
    await reload();
    setBusy(false);
    setStatus(`${t('Saved')}: ${added}, ${existing}${failed.length ? `, ${failed.length}` : ''}.`);
    failed.forEach((message) => toast.error(message));
  };

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;

  return (
    <>
      <h2>{t('Icons')}</h2>
      <div className="mt-2.5 text-xs leading-relaxed text-muted">{t('Machine icons: PNG, JPEG, WEBP or SVG. Choose an icon when adding or editing a machine.')}</div>
      <div className="mb-3.5 flex flex-wrap items-center gap-2.5">
        <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/webp,image/svg+xml,.png,.jpg,.jpeg,.webp,.svg" multiple hidden onChange={(event) => { const files = [...(event.target.files ?? [])]; event.target.value = ''; void importFiles(files); }} />
        <input ref={folderRef} type="file" multiple hidden {...folderInputProps} onChange={(event) => { const files = [...(event.target.files ?? [])]; event.target.value = ''; void importFiles(files); }} />
        <button type="button" className={primary} disabled={busy} onClick={() => fileRef.current?.click()}>{t('Upload image')}</button>
        <button type="button" disabled={busy} onClick={() => folderRef.current?.click()}>{t('Upload folder')}</button>
        <button type="button" className={danger} disabled={busy || selected.size === 0} onClick={() => {
          if (!confirm(`${t('Delete selected')} (${selected.size})?`)) return;
          void guard(async () => {
            const failed: string[] = [];
            for (const item of items.filter((entry) => selected.has(entry.id))) {
              try { await iconsApi.remove(item.id); } catch (error) { failed.push(`${item.name}: ${error instanceof Error ? error.message : String(error)}`); }
            }
            setSelected(new Set()); await reload();
            if (failed.length) toast.error(`${t('Error')}: ${failed.length}`);
          });
        }}>{selected.size ? `${t('Delete selected')} (${selected.size})` : t('Delete selected')}</button>
      </div>
      {status && <div className="mt-2.5 text-xs leading-relaxed text-muted" role="status">{status}</div>}
      <div className={box}>
        {items.length ? (
          <table>
            <thead><tr><th className="w-14"><RoundCheck checked={selected.size === items.length && items.length > 0} indeterminate={selected.size > 0 && selected.size < items.length} title={t('Select all')} ariaLabel={t('Select all')} onChange={toggleAll} /></th>
              {[t('Preview'), t('Name'), t('Size'), t('Transparency'), t('Machines'), t('Actions')].map((title) => <th key={title}>{title}</th>)}</tr></thead>
            <tbody>{items.map((item) => (
              <tr key={item.id}>
                <td className="w-14"><RoundCheck className="mx-auto" checked={selected.has(item.id)} title={item.name} ariaLabel={`${t('Select')} ${item.name}`} onChange={(checked) => toggle(item.id, checked)} /></td>
                <td><img className={`size-14 rounded-lg border border-line object-contain p-1 ${checker}`} src={item.url} alt="" width={56} height={56} /></td>
                <td className="max-w-[260px] truncate">{item.name}</td><td>{item.width}×{item.height}</td><td>{item.has_alpha ? t('Yes') : t('No')}</td><td>{item.used_by}</td>
                <td><div className="flex flex-wrap items-center gap-1.5 [&>button]:min-h-[30px] [&>button]:px-[11px] [&>button]:py-[5px] [&>button]:text-[13px]">
                  <button type="button" onClick={() => setRename(item)}>{t('Edit')}</button>
                  <button type="button" className={danger} onClick={() => {
                    if (!confirm(`${t('Delete icon?')} «${item.name}»`)) return;
                    void guard(async () => { await iconsApi.remove(item.id); await reload(); });
                  }}>{t('Delete')}</button>
                </div></td>
              </tr>
            ))}</tbody>
          </table>
        ) : <EmptyState title={t('Icon library is empty')} text={t('Upload an image or a whole folder.')} />}
      </div>
      {rename && <RenameIcon item={rename} onClose={() => setRename(null)} onSaved={() => { setRename(null); void reload(); }} />}
    </>
  );
}

function RenameIcon({ item, onClose, onSaved }: { item: IconItem; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(item.name);
  return (
    <Modal title={t('Rename icon')} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Save'), primary: true, onClick: async () => { await iconsApi.rename(item.id, name); onSaved(); } },
    ]}>
      <label>{t('Name')}</label><input value={name} maxLength={80} onChange={(event) => setName(event.target.value)} />
    </Modal>
  );
}
