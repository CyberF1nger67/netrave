import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// Server calls are mocked: what requests "Change role" sends is checked.
const api = vi.hoisted(() => ({
  list: vi.fn(),
  addMembers: vi.fn(),
  removeMember: vi.fn(),
  update: vi.fn(),
}));

vi.mock('../../api/endpoints', () => ({
  usersApi: { list: api.list, update: api.update },
  rolesApi: { list: vi.fn(async () => ROLES), addMembers: api.addMembers, removeMember: api.removeMember },
  labsApi: { list: vi.fn(async () => []) },
}));

import { UsersPage } from './UsersPage';
import { useAuthStore } from '../../stores/auth';
import type { User } from '../../types/api';

const admin: User = { id: 1, username: 'root', role: 'admin', disabled: false, created_at: 0, roles: [], role_perms: null };
const bob: User = { ...admin, id: 2, username: 'bob', role: 'admin', roles: [] };

const ROLES = [
  { id: -1, name: 'Administrator', description: '', permissions: [], folder_ids: [], member_count: 2, created_at: 0, updated_at: 0, system: true },
  { id: 5, name: 'Teacher', description: '', permissions: [], folder_ids: [], member_count: 0, created_at: 0, updated_at: 0 },
];

describe('смена роли', () => {
  it('администратору можно назначить другую роль: сначала понижение, затем назначение', async () => {
    api.list.mockResolvedValue([admin, bob]);
    api.addMembers.mockResolvedValue({ ok: true, user_ids: [2] });
    api.update.mockResolvedValue({ ok: true });
    // "Change role" is available to the administrator: the current user is an administrator.
    useAuthStore.setState({ user: admin, status: 'authenticated' });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><UsersPage /></QueryClientProvider>);

    const row = (await screen.findAllByText('bob'))[0]!.closest('tr')!;
    // Row buttons: "Change role" for every user except oneself.
    const target = [...row.querySelectorAll('button')].find((b) => b.textContent?.includes('Change role'));
    expect(target, 'кнопка смены роли').toBeTruthy();
    fireEvent.click(target!);

    const select = await screen.findByLabelText('Role', { selector: '#role-dialog-select' });
    expect(select).toHaveValue('-1');
    fireEvent.change(select, { target: { value: '5' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(api.update).toHaveBeenCalledWith(2, { role: 'user' }));
    await waitFor(() => expect(api.addMembers).toHaveBeenCalledWith(5, [2]));
  });
});
