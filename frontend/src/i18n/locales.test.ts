// Checks the YAML locale files against EN_KEYS. Runs against the same virtual module
// the application uses, so the assertions cover exactly what ships in the bundle.
import { readFileSync } from 'node:fs';
import { load } from 'js-yaml';
import { describe, expect, it } from 'vitest';
import { locales } from 'virtual:locales';
import { EN_KEYS } from './index';

// Vitest serves modules over http, so the file is read from the project root.
const RU_YAML = 'locales/ru.yaml';

// Keys whose Russian text is intentionally identical to the English one: names,
// abbreviations and product terms that are not translated.
const SAME_TEXT_ALLOWED = new Set<string>([
  'QEMU · KVM · NETWORK',
  '{name}: {detail}',
  '{name}: {detail_key}',
  '{path} ({size}).',
  '«{name}»',
  '{name} ({role})',
  '{name} → «{lab}»',
  '{device}: {mode}',
  'xterm.js',
  'BIOS (SeaBIOS)',
  'CD-ROM',
  'CPU',
  'IP',
  'NAT',
  'NetRave',
  'PID',
  'UEFI',
  'VNC',
]);

const uniqueKeys = [...new Set<string>(EN_KEYS)];
const ru = locales['ru'] ?? {};
const englishKeys = new Set(uniqueKeys);

/** Placeholder names such as `{username}`. */
function placeholders(text: string): string[] {
  return [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1] as string).sort();
}

describe('locales/ru.yaml', () => {
  it('is loaded into the virtual module', () => {
    expect(Object.keys(locales)).toEqual(['ru']);
    expect(Object.keys(ru).length).toBeGreaterThan(100);
  });

  it('contains only keys from EN_KEYS', () => {
    expect(Object.keys(ru).filter((key) => !englishKeys.has(key))).toEqual([]);
  });

  it('translates every EN_KEYS entry, except the listed identical terms', () => {
    const untranslated = uniqueKeys.filter((key) => ru[key] === undefined);
    expect(untranslated).toEqual([]);

    const sameText = uniqueKeys.filter((key) => ru[key] === key && !SAME_TEXT_ALLOWED.has(key));
    expect(sameText).toEqual([]);
  });

  it('keeps the identical-text exception list up to date', () => {
    const stale = [...SAME_TEXT_ALLOWED].filter((key) => !englishKeys.has(key) || ru[key] !== key);
    expect(stale).toEqual([]);
  });

  it('keeps the {placeholder} set of every key in sync', () => {
    const mismatched = uniqueKeys
      .filter((key) => ru[key] !== undefined)
      .map((key) => ({ key, expected: placeholders(key), actual: placeholders(ru[key] as string) }))
      .filter(({ expected, actual }) => expected.join(',') !== actual.join(','));
    expect(mismatched).toEqual([]);
  });

  it('has no empty translations', () => {
    const empty = Object.entries(ru).filter(([, text]) => String(text).trim() === '');
    expect(empty).toEqual([]);
  });

  it('follows the EN_KEYS order', () => {
    const fileOrder = Object.keys(ru);
    expect(fileOrder).toEqual(uniqueKeys.filter((key) => key in ru));
  });

  it('rejects duplicate keys', () => {
    const text = readFileSync(RU_YAML, 'utf8');
    expect(() => load(text)).not.toThrow();
    // js-yaml throws on a repeated key: the build fails instead of silently keeping the last value.
    expect(() => load('"Save": "Save"\n"Save": "Save"\n')).toThrow(/duplicated mapping key/);
  });
});
