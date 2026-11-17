import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { foldersApi, rolesApi } from '../../api/endpoints';
import type { LabFolder, RbacPermission, RbacRole } from '../../types/api';
import { guard, toast } from '../../stores/toast';
import { fmtTime } from '../../lib/format';
import { Modal } from '../../components/ui/Modal';
import { Spinner } from '../../components/ui/Spinner';
import { Switch } from '../../components/ui/Switch';
import { NumberField } from '../../components/ui/NumberField';
import { folderPath } from '../labs/FolderDialogs';
import { currentLanguage, t, useLanguageStore } from '../../i18n';

const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const box = 'mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5';
const hint = 'mt-2.5 text-xs leading-relaxed text-muted';
// Two columns inside each functional group: items go top to bottom in pairs.
const permGrid = 'grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2';
const permItem = 'flex items-start gap-2.5 rounded-md px-0.5 py-[5px] text-[13.5px] hover:text-text';
const roleSection = 'border-t border-line pt-3';
const roleTitle = 'mb-2 mt-0 text-[13px] font-bold uppercase tracking-[.05em] text-muted';
const list = 'grid max-h-[190px] grid-cols-1 gap-x-6 gap-y-0.5 overflow-auto rounded-app-sm border border-line bg-bg-2 px-2.5 py-2 sm:grid-cols-2';
const row2 = 'grid grid-cols-1 items-end gap-2.5 sm:grid-cols-2';

const GROUPS: Array<{ key: string; en: string; ru: string }> = [
  { key: 'labs', en: 'Laboratories', ru: 'Лаборатории' },
  { key: 'folders', en: 'Folders', ru: 'Папки' },
  { key: 'machines', en: 'Virtual machines', ru: 'Виртуальные машины' },
  { key: 'users', en: 'Users', ru: 'Пользователи' },
  { key: 'extra', en: 'Additional capabilities', ru: 'Дополнительные возможности' },
];

const PERMISSION_LABELS: Record<string, { en: string; ru: string }> = {
  'laboratories.view': { en: 'View laboratories', ru: 'Просмотр лабораторий' },
  'laboratories.create': { en: 'Create laboratories', ru: 'Создание лабораторий' },
  'laboratories.edit': { en: 'Edit laboratories', ru: 'Редактирование лабораторий' },
  'laboratories.delete': { en: 'Delete laboratories', ru: 'Удаление лабораторий' },
  'laboratories.copy': { en: 'Copy laboratories', ru: 'Копирование лабораторий' },
  'laboratories.start_stop': { en: 'Start and stop machines', ru: 'Запуск и остановка машин' },
  'laboratories.wipe': { en: 'Wipe machines', ru: 'Wipe машин' },
  'laboratories.topology': { en: 'Manage topology', ru: 'Управление топологией' },
  'folders.view': { en: 'View assigned folders', ru: 'Просмотр назначенных папок' },
  'folders.access': { en: 'Access folder contents', ru: 'Доступ к содержимому папок' },
  'folders.move_labs': { en: 'Move laboratories between folders', ru: 'Перемещение лабораторий между папками' },
  'folders.manage': { en: 'Manage folders', ru: 'Управление папками' },
  'machines.start_stop': { en: 'Start and stop', ru: 'Запуск и остановка' },
  'machines.restart': { en: 'Restart', ru: 'Перезагрузка' },
  'machines.wipe': { en: 'Wipe', ru: 'Wipe' },
  'machines.serial': { en: 'Serial console', ru: 'Serial Console' },
  'machines.vnc': { en: 'VNC console', ru: 'VNC Console' },
  'machines.clipboard': { en: 'Console clipboard (copy and paste)', ru: 'Буфер обмена в консоли (копирование и вставка)' },
  'users.view': { en: 'View users', ru: 'Просмотр пользователей' },
  'users.settings': { en: 'Change user settings', ru: 'Изменение пользовательских настроек' },
  'users.password': { en: 'Change own password', ru: 'Смена собственного пароля' },
  'users.manage': { en: 'Manage users', ru: 'Управление пользователями' },
  'notifications.view': { en: 'View notifications', ru: 'Просмотр уведомлений' },
  'settings.own': { en: 'Manage own settings', ru: 'Управление собственными настройками' },
  'admin.access': { en: 'Access administrative sections', ru: 'Доступ к административным разделам' },
};

const ru = () => currentLanguage() === 'ru';

function groupTitle(key: string): string {
  const group = GROUPS.find((item) => item.key === key);
  return group ? (ru() ? group.ru : group.en) : key;
}

function permissionTitle(key: string, fallback: string): string {
  const label = PERMISSION_LABELS[key];
  return label ? (ru() ? label.ru : label.en) : fallback;
}

function RoleDialog({ role, catalog, folders, onClose, onSaved }: {
  role: RbacRole | null;
  catalog: RbacPermission[];
  folders: LabFolder[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(role?.name ?? '');
  const [description, setDescription] = useState(role?.description ?? '');
  const [permissions, setPermissions] = useState<string[]>(role?.permissions ?? []);
  const [folderIds, setFolderIds] = useState<number[]>(role?.folder_ids ?? []);
  const [saving, setSaving] = useState(false);
  // 0 - no limit: how many labs the role allows to run simultaneously.
  const [maxRunning, setMaxRunning] = useState(role?.max_running_labs ?? 0);
  // For the built-in administrator role only the description can be changed: permissions and members are defined by the role itself.
  const system = Boolean(role?.system);

  const togglePermission = (key: string, checked: boolean) => {
    setPermissions((current) => checked ? [...current, key] : current.filter((item) => item !== key));
  };
  const toggleFolder = (id: number, checked: boolean) => {
    setFolderIds((current) => checked ? [...current, id] : current.filter((item) => item !== id));
  };

  const save = async () => {
    const trimmed = name.trim();
    if (!trimmed) { toast.error(t('Enter a name')); return; }
    setSaving(true);
    try {
      if (role && role.system) {
        await rolesApi.update(role.id, { description });
      } else if (role) {
        await rolesApi.update(role.id, { name: trimmed, description, permissions, folder_ids: folderIds, max_running_labs: maxRunning });
      } else {
        const created = await rolesApi.create(trimmed, description);
        await rolesApi.update(created.id, { permissions, folder_ids: folderIds, max_running_labs: maxRunning });
      }
      toast.success(t('Role saved'));
      onSaved();
      onClose();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title={role ? `${t('Edit')}: ${role.name}` : t('New role')} onClose={onClose} wide
      actions={[{ label: saving ? t('Saving…') : t('Save'), primary: true, onClick: save }, { label: t('Close'), onClick: onClose }]}>
      <div className="flex flex-col gap-3.5">
        {system && <div className="text-xs leading-relaxed text-muted">{t('The administrator role always has every permission. Only the description can be changed.')}</div>}
        <div className={row2}>
          <label className="my-0">
            <span className="mb-1.5 block text-[13px] leading-snug">{t('Role name')}</span>
            <input placeholder={t('Role name')} value={name} disabled={system} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="my-0">
            <span className="mb-1.5 block text-[13px] leading-snug">{t('Description')}</span>
            <input placeholder={t('Description')} value={description} onChange={(event) => setDescription(event.target.value)} />
          </label>
        </div>

        <div className={`${roleSection} ${system ? 'pointer-events-none opacity-50' : ''}`}>
          <h4 className={roleTitle}>{t('Permissions')}</h4>
          <div className={hint}>{t('Each role receives only the permissions selected here.')}</div>
          {GROUPS.map((group) => {
            const items = catalog.filter((perm) => perm.group_key === group.key);
            if (!items.length) return null;
            return (
              <div className="mb-1 mt-2.5 border-t-0 pt-0 [&>b]:mb-1 [&>b]:block [&>b]:text-[12.5px]" key={group.key}>
                <b>{groupTitle(group.key)}</b>
                <div className={permGrid}>
                  {items.map((perm) => (
                    <Switch key={perm.key} className={permItem} checked={permissions.includes(perm.key)} title={permissionTitle(perm.key, perm.title)}
                      label={permissionTitle(perm.key, perm.title)}
                      onChange={(checked) => togglePermission(perm.key, checked)} />
                  ))}
                </div>
              </div>
            );
          })}
        </div>

        <div className={`${roleSection} ${system ? 'pointer-events-none opacity-50' : ''}`}>
          <h4 className={roleTitle}>{t('Running laboratories')}</h4>
          <label className="my-0 max-w-[320px]">
            <span className="mb-1.5 block text-[13px] leading-snug">{t('Maximum simultaneously running laboratories')}</span>
            <NumberField min={0} max={1000} value={maxRunning} onValueChange={setMaxRunning} />
          </label>
          <div className={hint}>{t('0 means no limit. Only laboratories with machines running at the same time are counted.')}</div>
        </div>

        <div className={`${roleSection} ${system ? 'pointer-events-none opacity-50' : ''}`}>
          <h4 className={roleTitle}>{t('Folders')}</h4>
          <div className={hint}>{t('All role members automatically get access to the selected folders and their subfolders.')}</div>
          {folders.length ? (
            <div className={list}>
              {folders.map((folder) => (
                <Switch key={folder.id} className={permItem} checked={folderIds.includes(folder.id)}
                  label={folderPath(folders, folder.id).map((part) => part.name).join(' / ')}
                  onChange={(checked) => toggleFolder(folder.id, checked)} />
              ))}
            </div>
          ) : <div className="text-muted">{t('There are no folders yet. Create folders in the laboratory list.')}</div>}
        </div>
      </div>
    </Modal>
  );
}

export function RolesPage() {
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<RbacRole | null>(null);
  const [creating, setCreating] = useState(false);
  const roles = useQuery({ queryKey: ['roles'], queryFn: rolesApi.list });
  const catalog = useQuery({ queryKey: ['roles-catalog'], queryFn: rolesApi.catalog, staleTime: 300_000 });
  const folders = useQuery({ queryKey: ['folders'], queryFn: foldersApi.list });
  useLanguageStore((state) => state.language);

  const reload = () => {
    void queryClient.invalidateQueries({ queryKey: ['roles'] });
    void queryClient.invalidateQueries({ queryKey: ['users'] });
  };

  const remove = (role: RbacRole) => {
    if (!confirm(`${t('Delete role?')} (${role.name})`)) return;
    void guard(async () => {
      await rolesApi.remove(role.id);
      reload();
      toast.success(t('Role deleted'));
    });
  };

  if (roles.isLoading || catalog.isLoading) return <Spinner />;
  if (roles.error) return <div className="py-2 text-bad">{roles.error.message}</div>;

  return (
    <>
      <div className="mb-3 flex items-center gap-3">
        <h2>{t('Roles')}</h2>
        <div className="flex-1" />
        <button type="button" className={primary} onClick={() => setCreating(true)}>{t('New role')}</button>
      </div>
      <div className={box}>
        <div className={hint}>{t('Roles combine permissions and folder access for their members.')}</div>
        <table>
          <thead><tr>{[t('Name'), t('Description'), t('Members'), t('Permissions'), t('Created'), ''].map((title) => <th key={title}>{title}</th>)}</tr></thead>
          <tbody>
            {(roles.data ?? []).map((role) => (
              // The built-in administrator role is shown as a regular one, but its fields are read-only.
              <tr key={role.id} className={role.system ? 'opacity-70' : undefined}>
                <td><b>{role.name}</b>{role.system && <span className="ml-2 rounded-full bg-accent-soft px-2 py-[2px] text-[11px] text-accent">{t('Built-in')}</span>}</td>
                <td className="text-muted">{role.description || '—'}</td>
                <td>{role.member_count}</td>
                <td>{role.permissions.length}</td>
                <td className="text-muted">{role.system ? '—' : fmtTime(role.created_at)}</td>
                <td><div className="flex flex-wrap items-center gap-1.5 [&>button]:min-h-[30px] [&>button]:px-[11px] [&>button]:py-[5px] [&>button]:text-[13px]">
                  {/* The built-in administrator role can be opened and its description changed, but it cannot be deleted. */}
                  <button type="button" onClick={() => setEditing(role)}>{t('Edit')}</button>
                  <button type="button" className={danger} disabled={Boolean(role.system)}
                    onClick={() => remove(role)}>{t('Delete')}</button>
                </div></td>
              </tr>
            ))}
          </tbody>
        </table>
        {roles.data && roles.data.length === 0 && <div className="py-2 text-muted">{t('No roles yet')}</div>}
      </div>
      {(creating || editing) && (
        <RoleDialog role={editing} catalog={catalog.data ?? []} folders={folders.data ?? []}
          onClose={() => { setCreating(false); setEditing(null); }} onSaved={reload} />
      )}
    </>
  );
}
