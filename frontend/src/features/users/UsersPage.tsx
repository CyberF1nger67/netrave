import { askConfirm } from '../../stores/dialogs';
import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { labsApi, rolesApi, usersApi } from '../../api/endpoints';
import type { Lab, LabPermissions, RbacRole, User } from '../../types/api';
import { useAuthStore, can } from '../../stores/auth';
import { guard, toast } from '../../stores/toast';
import { fmtTime } from '../../lib/format';
import { Modal } from '../../components/ui/Modal';
import { PasswordDialog } from '../../components/ui/PasswordDialog';
import { PasswordInput } from '../../components/ui/PasswordInput';
import { Switch } from '../../components/ui/Switch';
import { Dropdown } from '../../components/ui/Dropdown';
import { Spinner } from '../../components/ui/Spinner';
import { t, translateMessage, useLanguageStore } from '../../i18n';
import { USERNAME_MAX_LENGTH, filterUsername, isValidUsername } from '../../lib/username';
import { PASSWORD_MAX_LENGTH, isValidPassword } from '../../lib/names';


const primary = 'border-transparent bg-gradient-to-br from-accent to-accent-2 font-semibold text-on-accent shadow-[0_6px_18px_-6px_rgba(99,102,241,.55)] hover:brightness-110';
const danger = 'text-bad hover:border-bad hover:bg-bad/10';
const box = 'mb-[18px] rounded-app border border-line bg-panel p-5 shadow-app max-sm:overflow-x-auto max-sm:p-3.5';
const boxTitle = 'mb-3 mt-0 text-[13px] font-semibold uppercase tracking-[.06em] text-muted';
const permUser = 'my-3 rounded-app-sm border border-line bg-bg-2 p-3.5';
// Two columns: permissions are read top to bottom in pairs and do not stretch across the full width.
const permGrid = 'grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2';
const permItem = 'flex items-start gap-2.5 rounded-md px-0.5 py-[5px] text-[13.5px] hover:text-text';
const hint = 'mt-2.5 text-xs leading-relaxed text-muted';

const LAB_PERM_TITLES: Record<string, string> = {
  'lab.view': 'View laboratory and topology',
  'lab.edit_meta': 'Edit name and description',
  'lab.save_topology': 'Save topology view',
  'lab.export': 'Export and copy laboratory',
  'lab.delete': 'Delete laboratory',
  'lab.manage_access': 'Manage access for other users',
  'lab.wipe': 'Wipe laboratory',
  'machine.add': 'Add machines',
  'machine.edit': 'Edit machine parameters',
  'machine.move': 'Move machines on canvas',
  'machine.clone': 'Clone machines',
  'machine.delete': 'Delete machines',
  'machine.start': 'Start machines',
  'machine.stop': 'Stop and pause machines',
  'machine.restart': 'Restart machines',
  'machine.wipe': 'Wipe machine state',
  'machine.snapshot': 'Saving images',
  'link.create': 'Create cables',
  'link.delete': 'Delete cables',
  'iface.manage': 'Manage machine network interfaces',
  'net.manage': 'Manage networks',
  'disk.attach': 'Attach disks and images',
  'disk.detach': 'Detach disks',
  'console.serial': 'Serial console',
  'console.vnc': 'VNC console',
  'console.clipboard': 'Copy and paste in the console',
};
const permTitle = (key: string): string => {
  const title = LAB_PERM_TITLES[key];
  if (!title) return key;
  return translateMessage(title);
};

function PermissionDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const [labId, setLabId] = useState<string | null>(null);
  const [permissions, setPermissions] = useState<LabPermissions | null>(null);
  const labs = useQuery({ queryKey: ['labs', 'perm-dialog'], queryFn: () => labsApi.list() });
  const openLab = async (id: string) => {
    setLabId(id);
    setPermissions(null);
    try {
      setPermissions(await labsApi.permissions(id));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    }
  };

  const row = permissions?.users.find((item) => item.id === user.id);
  const isOwner = permissions?.owner_id === user.id;

  const toggleLab = async (key: string, checked: boolean) => {
    if (!labId || !row) return;
    try {
      await labsApi.setPermission(labId, user.id, { [key]: checked });
      setPermissions(await labsApi.permissions(labId));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    }
  };

  return (
    <Modal title={`${t('Rights')}: ${user.username}`} onClose={onClose} wide
      actions={[{ label: t('Close'), primary: true, onClick: onClose }]}>
      <div className="w-full">
        <div className={permUser}>
          <div className="my-2 flex flex-wrap items-center gap-2 [&>select]:min-w-[280px] [&>select]:max-w-full">
            <b>{t('Rights for laboratories')}</b>
            <Dropdown className="w-full max-w-[420px]" value={labId ?? ''} options={[
              { value: '', label: `— ${t('Search')} —` },
              ...(labs.data ?? []).map((lab: Lab) => ({ value: lab.name, label: `${lab.name} · ${t('Owner')}: ${lab.owner}` })),
            ]} onChange={(value) => { if (value) void openLab(value); }} />
          </div>
          {!labId && <div className="text-muted">{t('Select a laboratory to configure machine permissions.')}</div>}
          {labId && !permissions && <Spinner />}
          {permissions && isOwner && <div className="text-muted">{t('The user owns this laboratory: all rights are always granted.')}</div>}
          {permissions && !isOwner && row && row.role !== 'admin' && (
            <div className={permGrid}>
              {permissions.catalog.map((perm) => {
                const inherited = Boolean(row.role_perms?.[perm.key]);
                return (
                  <div key={perm.key} className="flex items-start gap-2">
                    <Switch className={permItem} checked={Boolean(row.perms[perm.key])} title={permTitle(perm.key)}
                      label={permTitle(perm.key)}
                      onChange={(checked) => void toggleLab(perm.key, checked)} />
                    {inherited && <span className="whitespace-nowrap rounded-full bg-accent-soft px-2 py-[2px] text-[11px] text-accent">{t('from role')}</span>}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}

function RoleDialog({ user, onClose }: { user: User; onClose: () => void }) {
  // The role list is the whole "Roles" section: the built-in administrator role and all additional ones.
  // One role is selected; the account's base role (user) remains if nothing is selected.
  const roles = useQuery({ queryKey: ['roles'], queryFn: rolesApi.list });
  const assigned = (user.roles ?? []).map((item) => item.id);
  const adminRoleID = (roles.data ?? []).find((item) => item.system)?.id ?? Number.MIN_SAFE_INTEGER;
  // The built-in administrator role is the users.role field; other roles are assigned via user_roles.
  const initial = user.role === 'admin' ? adminRoleID : assigned.find((id) => id !== adminRoleID);
  const [choice, setChoice] = useState<string>(initial !== undefined ? String(initial) : '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const close = () => {
    if (busy) return;
    onClose();
  };

  const save = async () => {
    const next = choice === '' ? [] : [Number(choice)];
    if (busy || next.join(',') === (initial !== undefined ? String(initial) : '')) return;
    setBusy(true);
    setError('');
    try {
      // Assigning the built-in administrator role changes the account (users.role) and removes
      // the previous additional role; choosing another role first demotes the administrator.
      // First demote the administrator to a regular account: otherwise assigning a new role
      // does not change users.role and the user remains an administrator.
      if (user.role === 'admin' && !next.includes(adminRoleID)) {
        await usersApi.update(user.id, { role: 'user' });
      }
      const current = user.role === 'admin' ? [] : assigned;
      for (const id of next) {
        if (!current.includes(id)) await rolesApi.addMembers(id, [user.id]);
      }
      for (const id of current) {
        if (!next.includes(id)) await rolesApi.removeMember(id, user.id);
      }
      toast.success(t('Role changed'));
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setBusy(false);
    }
  };

  const list = roles.data ?? [];
  return (
    <Modal title={`${t('Change role')}: ${user.username}`} onClose={close} actions={[
      { label: t('Cancel'), onClick: close, closeOnClick: false },
      { label: busy ? t('Saving…') : t('Save'), primary: true, closeOnClick: false, onClick: () => save() },
    ]}>
      <p className="mb-4 mt-0 text-sm leading-relaxed text-muted">{t('Choose the role of the account. Every role grants its own permissions and folder access.')}</p>
      {roles.isLoading ? <Spinner /> : list.length ? (
        <>
          <label htmlFor="role-dialog-select">{t('Role')}</label>
          <select id="role-dialog-select" value={choice} disabled={busy} onChange={(event) => setChoice(event.target.value)}>
            <option value="">{t('No role')}</option>
            {list.map((item: RbacRole) => (
              <option key={item.id} value={String(item.id)}>{item.name}{item.description ? ` — ${item.description}` : ''}</option>
            ))}
          </select>
        </>
      ) : <div className={hint}>{t('No roles available')}</div>}
      {error && <div role="alert" aria-live="polite" className="mt-2.5 text-xs leading-relaxed text-bad">{error}</div>}
    </Modal>
  );
}

export function UsersPage() {
  const self = useAuthStore((state) => state.user);
  const isAdmin = self?.role === 'admin';
  const canManage = can(self, 'users.manage');
  const canSettings = can(self, 'users.settings') || canManage;
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['users'], queryFn: usersApi.list });
  useLanguageStore((state) => state.language);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  // The selected role of a new account: the built-in administrator role or an additional role.
  const [accountRole, setAccountRole] = useState('user');
  const [createError, setCreateError] = useState('');
  const [permsUser, setPermsUser] = useState<User | null>(null);
  const [passwordUser, setPasswordUser] = useState<User | null>(null);
  const [roleUser, setRoleUser] = useState<User | null>(null);
  const roles = useQuery({ queryKey: ['roles'], queryFn: rolesApi.list });
  const reload = () => void queryClient.invalidateQueries({ queryKey: ['users'] });
  const createMismatch = confirmPassword.length > 0 && password !== confirmPassword;

  const createUser = () => {
    const name = username.trim();
    if (!name) {
      setCreateError(t('Enter a name'));
      return;
    }
    if (!isValidUsername(name)) {
      setCreateError(t('Username: 1–64 characters, Latin letters, digits, _, - and single spaces between words'));
      return;
    }
    if (!isValidPassword(password)) {
      setCreateError(t('Password: 8–256 characters, Latin letters, digits and symbols, no spaces'));
      return;
    }
    if (password !== confirmPassword) {
      setCreateError(t('Passwords do not match'));
      return;
    }
    setCreateError('');
    void guard(async () => {
      // An administrator account differs from an additional role: the former gets full permissions.
      const isAdminAccount = accountRole === 'admin';
      const roleId = !isAdminAccount && accountRole !== 'user' ? Number(accountRole) : 0;
      await usersApi.create(name, password, isAdminAccount ? 'admin' : 'user', confirmPassword, roleId);
      setUsername(''); setPassword(''); setConfirmPassword(''); setAccountRole('user');
      toast.success(t('Saved')); reload();
    });
  };

  if (query.isLoading) return <Spinner />;
  if (query.error) return <div className="py-2 text-bad">{query.error.message}</div>;
  return (
    <>
      <h2>{t('Users')}</h2>
      {canManage && (
        <div className={box}>
          <h3 className={boxTitle}>{t('New user')}</h3>
          <div className={`grid grid-cols-1 items-end gap-2.5 ${isAdmin ? 'min-[901px]:grid-cols-[1fr_1fr_1fr_180px_auto]' : 'min-[901px]:grid-cols-[1fr_1fr_1fr_auto]'}`}>
            <input placeholder={t('Username')} autoComplete="off" maxLength={USERNAME_MAX_LENGTH} value={username}
              onChange={(event) => { setUsername(filterUsername(event.target.value)); if (createError) setCreateError(''); }} />
            <PasswordInput placeholder={t('Password (≥ 8 characters)')} autoComplete="new-password" minLength={8} maxLength={PASSWORD_MAX_LENGTH}
              value={password} onChange={(event) => { setPassword(event.target.value); if (createError) setCreateError(''); }} />
            <PasswordInput placeholder={t('Confirm Password')} autoComplete="new-password" minLength={8} maxLength={PASSWORD_MAX_LENGTH}
              value={confirmPassword} onChange={(event) => { setConfirmPassword(event.target.value); if (createError) setCreateError(''); }} />
            {isAdmin && (roles.data ?? []).length > 0 && (
              // The role is selected from the "Roles" section list: the built-in administrator role and all
              // additional roles. Multiple roles of one account are assigned in "Change role".
              <select aria-label={t('Role')} value={accountRole} onChange={(event) => setAccountRole(event.target.value)}>
                {(roles.data ?? []).map((item: RbacRole) => (
                  <option key={item.id} value={item.system ? 'admin' : String(item.id)}>{item.name}</option>
                ))}
              </select>
            )}
            <button type="button" className={primary} onClick={createUser}>{t('Create')}</button>
          </div>
          {createError && <div role="alert" aria-live="polite" className="mt-2.5 text-xs leading-relaxed text-bad">{createError}</div>}
          {!createError && createMismatch && <div role="status" aria-live="polite" className="mt-2.5 text-xs leading-relaxed text-bad">{t('Passwords do not match')}</div>}
        </div>
      )}
      <div className={box}>
        <table>
          <thead><tr>{[t('Username'), t('Role'), t('Status'), t('Created'), ''].map((title) => <th key={title}>{title}</th>)}</tr></thead>
          <tbody>
            {(query.data ?? []).map((user) => {
              const isSelf = user.id === self?.id;
              return (
                <tr key={user.id}>
                  <td>{user.username}{isSelf && <span className="text-muted"> {t('You')}</span>}</td>
                  <td>
                    {/* One value per account: administrator, the assigned role(s), or none. */}
                    {user.role === 'admin' ? <div>{t('Administrator')}</div>
                      : (user.roles ?? []).length > 0 ? <div>{(user.roles ?? []).map((item) => item.name).join(', ')}</div>
                      : <div className="text-muted">{t('No role')}</div>}
                  </td>
                  <td className={user.disabled ? 'text-bad' : 'text-ok'}>{user.disabled ? t('Disabled') : t('Active')}</td>
                  <td className="text-muted">{fmtTime(user.created_at)}</td>
                  <td><div className="flex flex-wrap items-center gap-1.5 [&>button]:min-h-[30px] [&>button]:px-[11px] [&>button]:py-[5px] [&>button]:text-[13px]">
                    {isAdmin && (
                      <button type="button" disabled={isSelf} onClick={() => setRoleUser(user)}>{t('Change role')}</button>
                    )}
                    {canSettings && (
                      <button type="button" disabled={isSelf || (!isAdmin && user.role === 'admin')} onClick={() => void guard(async () => {
                        await usersApi.update(user.id, { disabled: !user.disabled }); reload();
                      })}>{user.disabled ? t('Enable') : t('Disable')}</button>
                    )}
                    {isAdmin && <button type="button" disabled={user.role === 'admin'} onClick={() => setPermsUser(user)}>{t('Rights')}</button>}
                    {canSettings && (
                      <button type="button" disabled={!isAdmin && user.role === 'admin'} onClick={() => setPasswordUser(user)}>{t('Password')}</button>
                    )}
                    {canManage && (
                      <button type="button" className={danger} disabled={isSelf || (!isAdmin && user.role === 'admin')} onClick={async () => {
                        if (!(await askConfirm({ title: `${t('Delete user with their laboratories?')} (${user.username})`, danger: true }))) return;
                        void guard(async () => { await usersApi.remove(user.id); reload(); });
                      }}>{t('Delete')}</button>
                    )}
                  </div></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {roleUser && <RoleDialog user={roleUser} onClose={() => { setRoleUser(null); reload(); }} />}
      {permsUser && <PermissionDialog user={permsUser} onClose={() => { setPermsUser(null); reload(); }} />}
      {passwordUser && (
        <PasswordDialog
          requireCurrent={false}
          title={t('Change Password')}
          description={t('Set a new password for {username}', { username: passwordUser.username })}
          successMessage={t('Password reset')}
          onClose={() => setPasswordUser(null)}
          onSubmit={async (_current, next, confirm) => {
            await usersApi.update(passwordUser.id, { password: next, confirm });
          }}
        />
      )}
    </>
  );
}
