import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// There is no separate "User" role in the system: a regular account is the absence of a role.
// The UI must not have such an item, neither in role change nor in user creation.
describe('no predefined "User" role', () => {
  const read = (file: string) => readFileSync(resolve(__dirname, '..', '..', file), 'utf8');

  it('in role change the empty choice is labeled "No role"', () => {
    const source = read('features/users/UsersPage.tsx');
    expect(source).toContain("{t('No role')}");
    expect(source).not.toContain("{t('User')}");
  });

  it('the user list shows exactly one value for an account', () => {
    const source = read('features/users/UsersPage.tsx');
    expect(source).toContain("user.role === 'admin' ? <div>{t('Administrator')}</div>");
    expect(source).toContain("<div className=\"text-muted\">{t('No role')}</div>");
    expect(source).not.toMatch(/\{t\('Role'\)\}: /);
  });
});
