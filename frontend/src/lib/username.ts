// Login names follow the login name rule: 1–64 characters from Latin letters, digits, '_' and '-',
// with single spaces between words. Existing accounts keep whatever name they already have.
import { NAME_MAX_LENGTH, isValidLogin } from './names';

export const USERNAME_MAX_LENGTH = NAME_MAX_LENGTH;

const DISALLOWED_CHARS = /[^A-Za-z0-9_ -]/g;

/** Drops characters a login name may not contain (dots, Cyrillic, symbols) and caps the length. Spaces stay. */
export function filterUsername(input: string): string {
  return input.replace(DISALLOWED_CHARS, '').slice(0, USERNAME_MAX_LENGTH);
}

/** Reports whether a name satisfies the rule for a new account. */
export function isValidUsername(name: string): boolean {
  return isValidLogin(name);
}
