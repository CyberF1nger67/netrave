import { describe, expect, it } from 'vitest';
import { canChangePassword } from './auth';
import type { User } from '../types/api';

const user = (patch: Partial<User>): User => ({
  id: 1,
  username: 'user',
  role: 'user',
  disabled: false,
  created_at: 0,
  roles: [],
  role_perms: [],
  ...patch,
});

describe('canChangePassword', () => {
  it('always allows administrators', () => {
    expect(canChangePassword(user({ role: 'admin' }))).toBe(true);
  });

  it('keeps the old behaviour for users without roles', () => {
    expect(canChangePassword(user({ roles: [] }))).toBe(true);
  });

  it('denies a role user without users.password', () => {
    expect(canChangePassword(user({ roles: [{ id: 1, name: 'Students' }], role_perms: ['users.view'] }))).toBe(false);
  });

  it('allows a role user with users.password', () => {
    expect(canChangePassword(user({ roles: [{ id: 1, name: 'Students' }], role_perms: ['users.password'] }))).toBe(true);
  });
});
