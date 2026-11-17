import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { foldersApi, usersApi } from '../../api/endpoints';
import type { LabFolder, User } from '../../types/api';
import { Modal } from '../../components/ui/Modal';
import { Spinner } from '../../components/ui/Spinner';
import { Switch } from '../../components/ui/Switch';
import { guard, toast } from '../../stores/toast';
import { t } from '../../i18n';

// Tree display order: the parent, then its subfolders with indentation.
function folderRows(folders: LabFolder[]): Array<{ folder: LabFolder; depth: number }> {
  const kids = new Map<number, LabFolder[]>();
  for (const folder of folders) {
    const key = folder.parent_id ?? 0;
    kids.set(key, [...(kids.get(key) ?? []), folder]);
  }
  for (const list of kids.values()) list.sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  const rows: Array<{ folder: LabFolder; depth: number }> = [];
  const walk = (parent: number, depth: number) => {
    for (const folder of kids.get(parent) ?? []) {
      rows.push({ folder, depth });
      walk(folder.id, depth + 1);
    }
  };
  walk(0, 0);
  return rows;
}

const hint = 'mt-2.5 text-xs leading-relaxed text-muted';
const permHead = 'mb-2 flex flex-wrap items-center gap-2';
const permGrid = 'grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2';
const permItem = 'flex items-start gap-2.5 rounded-md px-0.5 py-[5px] text-[13.5px] hover:text-text';

export function FoldersDialog({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const folders = useQuery({ queryKey: ['folders'], queryFn: foldersApi.list });
  const access = useQuery({
    queryKey: ['user-folders', user.id],
    queryFn: () => usersApi.folders(user.id),
  });
  const [selected, setSelected] = useState<number[] | null>(null);
  const current = selected ?? access.data?.folder_ids ?? [];
  const rows = useMemo(() => folderRows(folders.data ?? []), [folders.data]);
  const isAdmin = user.role === 'admin';

  const toggle = (id: number, checked: boolean) => {
    const next = checked ? [...current, id] : current.filter((item) => item !== id);
    setSelected([...new Set(next)]);
  };

  const save = async () => {
    await guard(async () => {
      await usersApi.setFolders(user.id, current);
      toast.success(t('Folder access saved'));
      onSaved();
    });
  };

  return (
    <Modal title={`${t('Folder access')}: ${user.username}`} onClose={onClose} wide
      actions={[
        { label: t('Cancel'), onClick: onClose },
        ...(isAdmin ? [] : [{ label: t('Save'), primary: true, onClick: save }]),
      ]}>
      <div className="w-full">
        <p className="text-muted">{t('Select the folders this user can see. Laboratories from other folders stay hidden.')}</p>
        <p className="text-muted">{t('When a parent folder is assigned, its subfolders are available automatically.')}</p>
        {isAdmin ? (
          <div className={hint}>{t('The administrator always has access to all folders.')}</div>
        ) : (folders.isLoading || access.isLoading) ? <Spinner /> : !rows.length ? (
          <div className={hint}>{t('There are no folders yet. Create folders in the laboratory list.')}</div>
        ) : (
          <>
            <div className={permHead}>
              <b>{t('Assigned folders')}: {current.length}</b>
            </div>
            <div className={permGrid}>
              {rows.map(({ folder, depth }) => (
                <Switch key={folder.id} className={permItem} checked={current.includes(folder.id)} title={`${folder.lab_count} ${t('labs')}`}
                  label={<>{depth > 0 ? '↳ ' : ''}{folder.name}<span className="text-muted"> · {folder.lab_count} {t('labs')}</span></>}
                  onChange={(checked) => toggle(folder.id, checked)} />
              ))}
            </div>
          </>
        )}
        {access.error && <div className="text-bad">{access.error.message}</div>}
      </div>
    </Modal>
  );
}
