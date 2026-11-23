import { askConfirm, askText } from '../../stores/dialogs';
import { useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { imagesApi } from '../../api/endpoints';
import { can, useAuthStore } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import { fmtBytes } from '../../lib/format';
import { folderInputProps } from '../../lib/desktop';
import { EmptyState } from '../../components/ui/EmptyState';
import { Spinner } from '../../components/ui/Spinner';
import { t, translateMessage, useLanguageStore } from '../../i18n';
import { isValidName, isValidFileName } from '../../lib/names';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';

const DISK_RE = /\.(qcow2|raw|img|iso)$/i;

export function ImagesPage() {
  const user = useAuthStore((state) => state.user);
  const isAdmin = can(user, 'images');
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['images'], queryFn: imagesApi.list });
  const inputRef = useRef<HTMLInputElement>(null);
  const [progress, setProgress] = useState<{ loaded: number; total: number; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  useLanguageStore((state) => state.language);

  const finish = async () => {
    setProgress({ loaded: 1, total: 1, text: t('Loading…') });
    const result = await imagesApi.scan();
    (result.problems ?? []).forEach((problem) => toast.warning(translateMessage(problem.key, problem.params)));
    await queryClient.invalidateQueries({ queryKey: ['images'] });
  };

  /** Uploads the files of the selected folder to the server and registers the image. */
  const send = async (files: Array<{ dir: string; file: File }>) => {
    const total = files.reduce((sum, item) => sum + item.file.size, 0);
    let done = 0;
    setBusy(true);
    try {
      for (let index = 0; index < files.length; index++) {
        const item = files[index]!;
        await imagesApi.upload(item.dir, item.file, (loaded) => {
          setProgress({ loaded: done + loaded, total, text: `${index + 1} / ${files.length}: ${item.file.name}` });
        });
        done += item.file.size;
      }
      await finish();
      toast.success(`${t('Image registered:')} ${files[0]?.dir ?? ''}`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
      setProgress(null);
    }
  };

  /**
   * Files selected with a regular `<input type="file" webkitdirectory>`. A folder or file name that
   * the server would reject is reported before anything is uploaded; names are never changed silently.
   */
  const uploadFromPicker = async (picked: File[]) => {
    const dir = (picked.find((file) => file.webkitRelativePath)?.webkitRelativePath ?? '').split('/')[0] ?? '';
    if (!dir) { toast.error(t('Select image folder')); return; }
    const files = picked
      .map((file) => ({ file, parts: (file.webkitRelativePath || '').split('/') }))
      .filter((item) => item.parts.length === 2 && item.parts[0] === dir && !(item.parts[1] ?? '').startsWith('.') && DISK_RE.test(item.file.name))
      .map(({ file }) => file);
    if (!files.length) { toast.error(t('No disks')); return; }

    if (!isValidName(dir)) { toast.error(`${t('Invalid folder name:')} ${dir}`); return; }
    const badFile = files.find((file) => !isValidFileName(file.name));
    if (badFile) { toast.error(`${t('Invalid file name:')} ${badFile.name}`); return; }

    await send(files.map((file) => ({ dir, file })));
  };

  /** Opens the folder picker. The field is already created with `webkitdirectory`, so the first click always opens a folder. */
  const openFolder = () => {
    if (!busy) inputRef.current?.click();
  };

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;
  return (
    <>
      <h2>{t('Images')}</h2>
      {isAdmin && (
        <div className="mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5">
          <h3 className="mb-3 mt-0 text-[13px] font-semibold uppercase tracking-[.06em] text-muted">{t('Upload image (whole folder)')}</h3>
          <div className="flex flex-wrap items-center gap-2.5">
            <input ref={inputRef} type="file" multiple hidden {...folderInputProps} onChange={(event) => {
              const files = [...(event.target.files ?? [])];
              event.target.value = '';
              void uploadFromPicker(files);
            }} />
            <button type="button" className={primary} disabled={busy} onClick={openFolder}>{t('Upload image folder')}</button>
            <button type="button" disabled={busy} onClick={() => void guard(async () => {
              const result = await imagesApi.scan();
              toast.success(`${t('Added:')} ${result.added}, ${t('updated:')} ${result.updated}`);
              (result.problems ?? []).forEach((problem) => toast.warning(translateMessage(problem.key, problem.params)));
              await queryClient.invalidateQueries({ queryKey: ['images'] });
            })}>{t('Scan image directory')}</button>
          </div>
          <div className="mt-2.5 text-xs leading-relaxed text-muted">{t('Choose the folder that contains the image disks.')}</div>
          {progress && <><div className="my-1.5 h-[7px] overflow-hidden rounded-full bg-panel3"><i className="block h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-[width] duration-500" style={{ width: `${progress.total ? progress.loaded / progress.total * 100 : 100}%` }} /></div><div className="text-muted">{progress.text}</div></>}
        </div>
      )}
      <div className="mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5">
        {(query.data ?? []).length ? (
          <table>
            <thead><tr>{[t('Name'), t('Folder'), t('Disks (in order)'), t('CD-ROM'), t('Size'), t('Used by'), ...(isAdmin ? [t('Actions')] : [])].map((title) => <th key={title}>{title}</th>)}</tr></thead>
            <tbody>{query.data!.map((image) => (
              <tr key={image.id}>
                <td>{image.name}</td><td className="text-muted">{image.dirname}</td>
                <td>{image.disks.length ? image.disks.map((disk) => disk.name).join(', ') : <span className="text-bad">{t('No disks')}</span>}</td>
                <td>{image.cdroms.length ? image.cdroms.join(', ') : '—'}</td>
                <td>{fmtBytes(image.size_bytes)}</td><td>{image.used_by ? `${image.used_by} ${t('Device')}` : '—'}</td>
                {isAdmin && <td><div className="flex flex-wrap items-center gap-1.5 [&>button]:min-h-[30px] [&>button]:px-[11px] [&>button]:py-[5px] [&>button]:text-[13px]">
                  <button type="button" onClick={async () => {
                    const name = await askText({ title: t('New image name:'), initial: image.name, confirmLabel: t('Save') });
                    if (!name) return;
                    void guard(async () => { await imagesApi.rename(image.id, name); await queryClient.invalidateQueries({ queryKey: ['images'] }); });
                  }}>{t('Edit')}</button>
                  <button type="button" className={danger} onClick={async () => {
                    if (!(await askConfirm({ title: `${t('Delete image and its folder?')} (${image.name})`, danger: true }))) return;
                    void guard(async () => { await imagesApi.remove(image.id); await queryClient.invalidateQueries({ queryKey: ['images'] }); });
                  }}>{t('Delete')}</button>
                </div></td>}
              </tr>
            ))}</tbody>
          </table>
        ) : <EmptyState title={t('No images yet')} text={t('Upload a folder with disks.')} />}
      </div>
    </>
  );
}
