import { askConfirm } from '../../stores/dialogs';
import { useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom';
import { labPath, paths } from '../../routing/paths';
import { findFolder } from '../../routing/labLookup';
import { foldersApi, labsApi } from '../../api/endpoints';
import type { Lab, LabFolder } from '../../types/api';
import { canCreateLabs, canManageFolders, canMoveLabs, useAuthStore } from '../../stores/auth';
import { closeConsolesForLab } from '../consoles/registry';
import { guard, toast } from '../../stores/toast';
import { fmtTime } from '../../lib/format';
import { TopologyPreview } from './TopologyPreview';
import { AccessDialog, LabDialog, WipeLabDialog } from './LabDialogs';
import { FolderNameDialog, FolderSelectDialog, folderPath, subtreeIds } from './FolderDialogs';
import { IMPORT_ACCEPT, importLabFile } from './importLab';
import { Icon } from '../../components/Icon';
import { EmptyState } from '../../components/ui/EmptyState';
import { Spinner } from '../../components/ui/Spinner';
import { t, useLanguageStore } from '../../i18n';

const DRAG = 'application/x-netrave-lab';
const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const iconBtn = 'size-[30px] min-h-[30px] rounded-app-sm border-transparent bg-transparent p-0 text-muted hover:bg-panel2 hover:text-text';
const acts = 'flex flex-wrap items-center gap-1.5 [&>button]:min-h-[30px] [&>button]:px-[11px] [&>button]:py-[5px] [&>button]:text-[13px]';
const crumb = "inline-flex min-h-7 items-center rounded-lg border border-dashed border-transparent px-[9px] py-[3px] text-muted no-underline transition-[background,border-color,color] hover:bg-panel2 hover:text-text last:font-semibold last:text-text data-[drop=true]:border-2 data-[drop=true]:border-solid data-[drop=true]:border-accent data-[drop=true]:bg-accent-soft";

function Instances({ lab, folderName }: { lab: Lab; folderName: string | null }) {
  const user = useAuthStore((state) => state.user);
  const query = useQuery({ queryKey: ['lab-instances', lab.id], queryFn: () => labsApi.instances(lab.id), enabled: user?.role === 'admin' });
  if (user?.role !== 'admin' || !query.data) return null;
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
      {query.data.length ? (
        <><span className="text-muted">{t('Instances:')} </span>{query.data.map((item) => (
          <Link key={item.id} className="rounded-full border border-line bg-panel2 px-2.5 py-[3px] text-xs text-text no-underline hover:border-accent" to={labPath(lab, folderName, item.id)} title={`${t('Instance user')} ${item.username}`}>
            {item.username} ({item.running}/{item.total})
          </Link>
        ))}</>
      ) : <span className="text-muted">{t('No instances')}</span>}
    </div>
  );
}

function Preview({ labId }: { labId: string }) {
  const query = useQuery({ queryKey: ['lab-preview', labId], queryFn: () => labsApi.topology(labId), staleTime: 30_000 });
  return query.data ? <TopologyPreview topology={query.data} /> : <span className="text-muted">{t('Loading…')}</span>;
}

type DialogState =
  | { kind: 'lab'; lab: Lab | null }
  | { kind: 'access'; lab: Lab }
  | { kind: 'wipe'; lab: Lab }
  | { kind: 'folder-new'; parent: string }
  | { kind: 'folder-rename'; folder: LabFolder }
  | { kind: 'folder-move'; folder: LabFolder }
  | { kind: 'lab-move'; lab: Lab };

export function LabsPage() {
  const params = useParams();
  const requestedFolder = params['folderName'] ?? '';
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const [search, setSearch] = useState('');
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const importInput = useRef<HTMLInputElement>(null);
  useLanguageStore((state) => state.language);
  const manageFolders = canManageFolders(user);
  const moveLabs = canMoveLabs(user);
  const createLabs = canCreateLabs(user);
  const folders = useQuery({ queryKey: ['folders'], queryFn: foldersApi.list });
  const labs = useQuery({ queryKey: ['labs'], queryFn: () => labsApi.list() });
  const reload = () => {
    void queryClient.invalidateQueries({ queryKey: ['labs'] });
    void queryClient.invalidateQueries({ queryKey: ['folders'] });
  };
  const searching = search.trim() !== '';
  const folderList = folders.data ?? [];
  const allLabs = labs.data ?? [];
  // A folder's URL is its name.
  const folder = requestedFolder ? findFolder(folderList, requestedFolder) : undefined;
  const current = folder?.path ?? '';

  const shown = useMemo(() => {
    const text = search.trim().toLowerCase();
    if (text) return allLabs.filter((lab) => `${lab.name} ${lab.description}`.toLowerCase().includes(text));
    return allLabs.filter((lab) => (lab.folder || '') === current);
  }, [allLabs, current, search]);

  const subs = useMemo(() => {
    const text = search.trim().toLowerCase();
    if (text) {
      return folderList.filter((folder) => folder.name.toLowerCase().includes(text))
        .sort((a, b) => a.name.localeCompare(b.name, 'ru'));
    }
    return folderList.filter((folder) => folder.parent === current).sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  }, [folderList, current, search]);

  const createFolder = async (name: string) => {
    if (dialog?.kind === 'folder-new') { await foldersApi.create(name, dialog.parent); reload(); }
  };
  const renameFolder = async (name: string) => {
    if (dialog?.kind === 'folder-rename') { await foldersApi.update(dialog.folder.path, { name }); reload(); }
  };
  const moveFolder = async (folderPath: string | null) => {
    if (dialog?.kind === 'folder-move') { await foldersApi.update(dialog.folder.path, { parent: folderPath }); reload(); }
  };
  const moveLab = async (folderPath: string | null) => {
    if (dialog?.kind === 'lab-move') { await foldersApi.placeLab(dialog.lab.name, folderPath); reload(); }
  };
  const dropLab = (labName: string, folderPath: string) => {
    if (!moveLabs) return;
    void guard(async () => {
      await foldersApi.placeLab(labName, folderPath || null);
      toast.success(t('Laboratory copied'));
      reload();
    });
  };

  const path = folderPath(folderList, current);
  if (folders.isLoading || labs.isLoading) return <Spinner />;
  if (folders.error || labs.error) return <div className="py-2 text-bad">{t('Failed to load laboratories')}</div>;
  if (requestedFolder && !searching && !folder) {
    return <Navigate to={paths.labs} replace />;
  }

  return (
    <>
      <h2>{t('Laboratories')}</h2>
      <div className="mb-3.5 flex flex-wrap items-center gap-2.5 [&>input]:max-w-[520px] [&>input]:flex-1 max-[900px]:[&>input]:max-w-none">
        <input placeholder={t('Search by name and description…')} value={search} onChange={(event) => setSearch(event.target.value)} />
        {manageFolders && <button type="button" onClick={() => setDialog({ kind: 'folder-new', parent: current })}><Icon name="folder" size={16} /> {t('New folder')}</button>}
        {createLabs && <button type="button" onClick={() => importInput.current?.click()} title={t('Import a laboratory from a NetRave export file (YAML)')}>{t('Import')}</button>}
        {createLabs && <button type="button" className={primary} onClick={() => setDialog({ kind: 'lab', lab: null })}>{t('New laboratory')}</button>}
        {createLabs && (
          <input ref={importInput} type="file" accept={IMPORT_ACCEPT} className="hidden" aria-hidden="true" tabIndex={-1}
            onChange={(event) => {
              const file = event.target.files?.[0];
              event.target.value = ''; // so the same file can be selected again
              if (!file) return;
              void guard(async () => {
                await importLabFile(file, current);
                reload();
              });
            }} />
        )}
      </div>
      <nav className="-mt-1 mb-[18px] flex flex-wrap items-center gap-1.5 text-[13.5px]" aria-label={t('Folders')}>
        {searching ? <span className="text-muted">{t('Search')}</span> : (
          <>
            <Link className={crumb} to={paths.labs} onDragOver={(event) => { if (moveLabs && event.dataTransfer.types.includes(DRAG)) event.preventDefault(); }}
              onDrop={(event) => { const name = event.dataTransfer.getData(DRAG); if (name) dropLab(name, ''); }} title={t('Root')}>root</Link>
            {path.flatMap((item) => [
              <span className="text-line-2" key={`sep-${item.path}`}>/</span>,
              <Link className={crumb} key={item.path} to={paths.folder(item.name)}
                onDragOver={(event) => { if (moveLabs && event.dataTransfer.types.includes(DRAG)) event.preventDefault(); }}
                onDrop={(event) => { const name = event.dataTransfer.getData(DRAG); if (name) dropLab(name, item.path); }}>
                {item.name}
              </Link>,
            ])}
          </>
        )}
      </nav>
      {subs.length > 0 && (
        <>
          <div className="mb-2.5 mt-1 text-xs font-semibold uppercase tracking-[.06em] text-muted">{t('Folders')}</div>
          <div className="mb-[22px] grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-3 max-sm:grid-cols-1">
            {subs.map((folder) => {
              const subCount = folderList.filter((item) => item.parent === folder.path).length;
              const labCount = folder.lab_count ?? allLabs.filter((lab) => lab.folder === folder.path).length;
              return (
                <div key={folder.path} data-folder-card className="group flex min-w-0 cursor-pointer items-center gap-3 rounded-app border border-line bg-panel py-3 pl-3 pr-2.5 shadow-app transition-[transform,border-color] hover:-translate-y-0.5 hover:border-line-2 data-[drop=true]:border-accent data-[drop=true]:bg-accent-soft" role="link" tabIndex={0} title={t('Open')}
                  onClick={() => { if (searching) setSearch(''); navigate(paths.folder(folder.name)); }}
                  onKeyDown={(event) => { if (event.key === 'Enter') { if (searching) setSearch(''); navigate(paths.folder(folder.name)); } }}
                  onDragOver={(event) => { if (moveLabs && event.dataTransfer.types.includes(DRAG)) { event.preventDefault(); event.currentTarget.dataset['drop'] = 'true'; } }}
                  onDragLeave={(event) => { delete event.currentTarget.dataset['drop']; }}
                  onDrop={(event) => { event.preventDefault(); delete event.currentTarget.dataset['drop']; const name = event.dataTransfer.getData(DRAG); if (name) dropLab(name, folder.path); }}>
                  <span className="grid size-11 flex-none place-items-center rounded-xl bg-gradient-to-br from-accent/22 to-accent-2/12 text-accent"><Icon name="folder" size={22} /></span>
                  <div className="flex min-w-0 flex-1 flex-col"><b className="truncate font-semibold">{folder.name}</b><small className="text-xs text-muted">{subCount} {t('Subfolders')} · {labCount} {t('labs')}</small></div>
                  {manageFolders && (
                    <div className="flex flex-none gap-0 opacity-60 group-hover:opacity-100 group-focus-within:opacity-100">
                      <button type="button" className={iconBtn} title={t('Rename folder')} onClick={(event) => { event.stopPropagation(); setDialog({ kind: 'folder-rename', folder }); }}><Icon name="edit" size={16} /></button>
                      <button type="button" className={iconBtn} title={t('Move folder')} onClick={(event) => { event.stopPropagation(); setDialog({ kind: 'folder-move', folder }); }}><Icon name="open" size={16} /></button>
                      <button type="button" className={`${iconBtn} hover:text-bad`} title={t('Delete')} onClick={async (event) => {
                        event.stopPropagation();
                        if (!(await askConfirm({ title: `${t('Delete folder?')} (${folder.name}). ${t('Laboratories will be moved to “No folder”.')}`, danger: true }))) return;
                        void guard(async () => { await foldersApi.remove(folder.path); reload(); });
                      }}><Icon name="trash" size={16} /></button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </>
      )}
      {shown.length > 0 ? (
        <>
          <div className="mb-2.5 mt-1 text-xs font-semibold uppercase tracking-[.06em] text-muted">{searching ? t('Laboratories') : current ? t('Laboratories in this folder') : t('Laboratories without a folder')}</div>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-4 max-sm:grid-cols-1">
            {shown.map((lab) => (
              <div key={lab.name} data-lab-card className="group flex flex-col overflow-hidden rounded-app border border-line bg-panel shadow-app transition-[transform,box-shadow,border-color] duration-300 hover:-translate-y-[3px] hover:border-line-2 hover:shadow-[0_20px_44px_-20px_rgba(0,0,0,.55)] [&[draggable=true]]:cursor-grab" draggable={moveLabs}
                onDragStart={(event) => { event.dataTransfer.setData(DRAG, lab.name); event.dataTransfer.effectAllowed = 'move'; }}
                title={moveLabs ? t('Drag to a folder to move') : undefined}>
                <Link className="flex h-[168px] items-center justify-center border-b border-line bg-bg-2 bg-[radial-gradient(circle_at_50%_35%,var(--accent-soft),transparent_70%)] no-underline" to={labPath(lab)} title={t('Open')}><Preview labId={lab.id} /></Link>
                <div className="px-[18px] pb-3.5 pt-4">
                  <div className="text-[15.5px] font-[650] tracking-[-.005em] [&>a]:text-text [&>a]:no-underline [&>a:hover]:text-accent"><Link to={labPath(lab)}>{lab.name}</Link></div>
                  <div className="my-1 truncate text-muted">{lab.description || '—'}</div>
                  <div className="text-muted">{lab.devices} {t('Device')} · {lab.networks} {t('Network')} · {fmtTime(lab.updated_at)} · {t('Owner')}: {lab.owner}</div>
                  {searching && <div className="text-muted">{t('Folder')}: {folderList.find((item) => item.path === lab.folder)?.name ?? t('Root (no folder)')}</div>}
                  <Instances lab={lab} folderName={null} />
                  <div className={`mt-3 ${acts} py-2`}>
                    <button type="button" className={primary} onClick={() => navigate(labPath(lab))}>{t('Open')}</button>
                    {lab.perms['lab.edit_meta'] && <button type="button" onClick={() => setDialog({ kind: 'lab', lab })}>{t('Parameters')}</button>}
                    {moveLabs && <button type="button" onClick={() => setDialog({ kind: 'lab-move', lab })}>{t('Move to folder…')}</button>}
                    {lab.perms['lab.manage_access'] && <button type="button" onClick={() => setDialog({ kind: 'access', lab })}>{t('Access permissions')}</button>}
                    {lab.perms['lab.export'] && lab.owner_id !== user?.id && (
                      <button type="button" className={primary} title={t('Separate copy only for you')} onClick={() => void guard(async () => {
                        const copy = await labsApi.copy(lab.name);
                        toast.success(t('Your copy of the laboratory has been created'));
                        navigate(labPath(copy));
                      })}>{t('Open my copy')}</button>
                    )}
                    {lab.perms['lab.export'] && lab.owner_id === user?.id && (
                      <button type="button" onClick={() => void guard(async () => {
                        await labsApi.copy(lab.name); toast.success(t('Laboratory copied')); reload();
                      })}>{t('Copy')}</button>
                    )}
                    {lab.perms['lab.export'] && <button type="button" onClick={() => void guard(async () => {
                      const blob = await labsApi.exportLab(lab.name);
                      const anchor = document.createElement('a');
                      anchor.href = URL.createObjectURL(blob);
                      anchor.download = `${lab.name}.yaml`;
                      document.body.append(anchor); anchor.click(); anchor.remove();
                      URL.revokeObjectURL(anchor.href);
                    })}>{t('Export')}</button>}
                    {lab.perms['lab.wipe'] && <button type="button" className={danger} onClick={() => setDialog({ kind: 'wipe', lab })}>{t('Wipe Lab')}</button>}
                    {lab.perms['lab.delete'] && <button type="button" className={danger} onClick={async () => {
                      if (!(await askConfirm({ title: `${t('Delete laboratory?')} (${lab.name})`, danger: true }))) return;
                      closeConsolesForLab(lab.name);
                      void guard(async () => { await labsApi.remove(lab.name); reload(); });
                    }}>{t('Delete')}</button>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </>
      ) : (
        <EmptyState title={searching ? t('Nothing found') : current ? t('Folder is empty') : (createLabs ? t('No laboratories yet') : t('No laboratories available'))}
          text={searching ? t('Change the query.') : current ? t('Drag a laboratory here or create a new one.') : (createLabs ? t('Create the first laboratory or folder for grouping.') : t('The administrator has not assigned any folders with laboratories to you yet.'))} />
      )}
      {dialog?.kind === 'lab' && <LabDialog lab={dialog.lab} folderId={current} folderName={folder?.name ?? null} onClose={() => setDialog(null)} onSaved={reload} />}
      {dialog?.kind === 'access' && <AccessDialog lab={dialog.lab} onClose={() => { setDialog(null); reload(); }} />}
      {dialog?.kind === 'wipe' && <WipeLabDialog lab={dialog.lab} onClose={() => setDialog(null)} onDone={reload} />}
      {dialog?.kind === 'folder-new' && <FolderNameDialog title={t('New folder')} onClose={() => setDialog(null)} onSave={createFolder} />}
      {dialog?.kind === 'folder-rename' && <FolderNameDialog title={t('Rename folder')} initial={dialog.folder.name} onClose={() => setDialog(null)} onSave={renameFolder} />}
      {dialog?.kind === 'folder-move' && <FolderSelectDialog title={`${t('Move folder')} (${dialog.folder.name})`} folders={folderList}
        value={dialog.folder.parent || null} exclude={subtreeIds(folderList, dialog.folder.path)} onClose={() => setDialog(null)} onSave={moveFolder} />}
      {dialog?.kind === 'lab-move' && <FolderSelectDialog title={t('Move laboratory')} folders={folderList} confirmLabel={t('Move laboratory')}
        value={dialog.lab.folder || null} onClose={() => setDialog(null)} onSave={moveLab} />}
    </>
  );
}
