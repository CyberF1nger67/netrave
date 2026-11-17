import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// There is no separate "User" role in the system: a regular account is the absence of a role.
// The UI must not have such an item, neither in role change nor in user creation.
describe('нет предустановленной роли «Пользователь»', () => {
  const read = (file: string) => readFileSync(resolve(__dirname, '..', '..', file), 'utf8');

  it('в смене роли пустой выбор называется «Без роли»', () => {
    const source = read('features/users/UsersPage.tsx');
    expect(source).toContain('{t(\'No role\')}');
    expect(source).not.toContain('{t(\'User\')}');
  });

  it('в списке пользователей обычная учётная запись — «Без роли»', () => {
    const source = read('features/users/UsersPage.tsx');
    expect(source).toContain("t('Administrator') : t('No role')");
  });
});
