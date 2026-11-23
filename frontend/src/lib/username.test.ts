import { describe, expect, it } from 'vitest';
import { filterUsername, isValidUsername } from './username';

describe('filterUsername', () => {
  it('removes dots, symbols and Cyrillic, keeps spaces', () => {
    expect(filterUsername('john doe')).toBe('john doe');
    expect(filterUsername('john.doe')).toBe('johndoe');
    expect(filterUsername('a@b#c$')).toBe('abc');
    expect(filterUsername('тест')).toBe('');
  });

  it('keeps letters, digits, underscore and hyphen', () => {
    expect(filterUsername('Admin_01-x')).toBe('Admin_01-x');
  });

  it('caps the length at 64 characters', () => {
    expect(filterUsername('a'.repeat(80))).toHaveLength(64);
  });
});

describe('isValidUsername', () => {
  it('accepts names of 1 to 64 allowed characters, with single spaces between words', () => {
    expect(isValidUsername('a')).toBe(true);
    expect(isValidUsername('a'.repeat(64))).toBe(true);
    expect(isValidUsername('john doe')).toBe(true);
  });

  it('rejects empty, long, and disallowed names', () => {
    expect(isValidUsername('')).toBe(false);
    expect(isValidUsername('a'.repeat(65))).toBe(false);
    expect(isValidUsername('john  doe')).toBe(false);
    expect(isValidUsername('john.doe')).toBe(false);
    expect(isValidUsername('тест')).toBe(false);
  });
});
