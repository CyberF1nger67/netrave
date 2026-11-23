import { describe, expect, it } from 'vitest';
import { isValidFileName, isValidLogin, isValidName, isValidPassword } from './names';

describe('object names', () => {
  it('accepts any name Linux allows in a file name, up to 64 characters', () => {
    expect(isValidName('a')).toBe(true);
    expect(isValidName('Lab_01-x')).toBe(true);
    expect(isValidName('тест')).toBe(true);
    expect(isValidName('my lab')).toBe(true);
    expect(isValidName('disk.v1')).toBe(true);
    expect(isValidName('a'.repeat(64))).toBe(true);
    expect(isValidName('я'.repeat(64))).toBe(true);
    expect(isValidName('a'.repeat(65))).toBe(false);
  });

  it('rejects slashes, control characters, leading dots and empty names', () => {
    expect(isValidName('')).toBe(false);
    expect(isValidName('a/b')).toBe(false);
    expect(isValidName('a\nb')).toBe(false);
    expect(isValidName('a\tb')).toBe(false);
    expect(isValidName('.hidden')).toBe(false);
    expect(isValidName('..')).toBe(false);
  });
});

describe('file names', () => {
  it('accepts names with Cyrillic, spaces and dots', () => {
    expect(isValidFileName('disk.qcow2')).toBe(true);
    expect(isValidFileName('диск.iso')).toBe(true);
    expect(isValidFileName('my disk.v1.iso')).toBe(true);
  });

  it('limits the length to 255 bytes and rejects slashes', () => {
    expect(isValidFileName('a'.repeat(255))).toBe(true);
    expect(isValidFileName('a'.repeat(256))).toBe(false);
    expect(isValidFileName('я'.repeat(128))).toBe(false);
    expect(isValidFileName('a/b.iso')).toBe(false);
  });
});

describe('login names', () => {
  it('accepts Latin letters, digits, underscore, hyphen and single spaces between words', () => {
    expect(isValidLogin('admin')).toBe(true);
    expect(isValidLogin('John Doe')).toBe(true);
    expect(isValidLogin('a'.repeat(64))).toBe(true);
    expect(isValidLogin('a'.repeat(65))).toBe(false);
  });

  it('rejects leading, trailing and doubled spaces, dots and Cyrillic', () => {
    expect(isValidLogin(' john')).toBe(false);
    expect(isValidLogin('john ')).toBe(false);
    expect(isValidLogin('john  doe')).toBe(false);
    expect(isValidLogin('john.doe')).toBe(false);
    expect(isValidLogin('тест')).toBe(false);
  });
});

describe('passwords', () => {
  it('requires 8 to 256 printable ASCII characters without spaces', () => {
    expect(isValidPassword('12345678')).toBe(true);
    expect(isValidPassword('a'.repeat(256))).toBe(true);
    expect(isValidPassword('1234567')).toBe(false);
    expect(isValidPassword('a'.repeat(257))).toBe(false);
    expect(isValidPassword('пароль123')).toBe(false);
    expect(isValidPassword('pass word')).toBe(false);
  });
});
