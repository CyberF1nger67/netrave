import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { labPath } from '../../routing/paths';
import { foldersApi, labsApi } from '../../api/endpoints';
import type { Lab, LabPermissions } from '../../types/api';
import { guard, toast } from '../../stores/toast';
import { Modal } from '../../components/ui/Modal';
import { Spinner } from '../../components/ui/Spinner';
import { Switch } from '../../components/ui/Switch';
import { Dropdown } from '../../components/ui/Dropdown';
import { t, translateMessage } from '../../i18n';

export function LabDialog({ lab, folderId = '', folderName = null, onClose, onSaved }: { lab: Lab | null; folderId?: string; folderName?: string | null; onClose: () => void; onSaved: () => void }) {
  const navigate = useNavigate();
  const [name, setName] = useState(lab?.name ?? '');
  const [description, setDescription] = useState(lab?.description ?? '');
  return (
    <Modal title={lab ? t('Parameters') : t('New laboratory')} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: lab ? t('Save') : t('Create'), primary: true, onClick: async () => {
        if (lab) {
          await labsApi.update(lab.name, { name, description });
          toast.success(t('Saved')); onSaved(); onClose();
          if (name !== lab.name) navigate(labPath({ name, folder_id: lab.folder_id }, folderName));
        } else {
          const created = await labsApi.create(name, description);
          if (folderId) await foldersApi.placeLab(created.name, folderId);
          onSaved(); onClose();
          // The new lab opens immediately; the URL includes its folder name.
          navigate(labPath({ name: created.name, folder_id: folderId }, folderName));
        }
      } },
    ]}>
      <label htmlFor="lab-name">{t('Name')}</label><input id="lab-name" value={name} maxLength={80} disabled={!!lab?.running} onChange={(event) => setName(event.target.value)} />
      {lab?.running && <p className="text-muted">{t('The laboratory is running: stop it before renaming')}</p>}
      <label htmlFor="lab-description">{t('Description')}</label><textarea id="lab-description" rows={4} maxLength={2000} value={description} onChange={(event) => setDescription(event.target.value)} />
    </Modal>
  );
}

export function WipeLabDialog({ lab, instance, onClose, onDone }: { lab: { id: string; name: string }; instance?: number | undefined; onClose: () => void; onDone: () => void }) {
  return (
    <Modal title={t('Wipe Lab')} onClose={onClose} actions={[
      { label: t('Cancel'), onClick: onClose },
      { label: t('Reset laboratory'), primary: true, danger: true, onClick: async () => {
        const result = await labsApi.wipe(lab.id, instance);
        if (result.failed.length) toast.warning(`${t('Reset completed with errors')}: ${result.devices}, ${result.failed.map((item) => translateMessage(item.key, item.params)).join('; ')}`);
        else toast.success(`${t('Laboratory reset')}: ${result.devices}`);
        onDone(); onClose();
      } },
    ]}>
      <p>{t('Reset laboratory')} «{lab.name}» (ID {lab.id})?</p>
      <p className="text-bad">{t('All machines will be stopped and their state deleted. This is irreversible.')}</p>
      <p className="text-muted">{t('Preserved: topology, cables, layout, source images and uploaded disks.')}</p>
    </Modal>
  );
}

export function AccessDialog({ lab, onClose }: { lab: Lab; onClose: () => void }) {
  const [data, setData] = useState<LabPermissions | null>(null);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState('');
  useEffect(() => {
    labsApi.permissions(lab.id).then(setData).catch((cause: unknown) => setError(cause instanceof Error ? cause.message : String(cause)));
  }, [lab.id]);
  const toggle = async (userId: number, key: string, checked: boolean) => {
    await guard(async () => {
      await labsApi.setPermission(lab.id, userId, { [key]: checked });
      setData(await labsApi.permissions(lab.id));
    });
  };
  const users = data?.users ?? [];
  const current = users.find((user) => String(user.id) === selected) ?? users[0];
  return (
    <Modal title={`${t('Access permissions')}: ${lab.name}`} onClose={onClose} wide actions={[{ label: t('Close'), primary: true, onClick: onClose }]}>
      {error && <div className="text-bad">{error}</div>}
      {!data && !error && <Spinner />}
      {data && (
        <div className="w-full">
          <div className="my-2 flex flex-wrap items-center gap-2">
            <label className="my-0">{t('General access:')}</label>
            <div className="min-w-[280px] max-w-full">
              <Dropdown value={data.public_level} options={[
                { value: 'none', label: t('No (owner and explicitly granted rights only)') },
                { value: 'view', label: t('View for everyone') },
              ]} onChange={(value) => void guard(async () => {
                await labsApi.setPublic(lab.id, value as 'none' | 'view');
                setData(await labsApi.permissions(lab.id));
                toast.success(t('Access saved'));
              })} />
            </div>
          </div>
          <div className="mt-2.5 text-xs leading-relaxed text-muted">{t('The owner and administrators always have all rights.')}</div>
          {users.length > 0 && (
            <>
              <div className="mt-3 flex flex-wrap items-center gap-2.5">
                <label className="my-0" htmlFor="access-user">{t('User')}</label>
                <Dropdown className="w-full max-w-[360px]" value={current ? String(current.id) : ''}
                  options={users.map((user) => ({ value: String(user.id), label: user.role === 'admin' ? `${user.username} · ${t('Administrator')}` : user.username }))}
                  onChange={setSelected} />
              </div>
              {current && (
                <div className="mt-3 rounded-app-sm border border-line bg-bg-2 p-3.5">
                  <div className="mb-2 flex flex-wrap items-center gap-2"><b>{current.username}</b>{current.disabled && <span className="text-muted"> ({t('Disabled')})</span>}</div>
                  {current.role === 'admin' ? <div className="text-muted">{t('The administrator always has all sections.')}</div> : (
                    <div className="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2">
                      {data.catalog.map((perm) => (
                        <div key={perm.key} className="flex items-start gap-2">
                          <Switch className="flex items-start gap-2.5 rounded-md px-0.5 py-[5px] text-[13.5px] hover:text-text" checked={Boolean(current.perms[perm.key])}
                            label={perm.title}
                            onChange={(checked) => void toggle(current.id, perm.key, checked)} />
                          {current.role_perms?.[perm.key] && <span className="whitespace-nowrap rounded-full bg-accent-soft px-2 py-[2px] text-[11px] text-accent">{t('from role')}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </Modal>
  );
}
